package httpapi

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
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/runtime"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestMailboxHTTPWithRealRuntime(t *testing.T) {
	// Isolate fallback roots in a child test process, keeping the normal trust
	// store untouched for other tests and repeated runs.
	if os.Getenv("MAILWAKE_TEST_TLS_FIXTURE") != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMailboxHTTPWithRealRuntime$")
		command.Env = append(os.Environ(), "MAILWAKE_TEST_TLS_FIXTURE=1", "GODEBUG=x509usefallbackroots=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("TLS integration: %v\n%s", err, output)
		}
		return
	}
	cert := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer cert.Close()
	pool := x509.NewCertPool()
	pool.AddCert(cert.Certificate())
	x509.SetFallbackRoots(pool)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", cert.TLS.Clone())
	if err != nil {
		t.Fatal(err)
	}
	backend := imapmemserver.New()
	user := imapmemserver.NewUser("user@example.test", "synthetic-password")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	backend.AddUser(user)
	for i := 1; i < 20; i++ {
		u := imapmemserver.NewUser(fmt.Sprintf("user%d@example.test", i), "synthetic-password")
		if err := u.Create("INBOX", nil); err != nil {
			t.Fatal(err)
		}
		backend.AddUser(u)
	}
	server := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return backend.NewSession(), nil, nil
	}, Caps: protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapIdle: {}}, Logger: log.New(io.Discard, "", 0)})
	serverDone := make(chan struct{})
	go func() { defer close(serverDone); server.Serve(listener) }()
	defer func() { server.Close(); <-serverDone }()
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := runtime.New(t.Context(), store, vault, logger, "")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	auth, grant := testAdministrator(t, store)
	router := New(auth, manager, store, logger)
	call := func(method, path string, body any, status int) map[string]any {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(string(raw)))
		authorizeSession(req, grant)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: want %d, got %d %s", method, path, status, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "synthetic-password") || strings.Contains(w.Body.String(), "synthetic-signing-secret") {
			t.Fatal("API exposed credential", w.Body.String())
		}
		response := map[string]any{}
		if status != 204 {
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
		}
		return response
	}
	input := map[string]any{"host": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port, "username": "user@example.test", "password": "synthetic-password", "label": "Work-private-label", "connection_limit": 2}
	call("POST", "/mailboxes/test", input, 200)
	if boxes := call("GET", "/mailboxes", nil, 200)["mailboxes"].([]any); len(boxes) != 0 {
		t.Fatal("test saved mailbox")
	}
	a := call("POST", "/mailboxes", input, 201)
	id := a["id"].(string)
	if !strings.HasPrefix(id, "mbx_") || a["revision"] != float64(1) || a["password"].(map[string]any)["configured"] != true {
		t.Fatal(a)
	}
	if code := call("POST", "/mailboxes", input, 409)["error"].(map[string]any)["code"]; code != "mailbox_duplicate" {
		t.Fatal(code)
	}
	input["username"] = "user1@example.test"
	input["label"] = "Personal-private-label"
	b := call("POST", "/mailboxes", input, 201)
	other := b["id"].(string)
	if id == other {
		t.Fatal("duplicate mailbox ID")
	}
	input["username"] = "user@example.test"
	delete(input, "password")
	call("POST", "/mailboxes/"+id+"/test", input, 200)
	if code := call("POST", "/mailboxes/test", input, 400)["error"].(map[string]any)["code"]; code != "credential_required" {
		t.Fatal(code)
	}
	for _, box := range []string{id, other} {
		call("GET", "/mailboxes/"+box+"/folders", nil, 200)
		state := call("PUT", "/mailboxes/"+box+"/subscriptions", map[string]any{"revision": 1, "folders": mail.RealtimeFolders([]string{"INBOX"})}, 202)
		if state["revision"] != float64(2) {
			t.Fatal(state)
		}
	}
	if code := call("PUT", "/mailboxes/"+id+"/subscriptions", map[string]any{"revision": 1, "folders": []string{}}, 409)["error"].(map[string]any)["code"]; code != "subscriptions_conflict" {
		t.Fatal(code)
	}
	input["revision"] = 1
	input["label"] = "Updated-private-label"
	if view := call("PUT", "/mailboxes/"+id, input, 200); view["revision"] != float64(2) {
		t.Fatal(view)
	}
	if code := call("PUT", "/mailboxes/"+id, input, 409)["error"].(map[string]any)["code"]; code != "settings_conflict" {
		t.Fatal(code)
	}
	if view := call("GET", "/mailboxes/"+other, nil, 200); view["revision"] != float64(1) {
		t.Fatal("another mailbox revision advanced", view)
	}
	delivery := map[string]any{"revision": 0, "channel": "webhook", "preview": "off", "webhook": map[string]any{"url": cert.URL, "secret": "synthetic-signing-secret-long-enough"}}
	delivery["preview"] = "sender_subject"
	if code := call("PUT", "/settings/delivery", delivery, 400)["error"].(map[string]any)["code"]; code != "config_preview_invalid" {
		t.Fatal(code)
	}
	delivery["preview"] = "off"
	call("PUT", "/settings/delivery", delivery, 200)
	if code := call("PUT", "/settings/delivery", delivery, 409)["error"].(map[string]any)["code"]; code != "settings_conflict" {
		t.Fatal(code)
	}
	delete(delivery, "revision")
	call("PUT", "/settings/delivery", delivery, 400)
	status := call("GET", "/status", nil, 200)
	folders := status["folders"].([]any)
	if len(folders) != 2 {
		t.Fatal(status)
	}
	labels := map[string]string{id: "Updated-private-label", other: "Personal-private-label"}
	for _, raw := range folders {
		folder := raw.(map[string]any)
		if folder["check"] != "realtime" || folder["next_check"] != nil || folder["mailbox_label"] != labels[folder["mailbox_id"].(string)] {
			t.Fatal("folder identity missing", folder)
		}
	}
	report := call("GET", "/diagnostics", nil, 200)
	encoded, _ := json.Marshal(report)
	for _, private := range []string{id, other, "private-label", "127.0.0.1", "user@example.test", "INBOX"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("diagnostics exposed mailbox identity", private)
		}
	}
	watches := report["watches"].([]any)
	if len(watches) != 2 || watches[0].(map[string]any)["mailbox_index"] == watches[1].(map[string]any)["mailbox_index"] {
		t.Fatal("diagnostics merged mailboxes", watches)
	}
	for _, raw := range watches {
		if raw.(map[string]any)["check"] != "realtime" {
			t.Fatal("diagnostic check missing", raw)
		}
	}
	// The old routes are intentionally absent before release.
	for _, route := range []struct{ method, path string }{{"GET", "/settings/mailbox"}, {"PUT", "/settings/mailbox"}, {"POST", "/settings/mailbox/test"}, {"GET", "/folders"}, {"GET", "/subscriptions"}, {"PUT", "/subscriptions"}} {
		call(route.method, route.path, nil, 404)
	}
	input["password"] = "synthetic-password"
	delete(input, "revision")
	for i := 2; i < 20; i++ {
		input["label"] = fmt.Sprintf("mailbox %d", i)
		input["username"] = fmt.Sprintf("user%d@example.test", i)
		call("POST", "/mailboxes", input, 201)
	}
	if code := call("POST", "/mailboxes", input, 400)["error"].(map[string]any)["code"]; code != "mailbox_limit_exceeded" {
		t.Fatal(code)
	}
	call("DELETE", "/mailboxes/"+id, nil, 204)
	for _, suffix := range []string{"", "/folders", "/subscriptions"} {
		if code := call("GET", "/mailboxes/"+id+suffix, nil, 404)["error"].(map[string]any)["code"]; code != "mailbox_not_found" {
			t.Fatal(code)
		}
	}
	call("DELETE", "/mailboxes/"+id, nil, 404)
	if state := call("GET", "/mailboxes/"+other+"/subscriptions", nil, 200); state["revision"] != float64(2) {
		t.Fatal("delete changed another subscription", state)
	}
	// A second mailbox remains live after the first is removed.
	deadline := time.Now().Add(2 * time.Second)
	for {
		state := manager.Status()
		if len(state) == 1 && state[0].State == "watching" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(state)
		}
		time.Sleep(time.Millisecond)
	}
}
