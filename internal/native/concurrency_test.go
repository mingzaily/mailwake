package native

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mingzaily/mailwake/internal/event"
)

func TestSlowPollingDoesNotBlockSend(t *testing.T) {
	for _, kind := range []string{"status", "pairing"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			v := vector(t)
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			var claim Claim
			service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/info" {
					json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
					return
				}
				if r.URL.Path == "/v1/push" {
					var b map[string]json.RawMessage
					json.NewDecoder(r.Body).Decode(&b)
					var pid, eid string
					json.Unmarshal(b["pairing_id"], &pid)
					json.Unmarshal(b["event_id"], &eid)
					w.WriteHeader(202)
					json.NewEncoder(w).Encode(relayDelivery{ID: uuid.NewString(), PairingID: pid, EventID: eid, Status: "queued"})
					return
				}
				if strings.HasPrefix(r.URL.Path, "/v1/pairing-claims/") {
					json.NewEncoder(w).Encode(map[string]any{"claims": []Claim{claim}})
					return
				}
				entered <- struct{}{}
				select {
				case <-time.After(10 * time.Second):
				case <-release:
				case <-r.Context().Done():
				}
				w.WriteHeader(503)
			})
			defer close(release)
			ctx := t.Context()
			target := addDevice(t, store, "active", v.Encryption.PublicKey)
			if err := service.StartCheck(ctx); err != nil {
				t.Fatal(err)
			}
			if kind == "status" {
				n := event.Notification{ID: "prior", Test: true, ReceivedAt: time.Now()}
				if err := service.ForPairing(target).Send(ctx, n); err != nil {
					t.Fatal(err)
				}
				time.Sleep(10010 * time.Millisecond)
			} else {
				created, err := service.Create(ctx)
				if err != nil {
					t.Fatal(err)
				}
				p, _ := store.NativePairing(ctx, created.ID)
				identity, _ := service.identity.load(ctx)
				claim = Claim{v.Device.ID, v.Device.PublicKey, v.Encryption.PublicKey, "", "phone", p.ExpiresAt}
				claim.Signature, _ = sign(fixedKey(t, v.Device), consentText(v.Audience, p.ID, identity.ID, claim.DeviceID, v.Encryption.ID, p.TokenHash, strconv.FormatInt(p.ExpiresAt, 10)))
			}
			done := make(chan error, 1)
			go func() {
				if kind == "status" {
					done <- service.PollDeliveries(ctx)
				} else {
					done <- service.Poll(ctx)
				}
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("poll did not reach Relay")
			}
			sent := make(chan error, 1)
			go func() {
				sent <- service.ForPairing(target).Send(ctx, event.Notification{ID: "new", Test: true, ReceivedAt: time.Now()})
			}()
			select {
			case err := <-sent:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Error("Send blocked behind polling network request")
			}
			// The Relay really holds the poll for ten seconds; Send must finish before it.
			<-done
			select {
			case err := <-sent:
				if err != nil {
					t.Error(err)
				}
			default:
			}
		})
	}
}
