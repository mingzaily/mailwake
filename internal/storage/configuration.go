package storage

import (
	"context"
	"database/sql"
	"errors"
	"github.com/mingzaily/mailwake/internal/fault"
)

func (s *Store) HasConfiguration(ctx context.Context) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM configuration)").Scan(&exists)
	return exists, err
}
func (s *Store) Configuration(ctx context.Context, name string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, "SELECT encrypted FROM configuration WHERE name=?", name).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}
func (s *Store) SaveConfiguration(ctx context.Context, name string, encrypted []byte) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO configuration(name,encrypted) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET encrypted=excluded.encrypted", name, encrypted)
	return err
}

// SaveDelivery commits configuration, enqueue privacy policy and queued metadata
// removal atomically. The enqueue policy also covers watcher batches already in flight.
func (s *Store) SaveDelivery(ctx context.Context, encrypted []byte, preview, channel string, clearPreview bool, revision int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	err = tx.QueryRowContext(ctx, "SELECT revision FROM configuration WHERE name='delivery'").Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if current != revision {
		return fault.New("settings_conflict")
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO configuration(name,encrypted) VALUES('delivery',?) ON CONFLICT(name) DO UPDATE SET encrypted=excluded.encrypted,revision=configuration.revision+1", encrypted); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO settings(name,value) VALUES('delivery_preview',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", preview); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO settings(name,value) VALUES('delivery_channel',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", channel); err != nil {
		return err
	}
	if clearPreview {
		if _, err := tx.ExecContext(ctx, "UPDATE outbox SET payload=json_remove(payload,'$.sender','$.subject') WHERE state<>'accepted'"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetDeliveryChannel(ctx context.Context, channel string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO settings(name,value) VALUES('delivery_channel',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value", channel)
	return err
}
