package native

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mingzaily/mailwake/internal/storage"
)

func TestChangedRelayURLFailsBeforeNetwork(t *testing.T) {
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Info{Audience: "old", Environment: "sandbox", Version: 2})
	})
	if _, err := service.Create(t.Context()); err != nil {
		t.Fatal(err)
	}
	next, err := New(store, service.vault, "http://127.0.0.1:1", service.log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = next.Create(t.Context()); safeCode(err) != "native_environment_mismatch" {
		t.Fatalf("changed address must identify reset requirement before connecting: %v", err)
	}
}

func TestResetPushUnlinksPairingsAtTheOldRelayFirst(t *testing.T) {
	v := vector(t)
	var down atomic.Bool
	var deletes atomic.Int32
	service, store := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/v1/info" {
			json.NewEncoder(w).Encode(Info{Audience: v.Audience, Environment: "sandbox", Version: 2})
			return
		}
		if r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/v1/pairings/") {
			// The first pairing is already gone at Relay; the second is removed now.
			if deletes.Add(1) == 1 {
				w.WriteHeader(404)
				return
			}
			w.Write([]byte(`{}`))
			return
		}
		t.Error("unexpected request", r.Method, r.URL.Path)
	})
	ctx := t.Context()
	for _, name := range []string{"one", "two"} {
		created, err := service.Create(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SelectNativeDevice(ctx, storage.NativePairing{ID: created.ID, DeviceID: name, DeviceName: name}); err != nil {
			t.Fatal(err)
		}
		if err := store.FinishNativePairing(ctx, created.ID, "active", ""); err != nil {
			t.Fatal(err)
		}
	}
	identityBefore, _ := store.Configuration(ctx, "native.identity")

	down.Store(true)
	report, err := ResetPush(ctx, store, service.vault)
	if err != nil || !report.RelayUnavailable || report.Remaining != 2 {
		t.Fatal("offline reset must keep pairings", report, err)
	}

	down.Store(false)
	report, err = ResetPush(ctx, store, service.vault)
	if err != nil || report.Remaining != 0 || report.Unlinked != 2 {
		t.Fatal(report, err)
	}
	remaining, _ := store.NativeResetTargets(ctx)
	binding, _ := store.Configuration(ctx, "native.relay")
	identityAfter, _ := store.Configuration(ctx, "native.identity")
	if len(remaining) != 0 || binding != nil || !bytes.Equal(identityBefore, identityAfter) {
		t.Fatal("reset must clear the Relay binding and keep the Core identity")
	}
}
