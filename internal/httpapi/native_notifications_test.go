package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestNativeNotificationPreferencesRequireCurrentController(t *testing.T) {
	f := newAppFixture(t)
	credential, _ := f.invite(t, []string{"diagnostics"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	for _, tc := range []struct {
		method, path string
		body         any
		status       int
		contains     string
	}{
		{"GET", "/capabilities", nil, 200, `"activity_notifications":{"version":1}`},
		{"GET", "/native/notification-preferences", nil, 200, `"omit_activity_notification":false`},
		{"PUT", "/native/notification-preferences", map[string]any{}, 400, `"code":"request_invalid"`},
		{"PUT", "/native/notification-preferences", map[string]any{"omit_activity_notification": true}, 200, `"omit_activity_notification":true`},
		{"GET", "/native/notification-preferences", nil, 200, `"omit_activity_notification":true`},
		{"GET", "/native/notifications", nil, 200, `"items":[]`},
		{"DELETE", "/native/notifications/other/event", nil, 204, ""},
		{"DELETE", "/controller", nil, 204, ""},
		{"GET", "/native/notification-preferences", nil, 401, `"code":"unauthorized"`},
		{"GET", "/native/notifications", nil, 401, `"code":"unauthorized"`},
		{"PUT", "/native/notification-preferences", map[string]any{"omit_activity_notification": true}, 401, `"code":"unauthorized"`},
		{"DELETE", "/native/notifications/other/event", nil, 401, `"code":"unauthorized"`},
	} {
		w := f.do(t, tc.method, appPrefix+tc.path, tc.body, headers)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestNativeNotificationCatchUpReturnsOnlyAddressedCiphertext(t *testing.T) {
	f := newAppFixture(t)
	credential, deviceID := f.invite(t, []string{"diagnostics"})
	otherCredential, _ := f.invite(t, []string{"diagnostics"})
	headers := map[string]string{"Authorization": "Bearer " + credential}
	otherHeaders := map[string]string{"Authorization": "Bearer " + otherCredential}
	store := f.service.Store
	pairing := storage.NativePairing{ID: "pairing", State: "waiting", TokenHash: "test", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := store.CreateNativePairing(t.Context(), pairing); err != nil {
		t.Fatal(err)
	}
	pairing.DeviceID = deviceID
	if err := store.SelectNativeDevice(t.Context(), pairing); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishNativePairing(t.Context(), pairing.ID, "active", ""); err != nil {
		t.Fatal(err)
	}
	if w := f.do(t, "PUT", appPrefix+"/native/notification-preferences", map[string]any{"omit_activity_notification": true}, headers); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := store.Enqueue(t.Context(), event.Notification{ID: "event"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveNativePlan(t.Context(), []storage.NativeDelivery{{EventID: "event", PairingID: "pairing", Envelope: `{"ciphertext":"encrypted"}`, Signature: "signature"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.NativeActivityNotification(t.Context(), "event", "pairing", "activity", `{"ciphertext":"encrypted"}`, "signature"); err != nil {
		t.Fatal(err)
	}
	path := appPrefix + "/native/notifications"
	if w := f.do(t, "GET", path, nil, otherHeaders); w.Code != 200 || w.Body.String() != `{"items":[]}` {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.do(t, "DELETE", path+"/pairing/event", nil, otherHeaders); w.Code != 204 {
		t.Fatal(w.Code)
	}
	for range 2 {
		w := f.do(t, "GET", path, nil, headers)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"envelope":{"ciphertext":"encrypted"}`) || !strings.Contains(w.Body.String(), `"event_id":"event"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for range 2 {
		if w := f.do(t, "DELETE", path+"/pairing/event", nil, headers); w.Code != 204 {
			t.Fatal(w.Code)
		}
	}
	if w := f.do(t, "GET", path, nil, headers); w.Code != 200 || w.Body.String() != `{"items":[]}` {
		t.Fatal(w.Code, w.Body.String())
	}
}
