package coreidentity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func TestIdentityPersistenceAndAAD(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	keys := New(s, vault)
	first, err := keys.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := s.Configuration(ctx, "native.identity")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = vault.Open("delivery", encrypted); err == nil {
		t.Fatal("AAD accepted another config name")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	vault, err = settings.OpenVault(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := (New(s, vault)).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if restored.PublicKey != first.PublicKey || restored.ID != first.ID {
		t.Fatal("identity changed after restart")
	}
	if len(restored.Fingerprint()) != 19 || strings.ReplaceAll(restored.Fingerprint(), " ", "") != strings.ToUpper(first.ID[:16]) {
		t.Fatal("fingerprint mismatch")
	}
	if err = os.Remove(filepath.Join(dir, "secret.key")); err != nil {
		t.Fatal(err)
	}
	if _, err = settings.OpenVault(dir, true); err == nil {
		t.Fatal("missing encryption key accepted")
	}
}
