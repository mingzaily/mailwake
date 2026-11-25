package native

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestActivityPreferenceAuthenticatesKeyAndDeliveryChoice(t *testing.T) {
	v := vector(t)
	p := storage.NativePairing{DeviceID: v.Device.ID, SigningKey: v.Device.PublicKey}
	pref := &activityPreference{Version: 1, Key: v.Encryption.PublicKey, Enabled: true, Only: true}
	text := strings.Join([]string{"mailwake-activity-key-v2", v.Audience, p.DeviceID, pref.Key, "1", "1", "1"}, "\n")
	pref.Signature, _ = coreidentity.Sign(fixedKey(t, v.Device), text)
	got, err := validatedActivityPreference(p, relayPairing{Activity: pref}, v.Audience)
	if err != nil || got.ActivityKey != pref.Key || !got.ActivityOnly {
		t.Fatalf("preference=%+v err=%v", got, err)
	}
	for _, change := range []func(){func() { pref.Key = v.Core.PublicKey }, func() { pref.Key = v.Encryption.PublicKey; pref.Only = false }} {
		change()
		if _, err := validatedActivityPreference(p, relayPairing{Activity: pref}, v.Audience); err == nil {
			t.Fatal("modified preference accepted")
		}
	}
	got, err = validatedActivityPreference(got, relayPairing{}, v.Audience)
	if err != nil || got.ActivityKey != "" || got.ActivityOnly {
		t.Fatal("old Relay must restore ordinary delivery")
	}
}

