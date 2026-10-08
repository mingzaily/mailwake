package settings

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mingzaily/mailwake/internal/storage"
)

func TestVaultEncryptedPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("key permissions", err)
	}
	plain := []byte("synthetic mailbox credential")
	encrypted, err := v.Seal("mailbox", plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, plain) {
		t.Fatal("plaintext ciphertext")
	}
	if err := s.SaveConfiguration(t.Context(), "mailbox", encrypted); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Configuration(t.Context(), "mailbox")
	if err != nil || bytes.Contains(saved, plain) {
		t.Fatal("plaintext database", err)
	}
	reopened, err := OpenVault(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Open("mailbox", saved)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("roundtrip", err)
	}
	if _, err := v.Open("delivery", saved); err == nil {
		t.Fatal("row swapping accepted")
	}
	saved[len(saved)-1] ^= 1
	if _, err := v.Open("mailbox", saved); err == nil {
		t.Fatal("tampering accepted")
	}
	if err := os.Remove(filepath.Join(dir, "secret.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenVault(dir, true); err == nil {
		t.Fatal("missing key replaced")
	}
}
