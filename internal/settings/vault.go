// Package settings owns configuration models, validation and encrypted persistence.
package settings

import (
	"crypto/aes"
	"crypto/cipher"

	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"github.com/mingzaily/mailwake/internal/fault"
)

type Vault struct {
	aead cipher.AEAD
}

// OpenVault requires the caller to hold the data directory lock. An existing
// encrypted configuration requires its original key; a missing key fails closed.
func OpenVault(dir string, hasConfiguration bool) (*Vault, error) {
	path := filepath.Join(dir, "secret.key")
	info, err := os.Lstat(path)
	if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
		return nil, fault.New("secret_key_invalid")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fault.New("secret_key_invalid")
	}
	var key []byte
	if errors.Is(err, os.ErrNotExist) {
		if hasConfiguration {
			return nil, fault.New("secret_key_invalid")
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, fault.New("secret_key_invalid")
		}
		_, writeErr := f.Write(key)
		syncErr := f.Sync()
		closeErr := f.Close()
		if errors.Join(writeErr, syncErr, closeErr) != nil {
			return nil, fault.New("secret_key_invalid")
		}
		directory, err := os.Open(dir)
		if err != nil {
			return nil, fault.New("secret_key_invalid")
		}
		syncErr = directory.Sync()
		closeErr = directory.Close()
		if errors.Join(syncErr, closeErr) != nil {
			return nil, fault.New("secret_key_invalid")
		}

	} else {
		key, err = os.ReadFile(path)
		if err != nil {
			return nil, fault.New("secret_key_invalid")
		}
	}
	if len(key) != 32 {
		return nil, fault.New("secret_key_invalid")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

// The setting name is authenticated to prevent swapping ciphertext between rows.
func (v *Vault) Seal(name string, plain []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plain, []byte(name)), nil
}
func (v *Vault) Open(name string, encrypted []byte) ([]byte, error) {
	n := v.aead.NonceSize()
	if len(encrypted) < n {
		return nil, fault.New("configuration_corrupt")
	}
	plain, err := v.aead.Open(nil, encrypted[:n], encrypted[n:], []byte(name))
	if err != nil {
		return nil, fault.New("configuration_corrupt")
	}
	return plain, nil
}
