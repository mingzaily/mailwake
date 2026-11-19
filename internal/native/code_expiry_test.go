package native

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/event"
)

func TestActivityWindowPreservesRealCodeExpiry(t *testing.T) {
	v := vector(t)
	service, store := testService(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	pairing := addDevice(t, store, "expiry-device", v.Encryption.PublicKey)
	devices, err := store.NativeDevices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := service.identity.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                  string
		age, lifetime, window time.Duration
	}{
		{"unknown validity", 0, 0, 10 * time.Minute},
		{"short validity", 0, 5 * time.Minute, 5 * time.Minute},
		{"one hour ceiling", 0, 2 * time.Hour, time.Hour},
		{"delayed delivery", 2 * time.Minute, 5 * time.Minute, 5 * time.Minute},
		{"expired code", 2 * time.Minute, time.Minute, 0},
		{"expired unknown window", 11 * time.Minute, 0, 0},
		{"expired presentation ceiling", 2 * time.Hour, 3 * time.Hour, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := time.Now().Add(-tc.age).Truncate(time.Second)
			n := event.Notification{ID: "expiry-event", AccountID: "expiry-account", MailboxID: "mailbox", Account: "Work",
				Folder: "Clients", Sender: "sender@example.org", Subject: "Your login code", Code: "482913", ReceivedAt: received}
			if tc.lifetime > 0 {
				expiry := received.Add(tc.lifetime)
				n.CodeExpiresAt = &expiry
			}
			activities, err := service.planActivities(n, devices, identity, v.Audience)
			if err != nil {
				t.Fatal(err)
			}
			if tc.window == 0 {
				if len(activities) != 0 {
					t.Fatal("expired presentation created an activity")
				}
				return
			}
			if len(activities) != 1 {
				t.Fatalf("activity count=%d, want one", len(activities))
			}
			var request activityRequest
			if err := json.Unmarshal([]byte(activities[0].Request), &request); err != nil {
				t.Fatal(err)
			}
			want := received.Add(tc.window).Unix()
			if request.ExpiresAt != want || activities[0].ExpiresAt != want {
				t.Fatalf("activity expiry=%d, persisted expiry=%d, want %d", request.ExpiresAt, activities[0].ExpiresAt, want)
			}
			rawContext, err := json.Marshal([]any{"mailwake-activity-v1", pairing, n.ID, "start", n.ID, request.ReceivedAt, request.ExpiresAt, request.UpdatedAt})
			if err != nil {
				t.Fatal(err)
			}
			contextID := hash(rawContext)
			if err := coreidentity.Verify(identity.PublicKey, request.Signature, contentText(v.Audience, pairing, contextID, request.Envelope)); err != nil {
				t.Fatal(err)
			}
			var decrypted activityContent
			if err := json.Unmarshal(decryptNotification(t, v, pairing, contextID, request.Envelope), &decrypted); err != nil {
				t.Fatal(err)
			}
			if decrypted.ExpiresAt != want {
				t.Fatal("encrypted activity window differs")
			}
			expiry := decrypted.Notification.CodeExpiresAt
			if tc.lifetime == 0 {
				if expiry != nil {
					t.Fatal("unknown code expiry received a real deadline")
				}
			} else if expiry == nil || !expiry.Equal(*n.CodeExpiresAt) {
				t.Fatal("encrypted code expiry differs from the actual validity")
			}
		})
	}
}
