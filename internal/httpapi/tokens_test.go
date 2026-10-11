package httpapi

import (
	"encoding/json"

	"github.com/mingzaily/mailwake/internal/storage"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenLifecycle(t *testing.T) {
	s, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, g := testAdministrator(t, s)
	r := New(a, newTestRuntime("", nil, nil), s, nil)
	req := httptest.NewRequest("POST", "/api/v1/tokens", strings.NewReader(`{"name":"automation"}`))
	authorizeSession(req, g)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var issued tokenView
	if err := json.Unmarshal(w.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issued.Value, "mwk_") {
		t.Fatal("prefix")
	}
	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+issued.Value)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	w = call("POST", "/api/v1/test-push")
	if w.Code != 400 {
		t.Fatal("token requires CSRF", w.Code, w.Body.String())
	}
	w = call("GET", "/api/v1/tokens")
	if w.Code != 200 || strings.Contains(w.Body.String(), issued.Value) || strings.Contains(w.Body.String(), "token_hash") {
		t.Fatal("token disclosure", w.Code)
	}
	tokens, err := a.Tokens(t.Context())
	if err != nil || len(tokens) != 1 || tokens[0].LastUsedAt == nil {
		t.Fatal("last use", err)
	}
	w = call("DELETE", "/api/v1/tokens/"+issued.ID)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	w = call("GET", "/api/v1/tokens")
	if w.Code != 401 {
		t.Fatal("revoked token usable", w.Code)
	}
}
