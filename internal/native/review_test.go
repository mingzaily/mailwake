package native

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestPairingClockSkew(t *testing.T) {
	for _, seconds := range []int{119, -119, 121, -121} {
		t.Run(strconv.Itoa(seconds), func(t *testing.T) {
			service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Date", time.Now().Add(time.Duration(seconds)*time.Second).UTC().Format(http.TimeFormat))
				json.NewEncoder(w).Encode(Info{Audience: "test", Environment: "sandbox", Version: 2})
			})
			_, err := service.Create(t.Context())
			if seconds > 120 || seconds < -120 {
				var f *fault.Error
				if !errors.As(err, &f) || f.Code != "clock_skew" {
					t.Fatalf("expected clock_skew, got %v", err)
				}
				observed, parseErr := strconv.Atoi(f.Params["seconds"])
				// HTTP Date truncates seconds while the request midpoint can cross a second.
				if parseErr != nil || observed <= 120 || observed > 122 {
					t.Fatal("clock diagnostic exceeds HTTP Date precision", f.Params)
				}
				rows, _ := store.NativePairings(t.Context(), "waiting")
				if len(rows) != 0 {
					t.Fatal("created despite clock skew")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRelayRevocationSignals(t *testing.T) {
	for _, signal := range []string{"push", "status"} {
		t.Run(signal, func(t *testing.T) {
			v := vector(t)
			var revoked string
			var accepted relayDelivery
			service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/info" {
					json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
					return
				}
				if r.Method == "GET" {
					accepted.Status = "cancelled"
					accepted.LastError = "pairing_revoked"
					json.NewEncoder(w).Encode(accepted)
					return
				}
				var body struct {
					PairingID string `json:"pairing_id"`
					EventID   string `json:"event_id"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				if body.PairingID == revoked && signal == "push" {
					w.WriteHeader(410)
					json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "pairing_revoked"}})
					return
				}
				result := relayDelivery{ID: uuid.NewString(), PairingID: body.PairingID, EventID: body.EventID, Status: "queued"}
				if body.PairingID == revoked {
					accepted = result
				}
				w.WriteHeader(202)
				json.NewEncoder(w).Encode(result)
			})
			ctx := t.Context()
			revoked = addDevice(t, store, "revoked", v.Encryption.PublicKey)
			n := event.Notification{ID: "signal", Test: true, ReceivedAt: time.Now()}
			if signal == "status" {
				if err := service.ForPairing(revoked).Send(ctx, n); err != nil {
					t.Fatal(err)
				}
			}
			other := addDevice(t, store, "other", v.Core.PublicKey)
			queued := event.Notification{ID: "queued", ReceivedAt: time.Now()}
			if err := store.Enqueue(ctx, queued); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveNativePlan(ctx, []storage.NativeDelivery{{EventID: queued.ID, PairingID: revoked, Envelope: "cipher", Signature: "sig"}, {EventID: queued.ID, PairingID: other, Envelope: "cipher", Signature: "sig"}}); err != nil {
				t.Fatal(err)
			}
			if signal == "push" {
				_ = service.ForPairing(revoked).Send(ctx, n)
			} else {
				time.Sleep(10010 * time.Millisecond)
				if err := service.PollDeliveries(ctx); err != nil {
					t.Fatal(err)
				}
			}
			p, err := store.NativePairing(ctx, revoked)
			if err != nil || p.State != "revoked" {
				t.Fatalf("pairing still active: %+v %v", p, err)
			}
			rows, _ := store.NativeDeliveries(ctx, queued.ID)
			for _, d := range rows {
				if d.PairingID == revoked && (d.State != "cancelled" || d.ErrorCode != "pairing_revoked" || d.Envelope != "") {
					t.Fatalf("pending remains: %+v", d)
				}
				if d.PairingID == other && d.State != "pending" {
					t.Fatal("other device changed")
				}
			}
			next := event.Notification{ID: "next", Test: true, ReceivedAt: time.Now()}
			if err := service.ForPairing(other).Send(ctx, next); err != nil {
				t.Fatal(err)
			}
			rows, _ = store.NativeDeliveries(ctx, next.ID)
			if len(rows) != 1 || rows[0].PairingID != other {
				t.Fatal("new delivery targeted revoked device")
			}
			devices, err := service.Devices(ctx)
			if err != nil || len(devices) != 2 {
				t.Fatal("revoked device missing from management")
			}
		})
	}
}

func TestRelayDateCalibratesSignedRequestTimestamp(t *testing.T) {
	for _, offset := range []int{119, -119} {
		t.Run(strconv.Itoa(offset), func(t *testing.T) {
			service, _ := testService(t, func(w http.ResponseWriter, r *http.Request) {
				relayNow := time.Now().Add(time.Duration(offset) * time.Second)
				if r.URL.Path == "/v1/info" {
					w.Header().Set("Date", relayNow.UTC().Format(http.TimeFormat))
					json.NewEncoder(w).Encode(Info{Audience: "test", Environment: "sandbox", Version: 2})
					return
				}
				timestamp, err := strconv.ParseInt(r.Header.Get("X-Relay-Timestamp"), 10, 64)
				if err != nil || time.Unix(timestamp, 0).Sub(relayNow).Abs() > 2*time.Second {
					t.Error("request timestamp still uses skewed Core clock")
				}
				w.WriteHeader(200)
			})
			if _, err := service.Create(t.Context()); err != nil {
				t.Fatal(err)
			}
			identity, err := service.identity.load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if err = service.client.call(t.Context(), identity, "test", "DELETE", "/v1/pairings/fixture", nil, nil, 200); err != nil {
				t.Fatal(err)
			}
		})
	}
}
