package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestAdminNativeResetOfflineOutputAndRePair(t *testing.T) {
	var offline atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/v1/info" {
			json.NewEncoder(w).Encode(native.Info{Audience: "old", Environment: "sandbox", Version: 2})
			return
		}
		if r.Method != "DELETE" || !strings.HasPrefix(r.URL.Path, "/v1/pairings/") {
			t.Error("unexpected request", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	ctx := t.Context()
	dir := t.TempDir()
	s, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := native.New(s, vault, server.URL, log)
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SelectNativeDevice(ctx, storage.NativePairing{ID: before.ID, DeviceID: "phone", DeviceName: "Phone"}); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishNativePairing(ctx, before.ID, "active", ""); err != nil {
		t.Fatal(err)
	}
	s.Close()
	offline.Store(true)
	var output bytes.Buffer
	if err = runAdmin(ctx, dir, "reset-native-push", adminInteraction{confirm: func() (bool, error) { return true, nil }, output: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "The old Relay could not be reached") || !strings.Contains(output.String(), "unresolved: 1") || strings.Contains(output.String(), "reset completed") {
		t.Fatal(output.String())
	}
	nextServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(native.Info{Audience: "new", Environment: "production", Version: 2})
	}))
	defer nextServer.Close()
	s, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	vault, err = settings.OpenVault(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	service, err = native.New(s, vault, nextServer.URL, log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Create(ctx); fault.From(err, "").Code != "native_environment_mismatch" {
		t.Fatal("pending reset permitted another Relay", err)
	}
	if pending, err := s.NativeResetTargets(ctx); err != nil || len(pending) != 1 {
		t.Fatal("offline reset discarded the pairing", pending, err)
	}
	s.Close()
	offline.Store(false)
	output.Reset()
	if err = runAdmin(ctx, dir, "reset-native-push", adminInteraction{confirm: func() (bool, error) { return true, nil }, output: &output}); err != nil || !strings.Contains(output.String(), "reset completed") || !strings.Contains(output.String(), "unresolved: 0") {
		t.Fatal("successful revocation must complete the reset", err, output.String())
	}
	s, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	service, err = native.New(s, vault, nextServer.URL, log)
	if err != nil {
		t.Fatal(err)
	}
	after, err := service.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.Fingerprint != after.Fingerprint {
		t.Fatal("reset changed Core identity")
	}
}
