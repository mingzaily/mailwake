package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

type Administrator struct{ Username, PasswordHash string }
type Session struct {
	Hash, CSRF            string
	CreatedAt, LastUsedAt int64
}

func (s *Store) Administrator(ctx context.Context) (*Administrator, error) {
	var a Administrator
	err := s.db.QueryRowContext(ctx, "SELECT username,password_hash FROM administrator WHERE id=1").Scan(&a.Username, &a.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}
func (s *Store) PrepareSetup(ctx context.Context, hash string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO setup_state(id,code_hash) SELECT 1,? WHERE NOT EXISTS(SELECT 1 FROM administrator) ON CONFLICT(id) DO UPDATE SET code_hash=excluded.code_hash`, hash)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
func (s *Store) CompleteSetup(ctx context.Context, codeHash string, a Administrator, session Session) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM administrator)").Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fault.New("setup_complete")
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM setup_state WHERE id=1 AND code_hash=?", codeHash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fault.New("setup_code_invalid")
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO administrator(id,username,password_hash) VALUES(1,?,?)", a.Username, a.PasswordHash); err != nil {
		return err
	}
	if err := insertSession(ctx, tx, session); err != nil {
		return err
	}
	return tx.Commit()
}
func insertSession(ctx context.Context, db executor, s Session) error {
	_, err := db.ExecContext(ctx, "INSERT INTO sessions(id_hash,csrf,created_at,last_used_at) VALUES(?,?,?,?)", s.Hash, s.CSRF, s.CreatedAt, s.LastUsedAt)
	return err
}
func (s *Store) Session(ctx context.Context, hash string, now time.Time) (*Session, error) {
	var session Session
	err := s.db.QueryRowContext(ctx, `UPDATE sessions SET last_used_at=? WHERE id_hash=? AND last_used_at>? AND created_at>? RETURNING id_hash,csrf,created_at,last_used_at`, now.Unix(), hash, now.Add(-7*24*time.Hour).Unix(), now.Add(-30*24*time.Hour).Unix()).Scan(&session.Hash, &session.CSRF, &session.CreatedAt, &session.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fault.New("unauthorized")
	}
	return &session, err
}

// CreateSession checks the password version inside the transaction, so a concurrent
// reset cannot issue a session authenticated with the previous password.
func (s *Store) CreateSession(ctx context.Context, passwordHash string, session Session) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	if err := tx.QueryRowContext(ctx, "SELECT password_hash FROM administrator WHERE id=1").Scan(&current); err != nil {
		return err
	}
	if current != passwordHash {
		return fault.New("unauthorized")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE created_at<=? OR last_used_at<=?", session.CreatedAt-int64(30*24*time.Hour/time.Second), session.CreatedAt-int64(7*24*time.Hour/time.Second)); err != nil {
		return err
	}
	if err := insertSession(ctx, tx, session); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id_hash=?", hash)
	return err
}
func (s *Store) ChangePassword(ctx context.Context, previous, next, keepSession string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE administrator SET password_hash=? WHERE id=1 AND password_hash=?", next, previous)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fault.New("unauthorized")
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE id_hash<>?", keepSession); err != nil {
		return err
	}
	return tx.Commit()
}

// ResetSetup keeps mailbox configuration, subscriptions and queued work. All
// administrator access is revoked before the next startup issues a setup code.
func (s *Store) ResetSetup(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"administrator", "sessions", "api_tokens", "setup_state", "app_invitations", "app_controllers"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	return tx.Commit()
}
