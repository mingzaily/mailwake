package httpapi

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/storage"
)

func TestNativeRoutesKeepAuthenticationAndUnavailableBoundary(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auth, grant := testAdministrator(t, store)
	router := New(auth, newTestRuntime("", nil, nil), store, slog.Default())
	for _, route := range []struct{ method, path string }{{"POST", "/api/v1/native/pairings"}, {"GET", "/api/v1/native/pairings/test"}, {"GET", "/api/v1/native/devices"}, {"DELETE", "/api/v1/native/devices/test"}} {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatal(route, w.Code)
		}
		req = httptest.NewRequest(route.method, route.path, nil)
		authorizeSession(req, grant)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 503 || !strings.Contains(w.Body.String(), "native_push_unavailable") {
			t.Fatal(route, w.Code, w.Body.String())
		}
	}
}

func TestNativeOverviewRequiresActiveDevice(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auth, grant := testAdministrator(t, store)
	runtime := newTestRuntime("native", nil, nil)
	router := New(auth, runtime, store, slog.Default())
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	authorizeSession(req, grant)
	req.Header.Set("Accept-Language", "zh-CN")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "native_target_unavailable") || !strings.Contains(w.Body.String(), "接收设备已失效，请重新选择。") {
		t.Fatal(w.Code, w.Body.String())
	}
}
