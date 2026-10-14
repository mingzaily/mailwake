package runtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/storage"
)

func revocationManager(t *testing.T) (*Manager, *storage.Store, func() *Manager) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	vault, err := OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			json.NewEncoder(w).Encode(native.Info{Audience: "test", Environment: "sandbox", Version: 2})
			return
		}
		if r.URL.Path == "/v1/push" {
			w.WriteHeader(410)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "pairing_revoked"}})
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/pairings/")
		p, err := store.NativePairing(r.Context(), id)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "revoked", "device_id": p.DeviceID, "encryption_key": p.EncryptionKey})
	}))
	t.Cleanup(relay.Close)
	create := func() *Manager {
		m, err := newManager(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), relay.URL, nil, func(Delivery) delivery.Sender { return &countingSender{} })
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := create()
	restart := func() *Manager {
		m.Close()
		store.Close()
		store, err = storage.Open(t.Context(), dir)
		if err != nil {
			t.Fatal(err)
		}
		m = create()
		return m
	}
	t.Cleanup(func() { m.Close() })
	return m, store, restart
}
func runtimePair(t *testing.T, s *storage.Store) string {
	t.Helper()
	p := storage.NativePairing{ID: uuid.NewString(), TokenHash: "fixture", EncryptedToken: []byte{1}, ExpiresAt: time.Now().Unix() + 300, CreatedAt: time.Now().Unix()}
	if err := s.CreateNativePairing(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	p.DeviceID = uuid.NewString()
	p.EncryptionKey = "fixture-key"
	p.SigningKey = "fixture-key"
	if err := s.SelectNativeDevice(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishNativePairing(t.Context(), p.ID, "active", ""); err != nil {
		t.Fatal(err)
	}
	return p.ID
}
func selectRuntimePair(t *testing.T, m *Manager, id string) {
	t.Helper()
	rev := m.DeliveryView().Revision
	if err := m.UpdateDelivery(t.Context(), DeliveryUpdate{Revision: &rev, Channel: "native", NativePairingID: &id, Preview: "off"}); err != nil {
		t.Fatal(err)
	}
}
func TestRevokedCurrentTargetClearsConfigurationAndSurvivesRestart(t *testing.T) {
	for _, source := range []string{"web", "sync", "delivery"} {
		t.Run(source, func(t *testing.T) {
			m, s, restart := revocationManager(t)
			id := runtimePair(t, s)
			selectRuntimePair(t, m, id)
			before := m.DeliveryView().Revision
			var err error
			if source == "web" {
				err = m.native.Unlink(t.Context(), id)
			} else if source == "delivery" {
				n := event.Notification{ID: "revoked-delivery", ReceivedAt: time.Now()}
				if err := s.Enqueue(t.Context(), n); err != nil {
					t.Fatal(err)
				}
				if err := s.SaveNativePlan(t.Context(), []storage.NativeDelivery{{EventID: n.ID, PairingID: id, Envelope: `{}`, Signature: "fixture", ExpiresAt: time.Now().Unix() + 300}}); err != nil {
					t.Fatal(err)
				}
				sendErr := m.native.ForPairing(id).Send(t.Context(), n)
				if sendErr == nil {
					t.Fatal("expected revoked delivery")
				}
			} else {
				err = m.native.PollActivePairings(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			check := func(m *Manager) {
				v := m.DeliveryView()
				if m.Channel() != "" || v.NativePairingID != "" || v.Revision != before+1 {
					t.Fatalf("stale configuration: %+v", v)
				}
				saved, err := m.settings.LoadDelivery(t.Context())
				if err != nil || saved.Channel != "" || saved.NativePairingID != "" {
					t.Fatal(saved, err)
				}
			}
			check(m)
			if err := m.revokeNativePairing(t.Context(), id, false); err != nil {
				t.Fatal(err)
			}
			check(m)
			check(restart())
		})
	}
}
func TestRevokingAnotherTargetPreservesNotificationConfiguration(t *testing.T) {
	for _, channel := range []string{"native", "bark"} {
		t.Run(channel, func(t *testing.T) {
			m, s, _ := revocationManager(t)
			old := runtimePair(t, s)
			other := runtimePair(t, s)
			selectRuntimePair(t, m, other)
			if channel == "bark" {
				rev := m.DeliveryView().Revision
				key := "synthetic-key"
				input := DeliveryUpdate{Revision: &rev, Channel: "bark", Preview: "off"}
				input.Bark.Endpoint = "https://api.day.app"
				input.Bark.Key = &key
				if err := m.UpdateDelivery(t.Context(), input); err != nil {
					t.Fatal(err)
				}
			}
			before := m.delivery
			if err := m.revokeNativePairing(t.Context(), old, false); err != nil {
				t.Fatal(err)
			}
			if m.delivery != before {
				t.Fatal("other channel settings changed")
			}
			p, _ := s.NativePairing(t.Context(), other)
			if p.State != "active" {
				t.Fatal(p)
			}
		})
	}
}
func TestRevocationAndTargetSwitchUseRevisionConflict(t *testing.T) {
	for range 20 {
		m, s, _ := revocationManager(t)
		old := runtimePair(t, s)
		next := runtimePair(t, s)
		selectRuntimePair(t, m, old)
		rev := m.DeliveryView().Revision
		var revokeErr, switchErr error
		var wg sync.WaitGroup
		wg.Add(2)
		start := make(chan struct{})
		go func() { defer wg.Done(); <-start; revokeErr = m.revokeNativePairing(context.Background(), old, false) }()
		go func() {
			defer wg.Done()
			<-start
			switchErr = m.UpdateDelivery(context.Background(), DeliveryUpdate{Revision: &rev, Channel: "native", NativePairingID: &next, Preview: "off"})
		}()
		close(start)
		wg.Wait()
		if revokeErr != nil {
			t.Fatal(revokeErr)
		}
		if switchErr == nil {
			if m.delivery.Channel != "native" || m.delivery.NativePairingID != next || m.delivery.Revision != rev+1 {
				t.Fatal("new target cleared", m.DeliveryView())
			}
		} else {
			if fault.From(switchErr, "").Code != "settings_conflict" || m.Channel() != "" {
				t.Fatal(switchErr, m.DeliveryView())
			}
			selectRuntimePair(t, m, next)
		}
		// A late duplicate signal for the old target preserves the user's new choice.
		if err := m.revokeNativePairing(t.Context(), old, false); err != nil {
			t.Fatal(err)
		}
		if m.delivery.NativePairingID != next {
			t.Fatal("duplicate cleared new target")
		}
		m.Close()
	}
}

func TestSavingTargetDuringItsRevocationKeepsOnlyActiveTargets(t *testing.T) {
	for range 10 {
		m, s, _ := revocationManager(t)
		current := runtimePair(t, s)
		candidate := runtimePair(t, s)
		selectRuntimePair(t, m, current)
		rev := m.delivery.Revision
		var revoked, saved error
		var wg sync.WaitGroup
		wg.Add(2)
		start := make(chan struct{})
		go func() { defer wg.Done(); <-start; revoked = m.revokeNativePairing(t.Context(), candidate, false) }()
		go func() {
			defer wg.Done()
			<-start
			saved = m.UpdateDelivery(t.Context(), DeliveryUpdate{Revision: &rev, Channel: "native", NativePairingID: &candidate, Preview: "off"})
		}()
		close(start)
		wg.Wait()
		if revoked != nil {
			t.Fatal(revoked)
		}
		if saved == nil {
			if m.Channel() != "" || m.delivery.NativePairingID != "" {
				t.Fatal("saved revoked target")
			}
		} else if fault.From(saved, "").Code != "native_target_unavailable" || m.delivery.NativePairingID != current {
			t.Fatal(saved, m.DeliveryView())
		}
		m.Close()
	}
}
