package settings

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

// Repository owns encrypted configuration persistence and validates stored models.
type Repository struct {
	store *storage.Store
	vault *Vault
}

func NewRepository(store *storage.Store, vault *Vault) *Repository {
	return &Repository{store: store, vault: vault}
}
func (r *Repository) LoadDelivery(ctx context.Context) (*Delivery, error) {
	var value *Delivery
	if err := r.load(ctx, "delivery", &value); err != nil {
		return nil, err
	}
	if value != nil && value.validate() != nil {
		return nil, fault.New("configuration_invalid")
	}
	return value, nil
}
func (r *Repository) load(ctx context.Context, name string, target any) error {
	encrypted, err := r.store.Configuration(ctx, name)
	if err != nil || encrypted == nil {
		return err
	}
	data, err := r.vault.Open(name, encrypted)
	if err != nil {
		return err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' || json.Unmarshal(data, target) != nil {
		return fault.New("configuration_invalid")
	}
	return nil
}
func (r *Repository) encrypt(name string, value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return r.vault.Seal(name, data)
}