func TestActivityOnlyFallsBackUnlessRelayConfirmsAPNsAcceptance(t *testing.T) {
	for _, outcome := range []string{"accepted", "omitted", "omit_rejected", "rejected", "unknown", "queued", ""} {
		t.Run(outcome, func(t *testing.T) {
			v := vector(t)
			pushes := 0
			idForTest := ""
			service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/info":
					json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
				case "/v1/activities":
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["wait_for_acceptance"] != true || body["activity_key"] != v.Encryption.PublicKey {
						t.Error("activity acceptance request missing key binding")
					}
					w.WriteHeader(202)
					status := outcome
					if outcome == "omit_rejected" {
						status = "rejected"
					}
					if outcome == "omitted" {
						status = "accepted"
					}
					json.NewEncoder(w).Encode(map[string]string{"id": "activity-delivery", "status": "queued", "apns_status": status})
				case "/v1/push":
					pushes++
					var body struct {
						Envelope Envelope `json:"envelope"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					plain := decryptNotification(t, v, idForTest, "code-event", body.Envelope)
					var notification plaintext
					if err := json.Unmarshal(plain, &notification); err != nil || notification.Quiet != (outcome == "accepted") {
						t.Errorf("quiet notification: %s %v", plain, err)
					}
					w.WriteHeader(202)
					json.NewEncoder(w).Encode(relayDelivery{ID: "ordinary", Status: "queued"})
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			})
			id := addDevice(t, store, "phone", v.Encryption.PublicKey)
			idForTest = id
			if outcome == "omitted" || outcome == "omit_rejected" {
				invitation := storage.AppInvitation{ID: "invitation", TokenHash: "token", Management: true, Scopes: []string{"diagnostics"}, ExpiresAt: time.Now().Add(time.Hour).Unix()}
				if err := store.CreateAppInvitation(t.Context(), invitation); err != nil {
					t.Fatal(err)
				}
				controller, err := store.AcceptAppInvitation(t.Context(), "invitation", "token", storage.AppController{ID: "controller", DeviceID: "phone", DeviceName: "Phone"}, "credential")
				if err != nil {
					t.Fatal(err)
				}
				if err := store.SetActivityNotificationPreference(t.Context(), controller.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			p, err := store.NativePairing(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			p.ActivityKey = v.Encryption.PublicKey
			p.ActivityOnly = true
			if err = store.SaveActivityPreference(t.Context(), p); err != nil {
				t.Fatal(err)
			}
			n := event.Notification{ID: "code-event", Account: "Work", Folder: "INBOX", Code: "482913", ReceivedAt: time.Now()}
			if err = store.Commit(t.Context(), "account", "INBOX", "", "next", []event.Notification{n}); err != nil {
				t.Fatal(err)
			}
			if err = service.ForPairing(id).Send(t.Context(), n); err != nil {
				t.Fatal(err)
			}
			want := 1
			if outcome == "omitted" {
				want = 0
				pending, err := store.PendingNativeNotifications(t.Context(), "phone")
				if err != nil || len(pending) != 1 {
					t.Fatalf("omitted event missing: %v %v", pending, err)
				}
				var envelope Envelope
				if err := json.Unmarshal(pending[0].Envelope, &envelope); err != nil {
					t.Fatal(err)
				}
				plain := decryptNotification(t, v, id, n.ID, envelope)
				var payload plaintext
				if err := json.Unmarshal(plain, &payload); err != nil || payload.Code != n.Code || !payload.Quiet {
					t.Fatalf("pull decrypt: %s %v", plain, err)
				}
			}

			if pushes != want {
				t.Fatalf("ordinary deliveries=%d want=%d", pushes, want)
			}
			if ok, err := service.AlreadyAccepted(t.Context(), n.ID); err != nil || !ok {
				t.Fatalf("completion=%v err=%v", ok, err)
			}
		})
	}
}

func TestActivitiesEncryptToRegisteredSoftwareKeyAndKeepLegacyFallback(t *testing.T) {
	v := vector(t)
	service, _ := testService(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	identity, err := service.identity.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	n := event.Notification{ID: "key-event", Account: "Work", Folder: "INBOX", Code: "482913", ReceivedAt: time.Now()}
	for _, registered := range []bool{false, true} {
		p := storage.NativePairing{ID: v.PairingID, EncryptionKey: v.Encryption.PublicKey}
		recipient := v
		if registered {
			p.ActivityKey = v.Core.PublicKey
			recipient.Encryption = v.Core
		}
		items, err := service.planActivities(n, []storage.NativePairing{p}, identity, v.Audience)
		if err != nil {
			t.Fatal(err)
		}
		var request activityRequest
		if err = json.Unmarshal([]byte(items[0].Request), &request); err != nil {
			t.Fatal(err)
		}
		rawContext, _ := json.Marshal([]any{"mailwake-activity-v1", p.ID, n.ID, "start", n.ID, request.ReceivedAt, request.ExpiresAt, request.UpdatedAt})
		plain := decryptNotification(t, recipient, p.ID, hash(rawContext), request.Envelope)
		var content activityContent
		if err = json.Unmarshal(plain, &content); err != nil || content.Notification.Code != "482913" {
			t.Fatalf("activity decrypt: %s %v", plain, err)
		}
	}
}

func TestSignedActivityPreferenceRejectsVersionRollbackAndPreservesHighWater(t *testing.T) {
	v := vector(t)
	p := storage.NativePairing{DeviceID: v.Device.ID, SigningKey: v.Device.PublicKey, ActivityVersion: 2, ActivityKey: v.Core.PublicKey, ActivityOnly: false}
	pref := &activityPreference{Version: 1, Key: v.Encryption.PublicKey, Enabled: true, Only: true}
	text := strings.Join([]string{"mailwake-activity-key-v2", v.Audience, p.DeviceID, pref.Key, "1", "1", "1"}, "\n")
	pref.Signature, _ = coreidentity.Sign(fixedKey(t, v.Device), text)
	if _, err := validatedActivityPreference(p, relayPairing{Activity: pref}, v.Audience); err == nil {
		t.Fatal("retired proof restored old key and exclusive delivery")
	}
	missing, err := validatedActivityPreference(p, relayPairing{}, v.Audience)
	if err != nil || missing.ActivityVersion != 2 {
		t.Fatal("missing capability cleared the version high-water")
	}
	if _, err = validatedActivityPreference(missing, relayPairing{Activity: pref}, v.Audience); err == nil {
		t.Fatal("retired proof accepted after missing capability")
	}
	pref.Version = 3
	text = strings.Join([]string{"mailwake-activity-key-v2", v.Audience, p.DeviceID, pref.Key, "1", "1", "3"}, "\n")
	pref.Signature, _ = coreidentity.Sign(fixedKey(t, v.Device), text)
	current, err := validatedActivityPreference(p, relayPairing{Activity: pref}, v.Audience)
	if err != nil || current.ActivityVersion != 3 || !current.ActivityOnly {
		t.Fatalf("newer preference rejected: %+v %v", current, err)
	}
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	_ = service
	id := addDevice(t, store, "phone", v.Encryption.PublicKey)
	current.ID = id
	if err = store.SaveActivityPreference(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	stale := current
	stale.ActivityVersion = 1
	stale.ActivityKey = v.Core.PublicKey
	if err = store.SaveActivityPreference(t.Context(), stale); err != nil {
		t.Fatal(err)
	}
	stored, err := store.NativePairing(t.Context(), id)
	if err != nil || stored.ActivityVersion != 3 || stored.ActivityKey != pref.Key {
		t.Fatalf("stored high-water regressed: %+v %v", stored, err)
	}
}
