package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mingzaily/mailwake/internal/fault"
)

const MaxMailboxes = 20

func MailboxConfigurationName(id string) string { return "mailbox:" + id }

type ConfigurationRecord struct {
	Name      string
	Revision  int64
	Encrypted []byte
}

func (s *Store) ConfigurationRecord(ctx context.Context, name string) (ConfigurationRecord, error) {
	record := ConfigurationRecord{Name: name}
	err := s.db.QueryRowContext(ctx, "SELECT revision,encrypted FROM configuration WHERE name=?", name).Scan(&record.Revision, &record.Encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return record, nil
	}
	return record, err
}
func (s *Store) MailboxConfigurations(ctx context.Context) ([]ConfigurationRecord, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name,revision,encrypted FROM configuration WHERE name LIKE 'mailbox:%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []ConfigurationRecord{}
	for rows.Next() {
		var record ConfigurationRecord
		if err := rows.Scan(&record.Name, &record.Revision, &record.Encrypted); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// SaveMailboxRecord checks the persisted revision before changing this mailbox's progress.
func (s *Store) SaveMailboxRecord(ctx context.Context, id string, revision int64, encrypted []byte, reset bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	name := MailboxConfigurationName(id)
	if revision == 0 {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM configuration WHERE name LIKE 'mailbox:%'").Scan(&count); err != nil {
			return err
		}
		if count >= MaxMailboxes {
			return fault.New("mailbox_limit_exceeded")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO configuration(name,revision,encrypted) VALUES(?,1,?)", name, encrypted); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO mailbox_subscriptions(account,revision,folders) VALUES(?,1,'[]')", id); err != nil {
			return err
		}
	} else {
		result, err := tx.ExecContext(ctx, "UPDATE configuration SET encrypted=?,revision=revision+1 WHERE name=? AND revision=?", encrypted, name, revision)
		if err != nil {
			return err
		}
		if err := changedConfiguration(ctx, tx, result, name); err != nil {
			return err
		}
	}
	if reset {
		if _, err := tx.ExecContext(ctx, "DELETE FROM cursors WHERE account=?", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func changedConfiguration(ctx context.Context, tx *sql.Tx, result sql.Result, name string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM configuration WHERE name=?)", name).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fault.New("mailbox_not_found")
	}
	return fault.New("settings_conflict")
}
func (s *Store) DeleteMailbox(ctx context.Context, id string, revision int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM configuration WHERE name=? AND revision=?", MailboxConfigurationName(id), revision)
	if err != nil {
		return err
	}
	if err := changedConfiguration(ctx, tx, result, MailboxConfigurationName(id)); err != nil {
		return err
	}
	for _, query := range []string{"DELETE FROM mailbox_subscriptions WHERE account=?", "DELETE FROM cursors WHERE account=?"} {
		if _, err := tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
