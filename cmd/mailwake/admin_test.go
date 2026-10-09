package main

import (
	"log/slog"
	"testing"

	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestAdminRequiresStoppedCore(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, command := range []string{"reset-password", "reset-setup", "reset-native-push"} {
		err := runAdmin(t.Context(), dir, command, adminInteraction{password: func() (string, error) { t.Fatal("prompt reached while locked"); return "", nil }, confirm: func() (bool, error) { t.Fatal("confirmation reached while locked"); return false, nil }})
		if fault.From(err, "").Code != "data_directory_locked" {
			t.Fatal(command, err)
		}
	}
}
func TestAdminResets(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	a := auth.New(s, slog.Default())
	code, err := a.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := a.Setup(t.Context(), "test", code, "admin", "initial administrator password")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runAdmin(t.Context(), dir, "reset-password", adminInteraction{password: func() (string, error) { return "new administrator password", nil }}); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	a = auth.New(s, slog.Default())
	if _, err := a.AuthenticateSession(t.Context(), grant.ID); err == nil {
		t.Fatal("session survived reset")
	}
	if _, err := a.Login(t.Context(), "test", "admin", "new administrator password"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := runAdmin(t.Context(), dir, "reset-setup", adminInteraction{}); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a = auth.New(s, slog.Default())
	if ready, err := a.Ready(t.Context()); err != nil || ready {
		t.Fatal("setup retained", err)
	}
	if code, err := a.Prepare(t.Context()); err != nil || code == "" {
		t.Fatal("new setup unavailable", err)
	}
}

func TestAdminNativeResetCommandExists(t *testing.T) {
	if err := runAdmin(t.Context(), t.TempDir(), "reset-native-push", adminInteraction{confirm: func() (bool, error) { return true, nil }}); err != nil {
		t.Fatal(err)
	}
}

func TestAdminNativeResetRequiresConfirmation(t *testing.T) {
	for _, confirm := range []func() (bool, error){nil, func() (bool, error) { return false, nil }} {
		dir := t.TempDir()
		s, err := storage.Open(t.Context(), dir)
		if err != nil {
			t.Fatal(err)
		}
		s.SaveConfiguration(t.Context(), "native.relay", []byte("existing"))
		s.Close()
		err = runAdmin(t.Context(), dir, "reset-native-push", adminInteraction{confirm: confirm})
		if err == nil {
			t.Fatal("reset proceeded without confirmation")
		}
		s, err = storage.Open(t.Context(), dir)
		if err != nil {
			t.Fatal(err)
		}
		saved, _ := s.Configuration(t.Context(), "native.relay")
		s.Close()
		if string(saved) != "existing" {
			t.Fatal("cancelled reset changed local binding")
		}
	}
}
