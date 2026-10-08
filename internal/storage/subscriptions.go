package storage

import (
	"context"
	"encoding/json"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// LoadSubscriptions reads one mailbox's saved folders. A missing row is an empty list.
func (s *Store) LoadSubscriptions(ctx context.Context, account string) (mail.Subscriptions, error) {
	if _, err := s.db.ExecContext(ctx, "INSERT INTO mailbox_subscriptions(account,revision,folders) VALUES(?,1,'[]') ON CONFLICT(account) DO NOTHING", account); err != nil {
		return mail.Subscriptions{}, err
	}
	var data string
	var state mail.Subscriptions
	if err := s.db.QueryRowContext(ctx, "SELECT revision,folders FROM mailbox_subscriptions WHERE account=?", account).Scan(&state.Revision, &data); err != nil {
		return mail.Subscriptions{}, err
	}
	if err := json.Unmarshal([]byte(data), &state.Folders); err != nil {
		return state, fault.New("subscriptions_invalid")
	}
	var err error
	state.Folders, err = mail.NormalizeSubscriptions(state.Folders)
	if err != nil || state.Revision < 1 {
		return state, fault.New("subscriptions_invalid")
	}
	return state, nil
}

// SaveSubscriptions stores the next revision and, in the same transaction, clears the
// progress of folders that were not subscribed before. A resubscribed folder starts from
// a new baseline instead of replaying mail that arrived while it was unsubscribed.
func (s *Store) SaveSubscriptions(ctx context.Context, account string, next mail.Subscriptions) error {
	data, err := json.Marshal(next.Folders)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	var current mail.Subscriptions
	if err := tx.QueryRowContext(ctx, "SELECT revision,folders FROM mailbox_subscriptions WHERE account=?", account).Scan(&current.Revision, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), &current.Folders); err != nil {
		return fault.New("subscriptions_invalid")
	}
	if current.Revision != next.Revision-1 {
		return fault.New("subscriptions_conflict")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE mailbox_subscriptions SET revision=?,folders=? WHERE account=?", next.Revision, string(data), account); err != nil {
		return err
	}
	currentNames := make(map[string]bool, len(current.Folders))
	for _, folder := range current.Folders {
		currentNames[folder.Name] = true
	}
	for _, folder := range next.Folders {
		if currentNames[folder.Name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM cursors WHERE account=? AND folder=?", account, folder.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}
