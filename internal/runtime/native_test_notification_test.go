package runtime

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestNativeSaveIsLocalAndBothTestRoutesUseFreePairingTests(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/info":
			json.NewEncoder(w).Encode(native.Info{Audience: "test-relay", Environment: "sandbox", Version: 2})
		case "/v1/pairing-tests":
			requests.Add(1)
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]string{"test_id": body["test_id"], "status": "rejected", "error_code": "apns_authentication_failed"})
		default:
			t.Errorf("unexpected notification request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	m, err := newManager(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), server.URL, nil, func(Delivery) delivery.Sender { return &countingSender{} })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	id := uuid.NewString()
	pair := storage.NativePairing{ID: id, State: "waiting", TokenHash: "fixture", EncryptedToken: []byte{1}, CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Unix() + 300}
	if err := store.CreateNativePairing(t.Context(), pair); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishNativePairing(t.Context(), id, "active", ""); err != nil {
		t.Fatal(err)
	}
	revision := m.DeliveryView()["revision"].(int64)
	input := DeliveryUpdate{Revision: &revision, Channel: "native", NativePairingID: &id, Preview: "off"}
	if err := m.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("save contacted Relay")
	}
	for _, err := range []error{m.TestDelivery(t.Context(), input), m.TestSavedDelivery(t.Context())} {
		if fault.From(err, "").Code != "apns_authentication_failed" {
			t.Fatal("test did not report APNs rejection", err)
		}
	}
	revision++
	if err := m.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal("APNs failure blocked save", err)
	}
	if requests.Load() != 2 {
		t.Fatal("save sent a test notification")
	}
	revision++
	if _, err := store.RevokeNativePairing(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateDelivery(t.Context(), input); fault.From(err, "").Code != "native_target_unavailable" {
		t.Fatal("inactive target saved", err)
	}
}
