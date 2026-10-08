package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

type NativePairing struct {
	RevokedAt       int64  `json:"-"`
	ID              string `json:"id"`
	State           string `json:"status"`
	TokenHash       string `json:"-"`
	EncryptedToken  []byte `json:"-"`
	ExpiresAt       int64  `json:"-"`
	DeviceID        string `json:"device_id"`
	SigningKey      string `json:"-"`
	EncryptionKey   string `json:"-"`
	DeviceSignature string `json:"-"`
	DeviceName      string `json:"device_name"`
	ErrorCode       string `json:"error_code,omitempty"`
	CreatedAt       int64  `json:"-"`
}

const pairingColumns = "id,state,token_hash,encrypted_token,expires_at,device_id,signing_key,encryption_key,device_signature,device_name,error_code,created_at,revoked_at"

func scanPairing(row interface{ Scan(...any) error }) (NativePairing, error) {
	var p NativePairing
	err := row.Scan(&p.ID, &p.State, &p.TokenHash, &p.EncryptedToken, &p.ExpiresAt, &p.DeviceID, &p.SigningKey, &p.EncryptionKey, &p.DeviceSignature, &p.DeviceName, &p.ErrorCode, &p.CreatedAt, &p.RevokedAt)
	return p, err
}
func (s *Store) CreateNativePairing(ctx context.Context, p NativePairing) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	// Expired unselected requests release capacity. Selected requests reconcile with Relay first.
	if _, err = tx.ExecContext(ctx, "UPDATE native_pairings SET state='expired',encrypted_token=NULL,token_hash='',device_signature='' WHERE state='waiting' AND device_id='' AND expires_at<=?", now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM native_pairings WHERE state IN ('expired','failed') AND expires_at<=?", now-86400); err != nil {
		return err
	}
	var waiting, total int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FILTER(WHERE state='waiting'),COUNT(*) FROM native_pairings WHERE state IN ('waiting','active','revoked')").Scan(&waiting, &total); err != nil {
		return err
	}
	if waiting >= 3 || total >= 16 {
		return fault.New("native_pairing_limit")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO native_pairings(id,state,token_hash,encrypted_token,expires_at,created_at) VALUES(?,'waiting',?,?,?,?)", p.ID, p.TokenHash, p.EncryptedToken, p.ExpiresAt, p.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) NativePairing(ctx context.Context, id string) (NativePairing, error) {
	p, err := scanPairing(s.db.QueryRowContext(ctx, "SELECT "+pairingColumns+" FROM native_pairings WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, fault.New("native_pairing_not_found")
	}
	return p, err
}
func (s *Store) NativePairings(ctx context.Context, state string) ([]NativePairing, error) {
	return s.nativePairings(ctx, "state=?", state)
}
func (s *Store) NativeDevices(ctx context.Context) ([]NativePairing, error) {
	return s.nativePairings(ctx, "state IN ('active','revoked')")
}
func (s *Store) nativePairings(ctx context.Context, where string, args ...any) ([]NativePairing, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+pairingColumns+" FROM native_pairings WHERE "+where+" ORDER BY created_at,id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []NativePairing{}
	for rows.Next() {
		p, err := scanPairing(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}
func (s *Store) SelectNativeDevice(ctx context.Context, p NativePairing) error {
	r, err := s.db.ExecContext(ctx, `UPDATE native_pairings SET device_id=?,signing_key=?,encryption_key=?,device_signature=?,device_name=?
 WHERE id=? AND state='waiting' AND device_id='' AND expires_at>?
 AND NOT EXISTS(SELECT 1 FROM native_pairings WHERE device_id=? AND state IN ('waiting','active'))`, p.DeviceID, p.SigningKey, p.EncryptionKey, p.DeviceSignature, p.DeviceName, p.ID, time.Now().Unix(), p.DeviceID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return fault.New("native_pairing_conflict")
	}
	return err
}
func (s *Store) FinishNativePairing(ctx context.Context, id, state, code string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE native_pairings SET state=?,error_code=?,encrypted_token=NULL,token_hash='',device_signature='' WHERE id=? AND state='waiting'", state, code, id)
	return err
}

const cancelPairingDeliveries = "UPDATE native_deliveries SET state='cancelled',error_code='pairing_revoked',envelope='',content_signature='',next_check=0 WHERE pairing_id=? AND relay_id='' AND state='pending'"

// RevokeNativePairing records a revocation the device already made at Relay and
// cancels unsent work. The first observed revocation time is kept.
func (s *Store) RevokeNativePairing(ctx context.Context, id string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE native_pairings SET state='revoked',revoked_at=?,error_code='pairing_revoked' WHERE id=? AND state='active'", time.Now().Unix(), id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, cancelPairingDeliveries, id); err != nil {
		return false, err
	}
	return count == 1, tx.Commit()
}

// UnlinkNativePairing revokes a pairing locally and, when a device was selected,
// queues the Relay DELETE. A pairing without a device never reached Relay and is removed.
func (s *Store) UnlinkNativePairing(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM native_pairings WHERE id=? AND device_id=''", id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 1 {
		return tx.Commit()
	}
	result, err = tx.ExecContext(ctx, `UPDATE native_pairings SET state='revoked',revoked_at=CASE WHEN revoked_at=0 THEN ? ELSE revoked_at END,error_code='pairing_revoked',
 encrypted_token=NULL,token_hash='',device_signature='',unlink_pending=1,unlink_attempts=0,unlink_next_at=0 WHERE id=?`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fault.New("native_pairing_not_found")
	}
	if _, err = tx.ExecContext(ctx, cancelPairingDeliveries, id); err != nil {
		return err
	}
	return tx.Commit()
}

type NativeUnlink struct {
	ID       string
	Attempts int
}

func (s *Store) DueNativeUnlinks(ctx context.Context) ([]NativeUnlink, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,unlink_attempts FROM native_pairings WHERE unlink_pending=1 AND unlink_next_at<=? ORDER BY unlink_next_at,id LIMIT 16", time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []NativeUnlink{}
	for rows.Next() {
		var u NativeUnlink
		if err = rows.Scan(&u.ID, &u.Attempts); err != nil {
			return nil, err
		}
		items = append(items, u)
	}
	return items, rows.Err()
}

// FinishNativeUnlink removes the pairing after Relay confirmed the deletion.
func (s *Store) FinishNativeUnlink(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM native_pairings WHERE id=? AND unlink_pending=1", id)
	return err
}

func (s *Store) DelayNativeUnlink(ctx context.Context, id string, next time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE native_pairings SET unlink_attempts=unlink_attempts+1,unlink_next_at=? WHERE id=? AND unlink_pending=1", next.Unix(), id)
	return err
}
