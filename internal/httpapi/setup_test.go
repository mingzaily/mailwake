package httpapi

import (
	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/storage"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAdministrator(t *testing.T, s *storage.Store) (*auth.Service, auth.Grant) {
	t.Helper()
	a := auth.New(s, slog.Default())
	code, err := a.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	g, err := a.Setup(t.Context(), "test-source", code, "admin", "test password long enough")
	if err != nil {
		t.Fatal(err)
	}
	return a, g
}
func authorizeSession(r *http.Request, g auth.Grant) {
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: g.ID})
	r.Header.Set("X-CSRF-Token", g.CSRF)
	r.Header.Set("Origin", "http://"+r.Host)
}
func TestSetupGateAndOneTimeCode(t *testing.T) {
	s, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := auth.New(s, slog.Default())
	code, err := a.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r := New(a, newTestRuntime("", nil, nil), s, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/status", nil))
	if w.Code != 403 || !strings.Contains(w.Body.String(), "setup_required") {
		t.Fatal(w.Code, w.Body.String())
	}
	request := func(code string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/setup", strings.NewReader(`{"code":"`+code+`","username":"admin","password":"long setup password"}`)))
		return w
	}
	if w := request("wrong"); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(code)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
		t.Fatal(cookies)
	}
	if w := request(code); w.Code != 409 {
		t.Fatal("setup repeated", w.Code)
	}
	if code, err := a.Prepare(t.Context()); err != nil || code != "" {
		t.Fatal("setup reopened", err)
	}
}
