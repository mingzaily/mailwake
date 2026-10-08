package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/i18n"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestAuthenticationInputErrorContracts(t *testing.T) {
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
	router := New(service, newTestRuntime("", nil, nil), store, slog.Default())
	call := func(method, path, body string, grant auth.Grant) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Accept-Language", "zh-CN")
		if grant.ID != "" {
			authorizeSession(request, grant)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	check := func(response *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		var result struct{ Error localizedError }
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != status || result.Error.Code != code || result.Error.Message != i18n.Message("zh-CN", code, result.Error.Params) {
			t.Fatalf("want %d %s, got %d %s", status, code, response.Code, response.Body.String())
		}
	}
	check(call("POST", "/api/v1/setup", `{"code":"incorrect","username":"admin","password":"synthetic password"}`, auth.Grant{}), 400, "setup_code_invalid")
	grant, err := service.Setup(t.Context(), "bootstrap", code, "admin", "synthetic password")
	if err != nil {
		t.Fatal(err)
	}
	check(call("POST", "/api/v1/session", `{"username":"admin","password":"incorrect"}`, auth.Grant{}), 401, "unauthorized")
	// The setup and login failures above also use this source's ten-attempt budget.
	for range 8 {
		check(call("PUT", "/api/v1/admin/password", `{"current_password":"incorrect","password":"replacement password"}`, grant), 400, "current_password_invalid")
	}
	response := call("PUT", "/api/v1/admin/password", `{"current_password":"incorrect","password":"replacement password"}`, grant)
	check(response, 429, "too_many_attempts")
	if response.Header().Get("Retry-After") == "" {
		t.Fatal("retry delay missing")
	}
	if response := call("GET", "/api/v1/session", "", grant); response.Code != 200 {
		t.Fatal("input error revoked session", response.Code)
	}
	check(call("GET", "/api/v1/session", "", auth.Grant{}), 401, "unauthorized")
}
