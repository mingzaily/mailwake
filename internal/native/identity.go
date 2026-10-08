package native

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"strings"
	"sync"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

type Identity struct {
	Key           *ecdsa.PrivateKey
	PublicKey, ID string
}
type identityStore struct {
	mu     sync.Mutex
	cached *Identity
	store  *storage.Store
	vault  *settings.Vault
}

func (s *identityStore) load(ctx context.Context) (*Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil {
		return s.cached, nil
	}
	const name = "native.identity"
	encrypted, err := s.store.Configuration(ctx, name)
	if err != nil {
		return nil, err
	}
	var key *ecdsa.PrivateKey
	if encrypted == nil {
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		encrypted, err = s.vault.Seal(name, der)
		if err != nil {
			return nil, err
		}
		if err = s.store.InitializeConfiguration(ctx, name, encrypted); err != nil {
			return nil, err
		}
	}
	encrypted, err = s.store.Configuration(ctx, name)
	if err != nil {
		return nil, err
	}
	der, err := s.vault.Open(name, encrypted)
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fault.New("configuration_corrupt")
	}
	var ok bool
	key, ok = parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fault.New("configuration_corrupt")
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	s.cached = &Identity{Key: key, PublicKey: encoding.EncodeToString(publicDER), ID: hash(publicDER)}
	return s.cached, nil
}
func (i *Identity) Fingerprint() string {
	s := strings.ToUpper(i.ID[:16])
	return s[:4] + " " + s[4:8] + " " + s[8:12] + " " + s[12:]
}
