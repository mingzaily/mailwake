package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestHTTPSProxySetupLoginAndMutations(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := auth.New(store, slog.Default())
	code, err := service.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	router := New(service, newTestRuntime("", nil, nil), store, nil)
	call := func(method, path, body, origin string, g auth.Grant) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://mail.example.org"+path, strings.NewReader(body))
		req.RemoteAddr = "10.0.0.2:1234"
		if g.ID != "" {
			authorizeSession(req, g)
		}
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	setup := `{"code":"` + code + `","username":"admin","password":"proxy test password"}`
	for _, origin := range []string{"https://evil.example", "https://mail.example.org/path", "null", "https://mail.example.org#", "https://user@mail.example.org", "ftp://mail.example.org", "https://mail.example.org:443"} {
		if w := call("POST", "/api/v1/setup", setup, origin, auth.Grant{}); w.Code != 403 {
			t.Fatal(origin, w.Code)
		}
	}
	w := call("POST", "/api/v1/setup", setup, "https://mail.example.org", auth.Grant{})
	if w.Code != 201 || !w.Result().Cookies()[0].Secure {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call("POST", "/api/v1/session", `{"username":"admin","password":"proxy test password"}`, "https://mail.example.org", auth.Grant{})
	if w.Code != 200 || !w.Result().Cookies()[0].Secure {
		t.Fatal(w.Code, w.Body.String())
	}
	var grant auth.Grant
	if err := json.Unmarshal(w.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}
	grant.ID = w.Result().Cookies()[0].Value
	for _, origin := range []string{"https://evil.example", "https://mail.example.org/", "broken", ""} {
		if w := call("POST", "/api/v1/test-push", "", origin, grant); w.Code != 403 {
			t.Fatal(origin, w.Code)
		}
	}
	if w := call("POST", "/api/v1/test-push", "", "https://mail.example.org", grant); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call("DELETE", "/api/v1/session", "", "https://mail.example.org", grant)
	if w.Code != 204 || !w.Result().Cookies()[0].Secure || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("cookie clearing", w.Code)
	}
}

func TestProxyFailureBudgets(t *testing.T) {
	for _, peer := range []string{"203.0.113.9:1234", "127.0.0.2:1234", "[::1]:1234", "10.2.3.4:1234", "172.16.0.2:1234", "192.168.0.2:1234", "[fd00::2]:1234"} {
		t.Run(peer, func(t *testing.T) {
			store, err := storage.Open(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			service, _ := testAdministrator(t, store)
			router := New(service, newTestRuntime("", nil, nil), store, nil)
			call := func(client string) int {
				req := httptest.NewRequest("POST", "/api/v1/session", strings.NewReader(`{"username":"admin","password":"`+strings.Repeat("x", 1025)+`"}`))
				req.RemoteAddr = peer
				req.Header.Set("X-Forwarded-For", client)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w.Code
			}
			for range 10 {
				if code := call("198.51.100.1"); code != 401 {
					t.Fatal(code)
				}
			}
			if code := call("198.51.100.1"); code != 429 {
				t.Fatal("budget not enforced", code)
			}
			want := 401
			if strings.HasPrefix(peer, "203.") {
				want = 429
			}
			if code := call("198.51.100.2"); code != want {
				t.Fatal("proxy attribution", code, want)
			}
		})
	}
}
