package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestSubscriptionAPIAndPrivateDiagnostics(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	initial, err := seedSubscriptions(t.Context(), store, "private-account", []string{"private-folder"})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	monitor := engine.New(folderSource{}, store, "private-account", initial, time.Minute, false, logger)
	authService, grant := testAdministrator(t, store)
	router := New(authService, newTestRuntime("bark", monitor, folderSource{}), store, logger)
	call := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Accept-Language", "zh-CN")
		if auth {
			authorizeSession(req, grant)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/mailboxes/mbx_test/subscriptions"}, {"PUT", "/api/v1/mailboxes/mbx_test/subscriptions"}, {"GET", "/api/v1/diagnostics"}} {
		if w := call(route.method, route.path, "", false); w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{"revision":1,"folders":["INBOX"]}`, `{"revision":1,"folders":[{"name":"INBOX"}]}`, `{"revision":1,"folders":[{"name":"INBOX","check":"hourly"}]}`, `{}`, `null`, `{"revision":1,"folders":null}`, `{"revision":1,"folders":[],"extra":true}`, `{"revision":1,"folders":[]} {}`, `{"revision":1,"folders":[{"name": "INBOX", "check": "realtime"}, {"name": "inbox", "check": "realtime"}]}`, `{"revision":1,"folders":[{"name": "a", "check": "realtime"}, {"name": "b", "check": "realtime"}, {"name": "c", "check": "realtime"}, {"name": "d", "check": "realtime"}, {"name": "e", "check": "realtime"}, {"name": "f", "check": "realtime"}, {"name": "g", "check": "realtime"}, {"name": "h", "check": "realtime"}, {"name": "i", "check": "realtime"}, {"name": "j", "check": "realtime"}, {"name": "k", "check": "realtime"}]}`, `{"revision":1,"folders":[{"name": "a\n", "check": "realtime"}]}`} {
		if w := call("PUT", "/api/v1/mailboxes/mbx_test/subscriptions", body, true); w.Code != 400 {
			t.Fatalf("invalid body: %d %s", w.Code, w.Body.String())
		}
	}
	if w := call("PUT", "/api/v1/mailboxes/mbx_test/subscriptions", strings.Repeat(" ", 65537), true); w.Code != 413 {
		t.Fatalf("oversized body: %d", w.Code)
	}
	// Exactly one writer with the same revision succeeds.
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- call("PUT", "/api/v1/mailboxes/mbx_test/subscriptions", `{"revision":2,"folders":[{"name": "inbox", "check": "realtime"}]}`, true).Code
		}()
	}
	wg.Wait()
	a, b := <-results, <-results
	if !((a == 202 && b == 409) || (a == 409 && b == 202)) {
		t.Fatal(a, b)
	}
	w := call("GET", "/api/v1/mailboxes/mbx_test/subscriptions", "", true)
	var state mail.Subscriptions
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil || state.Revision != 3 || len(state.Folders) != 1 || state.Folders[0].Name != "INBOX" {
		t.Fatal(w.Body.String(), err)
	}
	if err := store.Enqueue(t.Context(), event.Notification{ID: "private-id", Subject: "private-subject", Sender: "private@email.test"}); err != nil {
		t.Fatal(err)
	}
	w = call("GET", "/api/v1/diagnostics", "", true)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(w.Code, w.Header())
	}
	for _, secret := range []string{"secret-token", "INBOX", "private-folder", "private-account", "private-id", "private-subject", "private@email.test"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("diagnostic export leaked %q", secret)
		}
	}
	var report map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil || report["schema_version"] != float64(1) || report["build"] == nil {
		t.Fatal(report, err)
	}
	store.Close()
	w = call("PUT", "/api/v1/mailboxes/mbx_test/subscriptions", `{"revision":3,"folders":[]}`, true)
	if w.Code != 503 || monitor.Subscriptions().Revision != 3 {
		t.Fatalf("failed persistence changed desired state: %d %+v", w.Code, monitor.Subscriptions())
	}
}

// seedSubscriptions saves folders as the second revision of an account.
func seedSubscriptions(ctx context.Context, store *storage.Store, account string, folders []string) (mail.Subscriptions, error) {
	if _, err := store.LoadSubscriptions(ctx, account); err != nil {
		return mail.Subscriptions{}, err
	}
	if err := store.SaveSubscriptions(ctx, account, mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders(folders)}); err != nil {
		return mail.Subscriptions{}, err
	}
	return store.LoadSubscriptions(ctx, account)
}
