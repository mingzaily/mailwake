package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/i18n"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

type failingSource struct{ err error }

func (s failingSource) Folders(context.Context) (*mail.FolderDiscovery, error) { return nil, s.err }
func (s failingSource) Open(context.Context, string) (mail.Session, error)     { return nil, s.err }

func TestLocalizedAPIAndDiagnosticCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	source := failingSource{err: fault.New("imap_connection_failed")}
	monitor := engine.New(source, store, "a", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Customers"})}, time.Minute, false, logger)
	done := make(chan struct{})
	go func() { defer close(done); monitor.Run(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(2 * time.Second)
	for monitor.Status()[0].LastError == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if monitor.Status()[0].LastError == nil {
		t.Fatal("monitor did not report the connection error")
	}
	if err := store.Enqueue(ctx, event.Notification{ID: "delivery"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Failed(ctx, "delivery", &fault.Error{Code: "bark_http_error", Params: map[string]string{"status": "503"}}, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	authService, grant := testAdministrator(t, store)
	router := New(authService, newTestRuntime("webhook", monitor, source), store, logger)
	for _, locale := range []string{"en", "zh-CN", "zh-Hant", "ja", "ko", "de", "fr", "es", "pt-BR", "fr-FR", "zh-Hans", "zh-TW", "it-IT"} {
		t.Run(locale, func(t *testing.T) {
			call := func(method, path string, auth bool) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, nil)
				req.Header.Set("Accept-Language", locale)
				if auth {
					authorizeSession(req, grant)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w
			}
			wantLocale := i18n.Match(locale)
			for _, path := range []string{"/locales/default.json", "/locales/" + wantLocale + ".json"} {
				response := call("GET", path, false)
				var catalog map[string]string
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &catalog) != nil || catalog["ui.save"] != i18n.Message(wantLocale, "ui.save", nil) || response.Header().Get("Content-Language") != wantLocale || !strings.Contains(response.Header().Get("Vary"), "Accept-Language") {
					t.Fatalf("catalog %s: %d %s", path, response.Code, response.Body.String())
				}
			}

			for _, tc := range []struct {
				method, path, code string
				status             int
				auth               bool
			}{
				{"GET", "/api/v1/status", "unauthorized", 401, false},
				{"GET", "/api/v1/mailboxes/mbx_test/folders", "imap_connection_failed", 502, true},
				{"POST", "/api/v1/deliveries/missing/retry", "delivery_retry_conflict", 409, true},
				{"GET", "/missing", "route_not_found", 404, false},
				{"DELETE", "/api/v1/status", "method_not_allowed", 405, true},
			} {
				w := call(tc.method, tc.path, tc.auth)
				var body struct {
					Error localizedError `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if w.Code != tc.status || body.Error.Code != tc.code || body.Error.Message != i18n.Message(wantLocale, tc.code, nil) || w.Header().Get("Content-Language") != wantLocale {
					t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
				}
			}
			page := call("GET", "/", false)
			if page.Code != 200 || page.Header().Get("Content-Language") != wantLocale {
				t.Fatal(page.Body.String())
			}
			var status struct {
				Channel string `json:"channel"`
				Folders []struct {
					LastError localizedError `json:"last_error"`
				} `json:"folders"`
			}
			if err := json.Unmarshal(call("GET", "/api/v1/status", true).Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if status.Channel != "webhook" || status.Folders[0].LastError.Code != "imap_connection_failed" || status.Folders[0].LastError.Message != i18n.Message(wantLocale, "imap_connection_failed", nil) {
				t.Fatal(status)
			}
			var recent struct {
				Deliveries []struct {
					LastError localizedError `json:"last_error"`
				} `json:"deliveries"`
			}
			if err := json.Unmarshal(call("GET", "/api/v1/deliveries", true).Body.Bytes(), &recent); err != nil {
				t.Fatal(err)
			}
			failure := recent.Deliveries[0].LastError
			if failure.Code != "bark_http_error" || failure.Params["status"] != "503" || failure.Message != i18n.Message(wantLocale, "bark_http_error", failure.Params) {
				t.Fatal(failure)
			}
		})
	}
	// Raw upstream error text stays private even when it contains credentials.
	router = New(authService, newTestRuntime("bark", monitor, failingSource{err: errors.New("password=private-secret")}), store, logger)
	req := httptest.NewRequest("GET", "/api/v1/mailboxes/mbx_test/folders", nil)
	authorizeSession(req, grant)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 502 || strings.Contains(w.Body.String(), "private-secret") || !strings.Contains(w.Body.String(), "imap_discovery_failed") {
		t.Fatal(w.Body.String())
	}
}

type folderSource struct{}

func (folderSource) Folders(context.Context) (*mail.FolderDiscovery, error) {
	return &mail.FolderDiscovery{Folders: []string{"Clients", "INBOX"}, FolderRoles: map[string]string{"INBOX": "inbox"}}, nil
}
func (folderSource) Open(context.Context, string) (mail.Session, error) {
	panic("本测试只发现文件夹")
}

func TestAuthenticationAndDurableTestPush(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	s, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	source := folderSource{}
	monitor := engine.New(source, s, "private-account", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Clients"})}, time.Minute, false, log)
	token := strings.Repeat("secret", 8)
	authService, grant := testAdministrator(t, s)
	router := New(authService, newTestRuntime("bark", monitor, source), s, log)
	discoveryRequest := httptest.NewRequest("GET", "/api/v1/mailboxes/mbx_test/folders", nil)
	authorizeSession(discoveryRequest, grant)
	discoveryResponse := httptest.NewRecorder()
	router.ServeHTTP(discoveryResponse, discoveryRequest)
	var discovery mail.FolderDiscovery
	if discoveryResponse.Code != 200 || json.Unmarshal(discoveryResponse.Body.Bytes(), &discovery) != nil || len(discovery.Folders) != 2 || discovery.Folders[0] != "Clients" || discovery.FolderRoles["INBOX"] != "inbox" {
		t.Fatalf("folder paths and roles: %s", discoveryResponse.Body.String())
	}

	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/status"}, {"GET", "/api/v1/mailboxes/mbx_test/folders"}, {"GET", "/api/v1/deliveries"}, {"POST", "/api/v1/test-push"}, {"POST", "/api/v1/deliveries/id/retry"}} {
		request := httptest.NewRequest(route.method, route.path, nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != 401 || strings.Contains(recorder.Body.String(), "Clients") {
			t.Fatalf("路由鉴权: %s %d %s", route.path, recorder.Code, recorder.Body.String())
		}
	}
	for _, path := range []string{"/", "/locales/default.json", "/healthz"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != 200 || strings.Contains(recorder.Body.String(), "private-account") || strings.Contains(recorder.Body.String(), token) {
			t.Fatalf("公开资源: %s %d", path, recorder.Code)
		}
	}
	request := httptest.NewRequest("POST", "/api/v1/test-push", nil)
	authorizeSession(request, grant)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != 400 {
		t.Fatalf("测试发送失败应原样返回: %d %s", recorder.Code, recorder.Body.String())
	}
	task, err := s.Due(ctx, time.Now().Add(time.Second))
	if err != nil || task != nil {
		t.Fatalf("手动测试应独立于邮件队列: %+v %v", task, err)
	}
}

func (s failingSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}

func (folderSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}
