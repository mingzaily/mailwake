package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_baseline.sql
var baseline string

var ErrCheckpointConflict = fault.New("checkpoint_conflict")

type Store struct {
	db   *sql.DB
	lock *flock.Flock
}

func Open(ctx context.Context, dir string) (_ *Store, err error) {
	defer func() {
		switch {
		case errors.Is(err, os.ErrPermission):
			err = fault.New("data_directory_permission_denied")
		case errors.Is(err, syscall.EROFS):
			err = fault.New("data_directory_read_only")
		case err != nil:
			err = fault.From(err, "storage_open_failed")
		}
	}()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(dir, "core.lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fault.New("data_directory_locked")
	}
	path, err := filepath.Abs(filepath.Join(dir, "mailwake.db"))
	if err != nil {
		lock.Close()
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		lock.Close()
		return nil, err
	}
	err = f.Chmod(0600)
	f.Close()
	if err != nil {
		lock.Close()
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{}
	for _, pragma := range []string{"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)", "foreign_keys(ON)", "secure_delete(ON)"} {
		q.Add("_pragma", pragma)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		lock.Close()
		return nil, err
	}
	// One local writer keeps short SQLite transactions serialized. Network I/O
	// always happens after the transaction releases this connection.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, lock: lock}
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err == nil {
		switch version {
		case 0:
			err = s.applyBaseline(ctx)
		case 1:
		default:
			err = fault.New("database_version_unsupported")
		}
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// applyBaseline creates a new database. The product has no released schema to migrate.
func (s *Store) applyBaseline(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, baseline); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error                   { return errors.Join(s.db.Close(), s.lock.Close()) }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) Checkpoint(ctx context.Context, account, folder string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT checkpoint FROM cursors WHERE account=? AND folder=?", account, folder).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// Commit atomically queues events and advances the provider checkpoint.
func (s *Store) Commit(ctx context.Context, account, folder, previous, next string, events []event.Notification) error {
	if next == "" {
		return fault.New("checkpoint_required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRowContext(ctx, "SELECT checkpoint FROM cursors WHERE account=? AND folder=?", account, folder).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if current != previous {
		return ErrCheckpointConflict
	}
	now := time.Now().UnixMilli()
	for _, e := range events {
		if err := enqueue(ctx, tx, e, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO cursors(account,folder,checkpoint) VALUES(?,?,?) ON CONFLICT(account,folder) DO UPDATE SET checkpoint=excluded.checkpoint", account, folder, next); err != nil {
		return err
	}
	return tx.Commit()
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func enqueue(ctx context.Context, db executor, e event.Notification, now int64) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	message, err := json.Marshal(DeliveryMessage{MailboxID: e.MailboxID, MailboxLabel: e.Account, Folder: e.Folder, Subject: &e.Subject, Test: e.Test})
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO outbox(id,payload,next_attempt,created_at,message)
 SELECT ?,CASE WHEN (SELECT value FROM settings WHERE name='delivery_preview')='off' THEN json_remove(?,'$.sender','$.subject') ELSE ? END,?,?,
 CASE WHEN (SELECT value FROM settings WHERE name='delivery_preview')='off' THEN json_set(?,'$.subject',NULL) ELSE ? END
 WHERE ? OR COALESCE((SELECT value FROM settings WHERE name='delivery_channel'),'')<>'native' OR EXISTS(SELECT 1 FROM native_pairings WHERE state='active')
 ON CONFLICT(id) DO NOTHING`, e.ID, string(b), string(b), now, now, string(message), string(message), e.Test)
	return err
}

func (s *Store) Enqueue(ctx context.Context, e event.Notification) error {
	return enqueue(ctx, s.db, e, time.Now().UnixMilli())
}
