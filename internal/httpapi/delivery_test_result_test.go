package httpapi

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

type savedTestRuntime struct {
	Runtime
	result error
}

func (r savedTestRuntime) TestSavedDelivery(context.Context) error { return r.result }

func TestSavedPushReturnsAcceptedOrLocalizedFailure(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auth, grant := testAdministrator(t, store)
	for _, tc := range []struct {
		err     error
		status  int
		message string
	}{
		{nil, 204, ""},
		{fault.New("apns_device_invalid"), 400, "APNs 拒绝了设备 Token"},
		{&fault.Error{Code: "rate_limited", Params: map[string]string{"retry_after": "60"}}, 429, "rate_limited"},
	} {
		router := New(auth, savedTestRuntime{Runtime: newTestRuntime("native", nil, nil), result: tc.err}, store, slog.Default())
		req := httptest.NewRequest("POST", "/api/v1/test-push", nil)
		authorizeSession(req, grant)
		req.Header.Set("Accept-Language", "zh-CN")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.message) {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.status == 429 && w.Header().Get("Retry-After") != "60" {
			t.Fatal("retry delay missing")
		}
	}
}
