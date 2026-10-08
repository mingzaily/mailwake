package httpapi

import (
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestSessionSecurityAndPasswordChange(t *testing.T) {
	s, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, first := testAdministrator(t, s)
	r := New(a, newTestRuntime("", nil, nil), s, nil)
	call := func(method, path, body string, g auth.Grant, csrf, origin string, https bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if https {
			req.TLS = &tls.ConnectionState{}
		}
		if g.ID != "" {
			authorizeSession(req, g)
		}
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	login := call("POST", "/api/v1/session", `{"username":"admin","password":"test password long enough"}`, auth.Grant{}, "", "", true)
	if login.Code != 200 || !login.Result().Cookies()[0].Secure {
		t.Fatal(login.Code, login.Body.String())
	}
	var second auth.Grant
	if err := json.Unmarshal(login.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	second.ID = login.Result().Cookies()[0].Value
	body := `{"current_password":"test password long enough","password":"another valid password"}`
	for _, tc := range []struct{ csrf, origin string }{{"", "http://example.com"}, {first.CSRF, "http://evil.test"}, {first.CSRF, ""}} {
		if w := call("PUT", "/api/v1/admin/password", body, first, tc.csrf, tc.origin, false); w.Code != 403 {
			t.Fatal("CSRF accepted", w.Code, w.Body.String())
		}
	}
	if w := call("PUT", "/api/v1/admin/password", body, first, first.CSRF, "http://example.com", false); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("GET", "/api/v1/session", "", second, "", "", false); w.Code != 401 {
		t.Fatal("other session survived", w.Code)
	}
	if w := call("GET", "/api/v1/session", "", first, "", "", false); w.Code != 200 || !strings.Contains(w.Body.String(), first.CSRF) {
		t.Fatal("current session lost", w.Code)
	}
	if w := call("DELETE", "/api/v1/session", "", first, first.CSRF, "http://example.com", false); w.Code != 204 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout", w.Code)
	}
	if w := call("GET", "/api/v1/session", "", first, "", "", false); w.Code != 401 {
		t.Fatal("logout session usable", w.Code)
	}
}

func TestFailedAttemptLimitAndForwardedHeaders(t *testing.T) {
	for _, setup := range []bool{true, false} {
		t.Run(map[bool]string{true: "setup", false: "login"}[setup], func(t *testing.T) {
			s, err := storage.Open(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			a := auth.New(s, slog.Default())
			path, body := "/api/v1/setup", `{"code":"wrong","username":"admin","password":"long setup password"}`
			if setup {
				if _, err := a.Prepare(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else {
				a, _ = testAdministrator(t, s)
				path = "/api/v1/session"
				body = `{"username":"admin","password":"wrong"}`
			}
			r := New(a, newTestRuntime("", nil, nil), s, nil)
			for i := 0; i < 11; i++ {
				req := httptest.NewRequest("POST", path, strings.NewReader(body))
				req.Header.Set("X-Forwarded-For", string(rune('a'+i)))
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				want := 401
				if setup {
					want = 400
				}
				if i == 10 {
					want = 429
				}
				if w.Code != want {
					t.Fatal(i, w.Code, w.Body.String())
				}
				if i == 10 && w.Header().Get("Retry-After") == "" {
					t.Fatal("retry-after missing")
				}
			}
		})
	}
}
