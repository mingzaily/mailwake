package native

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestFixedNotificationUsesSignedPairingTestAndReportsOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, status, relayCode, want string
		http                          int
	}{
		{"accepted", "accepted", "", "", 200},
		{"rejected", "rejected", "apns_device_invalid", "apns_device_invalid", 200},
		{"unknown", "unknown", "apns_timeout", "apns_timeout", 200},
		{"sending", "sending", "", "native_test_sending", 200},
		{"recorded-rejection", "rejected", "", "native_test_rejected", 200},
		{"rate-limit", "", "rate_limited", "rate_limited", 429},
		{"forbidden", "", "forbidden", "native_relay_error", 403},
		{"malformed", "not-a-status", "", "native_protocol_invalid", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := storage.Open(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			vault, err := settings.OpenVault(t.TempDir(), false)
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			var service *Service
			target := addDevice(t, store, "test-device", vector(t).Encryption.PublicKey)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/info" {
					json.NewEncoder(w).Encode(Info{Audience: "test-relay", Environment: "sandbox", Version: 2})
					return
				}
				if r.URL.Path != "/v1/pairing-tests" || r.Method != "POST" {
					t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
					return
				}
				raw, _ := io.ReadAll(r.Body)
				var body map[string]string
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Error(err)
				}
				if len(body) != 2 || body["pairing_id"] != target {
					t.Errorf("unexpected test payload: %v", body)
				}
				if _, err := uuid.Parse(body["test_id"]); err != nil {
					t.Error(err)
				}
				identity, _ := service.CoreIdentity(t.Context())
				h := r.Header
				if h.Get("X-Relay-Role") != "core" || h.Get("X-Relay-ID") != identity.ID {
					t.Error("missing Core identity")
				}
				if err := verify(identity.PublicKey, h.Get("X-Relay-Signature"), requestText("test-relay", "core", identity.ID, r.Method, r.URL.Path, h.Get("X-Relay-Timestamp"), h.Get("X-Relay-Nonce"), raw, "")); err != nil {
					t.Error(err)
				}
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(tc.http)
				if tc.http != 200 {
					json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": tc.relayCode, "message": "private-server-body"}})
					return
				}
				json.NewEncoder(w).Encode(map[string]string{"test_id": body["test_id"], "status": tc.status, "error_code": tc.relayCode})
			}))
			defer server.Close()
			service, err = New(store, vault, server.URL, logging.New(&logs))
			if err != nil {
				t.Fatal(err)
			}
			err = service.Test(t.Context(), target)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			failure := fault.From(err, "")
			if failure.Code != tc.want {
				t.Fatal(failure)
			}
			if tc.http == 429 && failure.Params["retry_after"] != "60" {
				t.Fatal(failure)
			}
			if tc.relayCode != "" && (!strings.Contains(logs.String(), `"relay_code":"`+tc.relayCode+`"`) || !strings.Contains(logs.String(), `"http_status":`)) {
				t.Fatal("missing safe diagnostics", logs.String())
			}
			if strings.Contains(logs.String(), "private-server-body") {
				t.Fatal("response body logged")
			}
		})
	}
}
