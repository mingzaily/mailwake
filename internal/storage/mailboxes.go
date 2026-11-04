package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

const MaxMailboxes = 20

func MailboxConfigurationName(id string) string { return "mailbox:" + id }

func OutlookCredentialName(id string) string { return "outlook:" + id }

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

type MailboxRecord struct {
	ConfigurationRecord
	Outlook []byte
}

func (s *Store) MailboxConfigurations(ctx context.Context) ([]MailboxRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.name, m.revision, m.encrypted, o.encrypted
		FROM configuration m LEFT JOIN configuration o ON o.name='outlook:' || substr(m.name,length('mailbox:')+1)
		WHERE m.name LIKE 'mailbox:%' ORDER BY m.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []MailboxRecord{}
	for rows.Next() {
		var record MailboxRecord
		if err := rows.Scan(&record.Name, &record.Revision, &record.Encrypted, &record.Outlook); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type MailboxChange struct {
	InitialFolders []mail.Folder
	ResetCursors   bool
	// nil keeps the grant, an empty slice removes it, ciphertext replaces it.
	Outlook []byte
}

// WriteMailbox commits owner settings, initial subscriptions and any credential
// replacement together. A nil credential keeps the current authorization.
func (s *Store) WriteMailbox(ctx context.Context, id string, revision int64, encrypted []byte, change MailboxChange) error {
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
		folders := change.InitialFolders
		if folders == nil {
			folders = []mail.Folder{}
		}
		initial, err := json.Marshal(folders)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO mailbox_subscriptions(account,revision,folders) VALUES(?,1,?)", id, string(initial)); err != nil {
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
	if change.Outlook != nil {
		name := OutlookCredentialName(id)
		if len(change.Outlook) == 0 {
			_, err = tx.ExecContext(ctx, "DELETE FROM configuration WHERE name=?", name)
		} else {
			_, err = tx.ExecContext(ctx, "INSERT INTO configuration(name,encrypted) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET encrypted=excluded.encrypted", name, change.Outlook)
		}
		if err != nil {
			return err
		}
	}
	if change.ResetCursors {
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
	if _, err := tx.ExecContext(ctx, "DELETE FROM configuration WHERE name=?", OutlookCredentialName(id)); err != nil {
		return err
	}
	for _, query := range []string{"DELETE FROM mailbox_subscriptions WHERE account=?", "DELETE FROM cursors WHERE account=?"} {
		if _, err := tx.ExecContext(ctx, query, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RefreshMailboxDisplay keeps the owner's revision while updating derived folder metadata.
func (s *Store) RefreshMailboxDisplay(ctx context.Context, id string, revision int64, encrypted []byte) error {
	result, err := s.db.ExecContext(ctx, "UPDATE configuration SET encrypted=? WHERE name=? AND revision=?", encrypted, MailboxConfigurationName(id), revision)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fault.New("settings_conflict")
	}
	return nil
}

// RotateOutlookCredential replaces only the authorization that supplied this token.
// Concurrent reauthorization or deletion invalidates old connections' writes.
func (s *Store) RotateOutlookCredential(ctx context.Context, name string, previous, next []byte) error {
	result, err := s.db.ExecContext(ctx, "UPDATE configuration SET encrypted=? WHERE name=? AND encrypted=?", next, name, previous)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return fault.New("settings_conflict")
	}
	return err
}
