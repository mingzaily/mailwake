package httpapi

import (
	"encoding/json"
	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/storage"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogsAuthenticationAndFilters(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	log := logging.New(io.Discard)
	a := auth.New(store, log)
	router := New(a, newTestRuntime("", nil, nil), store, log)
	var grant auth.Grant
	call := func(path, token string, session bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if session {
			authorizeSession(r, grant)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := call("/api/v1/logs", "", false); w.Code != 403 {
		t.Fatal("setup gate", w.Code)
	}
	code, err := a.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	grant, err = a.Setup(t.Context(), "peer", code, "admin", "synthetic-log-password")
	if err != nil {
		t.Fatal(err)
	}
	token, err := a.CreateToken(t.Context(), "log reader")
	if err != nil {
		t.Fatal(err)
	}
	log.Info("Visible event", "mailbox_id", "mbx_a", "folder", "INBOX")
	log.Warn("Temporary failure", "mailbox_id", "mbx_a", "code", "imap_connection_failed")
	log.Error("Other failure", "mailbox_id", "mbx_b")
	for _, value := range []string{"", "invalid"} {
		if w := call("/api/v1/logs", value, false); w.Code != 401 {
			t.Fatal("logs authentication", w.Code)
		}
	}
	for _, useSession := range []bool{false, true} {
		value := ""
		if !useSession {
			value = token.Value
		}
		w := call("/api/v1/logs?after=1&level=warn&mailbox_id=mbx_a&limit=1", value, useSession)
		var p logging.Page
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || len(p.Entries) != 1 || p.Entries[0].Message != "Temporary failure" || p.Next != 3 {
			t.Fatal("log filters", w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"after=-1", "after=bad", "level=debug", "limit=0", "limit=501"} {
		if w := call("/api/v1/logs?"+query, "", true); w.Code != 400 || !strings.Contains(w.Body.String(), "logs_request_invalid") {
			t.Fatal("query validation", query, w.Code)
		}
	}
	if w := call("/api/v1/diagnostics", "", true); w.Code != 200 || strings.Contains(w.Body.String(), "Visible event") || strings.Contains(w.Body.String(), `"logs"`) {
		t.Fatal("diagnostics includes logs")
	}
}
