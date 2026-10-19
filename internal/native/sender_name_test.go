package native

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/storage"
	"golang.org/x/crypto/hkdf"
)

func TestSenderNameSurvivesPersistenceAndEncryptedDelivery(t *testing.T) {
	for _, name := range []string{"Alice", "", "通知中心"} {
		t.Run(name, func(t *testing.T) {
			v := vector(t)
			type mailRequest struct {
				PairingID string   `json:"pairing_id"`
				EventID   string   `json:"event_id"`
				Envelope  Envelope `json:"envelope"`
				Signature string   `json:"content_signature"`
			}
			requests := make(chan mailRequest, 1)
			service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/info" {
					json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
					return
				}
				var request mailRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				requests <- request
				w.WriteHeader(202)
				json.NewEncoder(w).Encode(relayDelivery{ID: "delivered", PairingID: request.PairingID, EventID: request.EventID, Status: "queued"})
			})
			pairing := addDevice(t, store, "sender-device", v.Encryption.PublicKey)
			if err := store.SaveDelivery(t.Context(), []byte("synthetic"), "subject", "native", false, 0); err != nil {
				t.Fatal(err)
			}
			notification := event.Notification{ID: "sender-event", MailboxID: "mailbox", Account: "Work", Folder: "Clients", Sender: "sender@example.org", SenderName: name, Subject: "Hello", ReceivedAt: time.Now()}
			if err := store.Commit(t.Context(), "account", "Clients", "", "next", []event.Notification{notification}); err != nil {
				t.Fatal(err)
			}
			task, err := store.Due(t.Context(), time.Now().Add(time.Second))
			if err != nil || task == nil || task.Event.SenderName != name || task.Event.Sender != notification.Sender {
				t.Fatalf("persisted sender: %+v %v", task, err)
			}
			if err := service.ForPairing(pairing).Send(t.Context(), task.Event); err != nil {
				t.Fatal(err)
			}
			deliveredRequest := <-requests
			identity, err := service.identity.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err := coreidentity.Verify(identity.PublicKey, deliveredRequest.Signature, contentText(v.Audience, pairing, notification.ID, deliveredRequest.Envelope)); err != nil {
				t.Fatal(err)
			}
			raw := decryptNotification(t, v, pairing, notification.ID, deliveredRequest.Envelope)
			var delivered plaintext
			if err := json.Unmarshal(raw, &delivered); err != nil {
				t.Fatal(err)
			}
			if delivered.Sender != notification.Sender || delivered.SenderName != name {
				t.Fatalf("decrypted sender: %+v", delivered)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if _, present := fields["sender_name"]; present != (name != "") {
				t.Fatal("optional name presence differs")
			}
			notification.Code = "482913"
			devices, err := store.NativeDevices(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			activities, err := service.planActivities(notification, devices, identity, v.Audience)
			if err != nil || len(activities) != 1 {
				t.Fatalf("activity: %+v %v", activities, err)
			}
			var request activityRequest
			if err := json.Unmarshal([]byte(activities[0].Request), &request); err != nil {
				t.Fatal(err)
			}
			rawContext, _ := json.Marshal([]any{"mailwake-activity-v1", pairing, notification.ID, "start", notification.ID, request.ReceivedAt, request.ExpiresAt, request.UpdatedAt})
			var activity activityContent
			if err := json.Unmarshal(decryptNotification(t, v, pairing, hash(rawContext), request.Envelope), &activity); err != nil {
				t.Fatal(err)
			}
			if activity.Notification.SenderName != name || activity.Notification.Sender != notification.Sender {
				t.Fatal("activity sender differs")
			}
		})
	}
}

func decryptNotification(t *testing.T, v testVector, pairing, eventID string, envelope Envelope) []byte {
	t.Helper()
	private, err := fixedKey(t, v.Encryption).ECDH()
	if err != nil {
		t.Fatal(err)
	}
	public, err := coreidentity.PublicKey(envelope.EphemeralPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := public.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	shared, err := private.ECDH(peer)
	if err != nil {
		t.Fatal(err)
	}
	salt, err := encoding.DecodeString(envelope.Salt)
	if err != nil {
		t.Fatal(err)
	}
	context := []byte(strings.Join([]string{"mailwake-e2ee-v1", v.Audience, pairing, eventID}, "\n"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, salt, context), key); err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := encoding.DecodeString(envelope.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := encoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := aead.Open(nil, nonce, ciphertext, context)
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

// Keep the persistence boundary explicit: an event is loaded from SQLite after reopening.
func TestSenderNameSurvivesOutboxRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := t.Context()
	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDelivery(ctx, []byte("synthetic"), "subject", "webhook", false, 0); err != nil {
		t.Fatal(err)
	}
	n := event.Notification{ID: "persisted", Sender: "sender@example.org", SenderName: "通知中心", ReceivedAt: time.Now()}
	if err := store.Enqueue(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	task, err := store.Due(ctx, time.Now().Add(time.Second))
	if err != nil || task == nil || task.Event.SenderName != n.SenderName || task.Event.Sender != n.Sender {
		t.Fatalf("reopened: %+v %v", task, err)
	}
}
