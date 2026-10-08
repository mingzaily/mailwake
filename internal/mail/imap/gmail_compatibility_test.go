package imap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

// This synthetic TLS fixture exercises Gmail-shaped IMAP responses. Real Gmail
// account policy, Label visibility and app-password compatibility require their
// own account acceptance record.
type gmailIMAPFixture struct {
	source  *Source
	user    *imapmemserver.User
	folders []protocol.ListData
	mu      sync.Mutex
	secret  string
	clients map[net.Conn]struct{}
}

type gmailIMAPSession struct {
	imapserver.Session
	fixture *gmailIMAPFixture
	conn    net.Conn
}

func (s *gmailIMAPSession) Close() error {
	s.fixture.mu.Lock()
	delete(s.fixture.clients, s.conn)
	s.fixture.mu.Unlock()
	return s.Session.Close()
}

func (s *gmailIMAPSession) Login(username, password string) error {
	s.fixture.mu.Lock()
	accepted := s.fixture.secret != "" && password == s.fixture.secret
	s.fixture.mu.Unlock()
	if !accepted {
		return imapserver.ErrAuthFailed
	}
	return s.Session.Login(username, "synthetic-backend-password")
}

func (s *gmailIMAPSession) List(w *imapserver.ListWriter, reference string, patterns []string, _ *protocol.ListOptions) error {
	for _, folder := range s.fixture.folders {
		for _, pattern := range patterns {
			if imapserver.MatchList(folder.Mailbox, folder.Delim, reference, pattern) {
				if err := w.WriteList(&folder); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func gmailFixture(t *testing.T, namespace string) *gmailIMAPFixture {
	t.Helper()
	f := &gmailIMAPFixture{secret: "synthetic-app-password", clients: make(map[net.Conn]struct{})}
	for _, folder := range []struct {
		name string
		attr protocol.MailboxAttr
	}{
		{namespace + "/Sent Mail", protocol.MailboxAttrSent},
		{"Projects/Invoices/2026", protocol.MailboxAttrHasNoChildren},
		{namespace, protocol.MailboxAttrNoSelect},
		{"INBOX", protocol.MailboxAttrHasNoChildren},
		{namespace + "/All Mail", protocol.MailboxAttrAll},
		{"Projects", protocol.MailboxAttrNoSelect},
		{namespace + "/Drafts", protocol.MailboxAttrDrafts},
		{"Projects/Clients", protocol.MailboxAttrHasNoChildren},
		{"Projects/Invoices", protocol.MailboxAttrNoSelect},
		{namespace + "/Spam", protocol.MailboxAttrJunk},
		{namespace + "/Trash", protocol.MailboxAttrTrash},
		{"重要/银行", protocol.MailboxAttrHasNoChildren},
		{"Label with spaces", protocol.MailboxAttrHasNoChildren},
	} {
		f.folders = append(f.folders, protocol.ListData{Mailbox: folder.name, Delim: '/', Attrs: []protocol.MailboxAttr{folder.attr}})
	}
	const username = "Synthetic.User@example.test"
	f.user = imapmemserver.NewUser(username, "synthetic-backend-password")
	for _, folder := range f.folders {
		if !slices.Contains(folder.Attrs, protocol.MailboxAttrNoSelect) {
			if err := f.user.Create(folder.Mailbox, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	backend := imapmemserver.New()
	backend.AddUser(f.user)
	cert := httptest.NewTLSServer(nil)
	roots := x509.NewCertPool()
	roots.AddCert(cert.Certificate())
	listener, err := tls.Listen("tcp", "127.0.0.1:0", cert.TLS.Clone())
	cert.Close()
	if err != nil {
		t.Fatal(err)
	}
	server := imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			f.mu.Lock()
			f.clients[conn.NetConn()] = struct{}{}
			f.mu.Unlock()
			return &gmailIMAPSession{Session: backend.NewSession(), fixture: f, conn: conn.NetConn()}, nil, nil
		},
		Caps:   protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapIdle: {}},
		Logger: log.New(io.Discard, "", 0),
	})
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(listener) }()
	t.Cleanup(func() { server.Close(); <-done })
	f.source = NewWithOptions("127.0.0.1", listener.Addr().(*net.TCPAddr).Port, username, f.secret, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{RootCAs: roots})
	return f
}

func (f *gmailIMAPFixture) setPassword(password string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.secret = password
}

func (f *gmailIMAPFixture) disconnect() {
	f.mu.Lock()
	clients := make([]net.Conn, 0, len(f.clients))
	for conn := range f.clients {
		clients = append(clients, conn)
	}
	f.mu.Unlock()
	for _, conn := range clients {
		conn.Close()
	}
}

func (f *gmailIMAPFixture) append(t *testing.T, folder, raw string, receivedAt time.Time) *protocol.AppendData {
	t.Helper()
	data, err := f.user.Append(folder, strings.NewReader(raw), &protocol.AppendOptions{Time: receivedAt})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestGmailStyleFolderDiscovery(t *testing.T) {
	for _, namespace := range []string{"[Gmail]", "[GoogleMail]"} {
		t.Run(namespace, func(t *testing.T) {
			f := gmailFixture(t, namespace)
			folders, err := f.source.Folders(t.Context())
			want := []string{"INBOX", "Label with spaces", "Projects/Clients", "Projects/Invoices/2026", namespace + "/All Mail", namespace + "/Drafts", namespace + "/Sent Mail", namespace + "/Spam", namespace + "/Trash", "重要/银行"}
			slices.Sort(want)
			if err != nil || !slices.Equal(folders, want) {
				t.Fatalf("selectable Label paths: got %v, want %v, error %v", folders, want, err)
			}
			for _, folder := range folders {
				session, err := f.source.Open(t.Context(), folder)
				if err != nil {
					t.Fatalf("open discovered Label %q: %v", folder, err)
				}
				batch, err := session.Poll(t.Context(), "", false)
				session.Close()
				if err != nil || batch.Checkpoint == "" || len(batch.Messages) != 0 {
					t.Fatalf("initial Label baseline %q: %+v, error %v", folder, batch, err)
				}
			}
		})
	}
}

func TestGmailStyleLabelsKeepFolderEventsAndOfflineWindow(t *testing.T) {
	f := gmailFixture(t, "[Gmail]")
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	folders := []string{"Projects/Clients", "Projects/Invoices/2026"}
	const raw = "From: Sender <sender@example.org>\r\nMessage-ID: <same-message@example.test>\r\nSubject: shared Label member\r\n\r\nbody remains unread\r\n"
	for i, folder := range folders {
		for j := 0; j <= i; j++ {
			f.append(t, folder, raw, time.Now())
		}
	}
	start := func() (context.CancelFunc, <-chan struct{}) {
		ctx, cancel := context.WithCancel(t.Context())
		monitor := engine.New(f.source, store, "gmail-imap", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders(folders)}, 20*time.Millisecond, true, f.source.log)
		done := make(chan struct{})
		go func() { defer close(done); monitor.Run(ctx) }()
		t.Cleanup(func() { cancel(); <-done })
		eventually(t, func() bool {
			statuses := monitor.Status()
			return len(statuses) == 2 && statuses[0].State == "watching" && statuses[1].State == "watching"
		})
		return cancel, done
	}
	stop, done := start()
	if summary, err := store.Summary(t.Context()); err != nil || summary.Pending != 0 {
		t.Fatalf("first subscription skips existing Label members: %+v, error %v", summary, err)
	}
	want := map[string]string{}
	lastUID := map[string]protocol.UID{}
	for _, folder := range folders {
		data := f.append(t, folder, raw, time.Now())
		want[event.ID("gmail-imap", folder, fmt.Sprintf("%d:%d", data.UIDValidity, data.UID))] = folder
		lastUID[folder] = data.UID
	}
	if lastUID[folders[0]] == lastUID[folders[1]] {
		t.Fatal("fixture must expose independent Folder UID sequences")
	}
	eventually(t, func() bool { summary, err := store.Summary(t.Context()); return err == nil && summary.Pending == 2 })
	f.disconnect()
	// The same Message-ID can arrive again with a new Folder UID. The receive
	// time determines the recovery window; the Folder and UID determine identity.
	for i, folder := range folders {
		if i == 1 {
			f.append(t, folder, raw, time.Now().Add(-25*time.Hour))
		}
		data := f.append(t, folder, raw, time.Now().Add(-23*time.Hour))
		want[event.ID("gmail-imap", folder, fmt.Sprintf("%d:%d", data.UIDValidity, data.UID))] = folder
		lastUID[folder] = data.UID
	}
	eventually(t, func() bool { summary, err := store.Summary(t.Context()); return err == nil && summary.Pending == 4 })
	// Await durable progress through the old member as well as the fresh member.
	for _, folder := range folders {
		checkpoint, err := store.Checkpoint(t.Context(), "gmail-imap", folder)
		var position cursor
		if err != nil || json.Unmarshal([]byte(checkpoint), &position) != nil || position.Next != uint32(lastUID[folder])+1 {
			t.Fatalf("Label progress %q: %q, error %v", folder, checkpoint, err)
		}
		status, err := f.user.Status(folder, &protocol.StatusOptions{NumUnseen: true, NumMessages: true})
		if err != nil || *status.NumUnseen != *status.NumMessages {
			t.Fatalf("Label metadata reads preserve unread messages %q: %+v, error %v", folder, status, err)
		}
	}
	stop()
	<-done
	stop, done = start()
	stop()
	<-done
	rows, err := store.Recent(t.Context())
	if err != nil || len(rows) != len(want) {
		t.Fatalf("Folder events after recovery: got %d, want %d, error %v", len(rows), len(want), err)
	}
	for _, row := range rows {
		folder, exists := want[row.ID]
		if !exists || row.Message == nil || row.Message.Folder != folder {
			t.Fatalf("event retains its Folder and UID identity: %+v", row)
		}
		delete(want, row.ID)
	}
	if len(want) != 0 {
		t.Fatal("missing Folder events", want)
	}
}

func TestGmailStyleAppPasswordReplacementResumesCheckpoint(t *testing.T) {
	f := gmailFixture(t, "[GoogleMail]")
	const folder = "Projects/Clients"
	const raw = "From: Sender <sender@example.org>\r\nSubject: app-password recovery\r\n\r\nbody remains unread\r\n"
	f.append(t, folder, raw, time.Now())
	session, err := f.source.Open(t.Context(), folder)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := session.Poll(t.Context(), "", false)
	session.Close()
	if err != nil || len(baseline.Messages) != 0 {
		t.Fatalf("credential test baseline: %+v, error %v", baseline, err)
	}
	f.setPassword("")
	if _, err := f.source.Folders(t.Context()); fault.From(err, "").Code != mail.CodeAuthFailed {
		t.Fatalf("revoked app password rejects discovery: %v", err)
	}
	if _, err := f.source.Open(t.Context(), folder); fault.From(err, "").Code != mail.CodeAuthFailed {
		t.Fatalf("revoked app password rejects new sessions: %v", err)
	}
	data := f.append(t, folder, raw, time.Now())
	f.setPassword("synthetic-replacement-app-password")
	if _, err := f.source.Folders(t.Context()); fault.From(err, "").Code != mail.CodeAuthFailed {
		t.Fatalf("old app password remains rejected after replacement: %v", err)
	}
	f.source.password = "synthetic-replacement-app-password"
	if _, err := f.source.Folders(t.Context()); err != nil {
		t.Fatal("replacement app password restores discovery", err)
	}
	session, err = f.source.Open(t.Context(), folder)
	if err != nil {
		t.Fatal("replacement app password restores monitoring", err)
	}
	defer session.Close()
	batch, err := session.Poll(t.Context(), baseline.Checkpoint, false)
	wantKey := fmt.Sprintf("%d:%d", data.UIDValidity, data.UID)
	if err != nil || batch.Reset || len(batch.Messages) != 1 || batch.Messages[0].Key != wantKey {
		t.Fatalf("replacement keeps progress and catches up the new member: %+v, error %v", batch, err)
	}
	if caughtUp, err := session.Poll(t.Context(), batch.Checkpoint, false); err != nil || len(caughtUp.Messages) != 0 {
		t.Fatalf("caught-up Label preserves progress: %+v, error %v", caughtUp, err)
	}
}
