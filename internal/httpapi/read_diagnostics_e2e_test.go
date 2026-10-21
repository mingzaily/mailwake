package httpapi

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/mingzaily/mailwake/internal/application"
	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

type readDiagnosticsSession struct {
	imapserver.Session
	reject *atomic.Bool
}

func (s readDiagnosticsSession) Select(name string, options *protocol.SelectOptions) (*protocol.SelectData, error) {
	data, err := s.Session.Select(name, options)
	if err == nil {
		// The memory server advertises flags currently in use. This fixture grants
		// persistent Seen changes before the first message has that flag.
		data.PermanentFlags = []protocol.Flag{protocol.FlagSeen, protocol.FlagFlagged}
	}
	return data, err
}

func (s readDiagnosticsSession) Store(w *imapserver.FetchWriter, set protocol.NumSet, flags *protocol.StoreFlags, options *protocol.StoreOptions) error {
	if s.reject.Load() {
		return &protocol.Error{Type: protocol.StatusResponseTypeNo, Code: protocol.ResponseCodeNoPerm, Text: "private provider explanation"}
	}
	return s.Session.Store(w, set, flags, options)
}

func TestReadDiagnosticsAcrossHTTPRuntimeAndIMAP(t *testing.T) {
	cert := httptest.NewTLSServer(nil)
	defer cert.Close()
	roots := x509.NewCertPool()
	roots.AddCert(cert.Certificate())
	listener, err := tls.Listen("tcp", "127.0.0.1:0", cert.TLS.Clone())
	if err != nil {
		t.Fatal(err)
	}
	user := imapmemserver.NewUser("private-user@example.test", "synthetic-password")
	if err = user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	backend := imapmemserver.New()
	backend.AddUser(user)
	var reject atomic.Bool
	server := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return readDiagnosticsSession{backend.NewSession(), &reject}, nil, nil
	}, Caps: protocol.CapSet{protocol.CapIMAP4rev1: {}}, Logger: log.New(io.Discard, "", 0)})
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(listener) }()
	defer func() { server.Close(); <-done }()
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	logger := logging.New(io.Discard)
	app, err := application.New(t.Context(), store, vault, logger, "", application.Options{IMAPRootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	auth, grant := testAdministrator(t, store)
	identity, err := app.Management.Identity.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f := appFixture{router: New(auth, app.Manager, store, logger, Integrations{Native: app.Native, Management: app.Management}), grant: grant, coreID: identity.ID, service: app.Management, log: logger}
	password := "synthetic-password"
	input := settings.MailboxUpdate{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Username: "private-user@example.test", Password: &password}
	mailbox, err := app.CreateMailbox(t.Context(), settings.MailboxCreate{MailboxUpdate: input, Folders: []mail.Folder{{Name: "INBOX", Check: "5m"}}})
	if err != nil {
		t.Fatal(err)
	}
	credential, _ := f.invite(t, []string{"content", "diagnostics"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	appendMessage := func() coreidentity.MessageReference {
		t.Helper()
		added, err := user.Append("INBOX", strings.NewReader("Content-Type: text/plain\r\n\r\nprivate-body-marker"), &protocol.AppendOptions{Flags: []protocol.Flag{protocol.FlagFlagged}})
		if err != nil {
			t.Fatal(err)
		}
		ref, err := identity.MessageReference(event.Notification{MailboxID: mailbox.ID, AccountID: (settings.Mailbox{ID: mailbox.ID, Host: input.Host, Port: input.Port, Username: input.Username}).Identity(), Folder: "INBOX", Location: &mail.Location{UIDValidity: added.UIDValidity, UID: uint32(added.UID)}})
		if err != nil {
			t.Fatal(err)
		}
		return *ref
	}
	reference := appendMessage()
	cached := f.do(t, "POST", appPrefix+"/content", map[string]any{"reference": reference}, headers)
	if cached.Code != 200 || !strings.Contains(cached.Body.String(), "private-body-marker") {
		t.Fatal(cached.Code, cached.Body.String())
	}
	check := func(ref coreidentity.MessageReference, status int, stage, outcome, code, response string) {
		t.Helper()
		result := f.do(t, "POST", appPrefix+"/content/read", map[string]any{"reference": ref}, headers)
		if result.Code != status {
			t.Fatal(result.Code, result.Body.String(), logging.AppRequests(logger))
		}
		report := f.do(t, "GET", "/api/v1/diagnostics", nil, nil)
		var data struct {
			Requests []logging.AppRequest `json:"app_requests"`
		}
		if report.Code != 200 || json.Unmarshal(report.Body.Bytes(), &data) != nil {
			t.Fatal(report.Code)
		}
		var matched *logging.AppRequest
		for i := range data.Requests {
			if data.Requests[i].RequestID == result.Header().Get("X-Request-ID") {
				matched = &data.Requests[i]
			}
		}
		if matched == nil || matched.Stage != stage || matched.Outcome != outcome || matched.Code != code || matched.ResponseCode != response {
			t.Fatalf("request=%+v", matched)
		}
		for _, secret := range []string{credential, ref.Signature, "private-body-marker", "private provider explanation", input.Username, "INBOX", mailbox.ID} {
			if strings.Contains(report.Body.String(), secret) {
				t.Fatalf("diagnostics leaked %q", secret)
			}
		}
	}
	check(reference, 204, "verify_seen", "marked_read", "", "")
	check(reference, 204, "read_flags", "already_read", "", "")
	reject.Store(true)
	check(appendMessage(), 503, "store_seen", "failed", "message_unavailable", "NOPERM")
	// Already-read requests are idempotent even while the provider rejects writes.
	check(reference, 204, "read_flags", "already_read", "", "")
	reject.Store(false)
	tampered := reference
	tampered.UID++
	check(tampered, 404, "reference", "failed", "message_not_found", "")
	if err = user.Delete("INBOX"); err != nil {
		t.Fatal(err)
	}
	check(reference, 404, "imap_select", "failed", "message_not_found", "NONEXISTENT")
}
