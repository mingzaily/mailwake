package httpapi

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/mingzaily/mailwake/internal/application"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/mailview"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestMailHistoryAcrossHTTPRuntimeAndIMAP(t *testing.T) {
	cert := httptest.NewTLSServer(nil)
	roots := x509.NewCertPool()
	roots.AddCert(cert.Certificate())
	listener, err := tls.Listen("tcp", "127.0.0.1:0", cert.TLS.Clone())
	cert.Close()
	if err != nil {
		t.Fatal(err)
	}
	user := imapmemserver.NewUser("synthetic@example.test", "synthetic-password")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	backend := imapmemserver.New()
	backend.AddUser(user)
	server := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return backend.NewSession(), nil, nil
	}, Caps: protocol.CapSet{protocol.CapIMAP4rev1: {}}, Logger: log.New(io.Discard, "", 0)})
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(listener) }()
	t.Cleanup(func() { server.Close(); <-done })
	// Imported dates intentionally disagree with ascending UID order.
	for i := 1; i <= 300; i++ {
		raw := fmt.Sprintf("From: Synthetic sender <alerts@example.test>\r\nSubject: private-history-marker %d\r\n\r\nprivate-body-marker", i)
		flags := []protocol.Flag{}
		if i%3 == 0 {
			flags = []protocol.Flag{protocol.FlagSeen}
		}
		if _, err := user.Append("INBOX", strings.NewReader(raw), &protocol.AppendOptions{Time: time.Now().Add(-time.Duration(i%29)*24*time.Hour - time.Hour), Flags: flags}); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	logger := logging.New(io.Discard)
	app, err := application.New(t.Context(), store, vault, logger, "", application.Options{IMAPRootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	auth, grant := testAdministrator(t, store)
	identity, err := app.Management.Identity.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f := appFixture{router: New(auth, app.Manager, store, logger, Integrations{Native: app.Native, Management: app.Management}), grant: grant, coreID: identity.ID, service: app.Management, log: logger}
	password := "synthetic-password"
	mailbox, err := app.CreateMailbox(t.Context(), settings.MailboxCreate{MailboxUpdate: settings.MailboxUpdate{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Username: "synthetic@example.test", Password: &password}, Folders: []mail.Folder{{Name: "INBOX", Check: "5m"}}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for app.Manager.Status()[0].State != "watching" {
		if time.Now().After(deadline) {
			t.Fatal("initial monitor did not become healthy")
		}
		time.Sleep(5 * time.Millisecond)
	}
	checkpoint, _ := store.Checkpoint(t.Context(), mailbox.ID, "INBOX")
	events, _ := store.Recent(t.Context())
	credential, _ := f.invite(t, []string{"content"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	w := f.do(t, "GET", appPrefix+"/mail/folders", nil, headers)
	var catalog mailview.Page[mailview.Folder]
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &catalog) != nil || len(catalog.Items) != 1 {
		t.Fatalf("catalog: %d %s", w.Code, w.Body.String())
	}
	input := mailview.MessageRequest{MailboxID: catalog.Items[0].MailboxID, AccountID: catalog.Items[0].AccountID, Folder: "INBOX", Limit: 100}
	started := time.Now()
	var all []mailview.Message
	pages, bytes := 0, 0
	for {
		w = f.do(t, "POST", appPrefix+"/mail/messages", input, headers)
		var page mailview.Messages
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil {
			t.Fatalf("messages: %d %s", w.Code, w.Body.String())
		}
		if w.Body.Len() > mailview.MaxResponseBytes || strings.Contains(w.Body.String(), "private-body-marker") {
			t.Fatal("history exceeded response boundary")
		}
		all = append(all, page.Items...)
		pages++
		bytes += w.Body.Len()
		if page.Complete {
			break
		}
		if pages > 4 {
			t.Fatal("pagination failed to finish")
		}
		input.Cursor = page.NextCursor
	}
	if len(all) != 300 || pages != 3 {
		t.Fatalf("incomplete history: %d messages in %d pages", len(all), pages)
	}
	for i, item := range all {
		if item.Reference.UID != uint32(300-i) || item.IsRead != (item.Reference.UID%3 == 0) {
			t.Fatalf("identity/read state: %+v", item)
		}
		if err := app.Management.Identity.VerifyMessageReference(t.Context(), *item.Reference); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("local TLS IMAP: 300 imported messages, %d pages, %d JSON bytes, %s total; scheduled monitor remained healthy", pages, bytes, time.Since(started).Round(time.Millisecond))
	after, _ := store.Checkpoint(t.Context(), mailbox.ID, "INBOX")
	currentEvents, _ := store.Recent(t.Context())
	if after != checkpoint || len(events) != len(currentEvents) {
		t.Fatal("history changed checkpoint/outbox")
	}
	health := f.do(t, "GET", appPrefix+"/mail/health", nil, headers)
	var statuses mailview.Page[mailview.Health]
	if health.Code != 200 || json.Unmarshal(health.Body.Bytes(), &statuses) != nil || statuses.Items[0].State != "healthy" || statuses.Items[0].LastCheckedAt == nil {
		t.Fatalf("health: %d %s", health.Code, health.Body.String())
	}
	// The emitted reference is directly usable by the existing lazy content route.
	content := f.do(t, "POST", appPrefix+"/content", map[string]any{"reference": all[0].Reference}, headers)
	if content.Code != 200 || !strings.Contains(content.Body.String(), "private-body-marker") {
		t.Fatal("history reference cannot open body", content.Code)
	}
	report := f.do(t, "GET", "/api/v1/diagnostics", nil, nil)
	for _, private := range []string{credential, all[0].Reference.Signature, "private-history-marker", "private-body-marker", input.Cursor, "synthetic@example.test"} {
		if strings.Contains(report.Body.String(), private) {
			t.Fatal("diagnostics exposed mail data")
		}
	}
	// A folder recreated between pages invalidates the old UID epoch.
	if err := user.Delete("INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	stale := f.do(t, "POST", appPrefix+"/mail/messages", input, headers)
	if stale.Code != 409 || !strings.Contains(stale.Body.String(), "sync_cursor_invalid") {
		t.Fatalf("old UIDVALIDITY accepted: %d %s", stale.Code, stale.Body.String())
	}
}
