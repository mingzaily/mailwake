package storage

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

type AppInvitation struct {
	ID          string
	TokenHash   string
	Management  bool
	Scopes      []string
	PairingID   string
	DeviceName  string
	ExpiresAt   int64
	UsedAt      int64
	CancelledAt int64
	CreatedAt   int64
}

type AppController struct {
	ID         string
	DeviceID   string
	DeviceName string
	Scopes     []string
	CreatedAt  int64
	LastUsedAt int64
}

func (c AppController) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (s *Store) CreateAppInvitation(ctx context.Context, i AppInvitation) error {
	scopes, err := json.Marshal(i.Scopes)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Finished invitations are only shown while the administrator's dialog is open.
	if _, err = tx.ExecContext(ctx, "DELETE FROM app_invitations WHERE expires_at<=?", time.Now().Unix()-86400); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO app_invitations(id,token_hash,management,scopes,pairing_id,expires_at,created_at) VALUES(?,?,?,?,?,?,?)", i.ID, i.TokenHash, i.Management, string(scopes), i.PairingID, i.ExpiresAt, i.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

const invitationColumns = "id,token_hash,management,scopes,pairing_id,device_name,expires_at,used_at,cancelled_at,created_at"

func scanInvitation(row interface{ Scan(...any) error }) (AppInvitation, error) {
	var i AppInvitation
	var scopes string
	err := row.Scan(&i.ID, &i.TokenHash, &i.Management, &scopes, &i.PairingID, &i.DeviceName, &i.ExpiresAt, &i.UsedAt, &i.CancelledAt, &i.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return i, fault.New("invitation_not_found")
	}
	if err != nil {
		return i, err
	}
	return i, json.Unmarshal([]byte(scopes), &i.Scopes)
}

func (s *Store) AppInvitation(ctx context.Context, id string) (AppInvitation, error) {
	return scanInvitation(s.db.QueryRowContext(ctx, "SELECT "+invitationColumns+" FROM app_invitations WHERE id=?", id))
}

func (s *Store) CancelAppInvitation(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE app_invitations SET cancelled_at=? WHERE id=? AND cancelled_at=0", time.Now().Unix(), id)
	return err
}

// AcceptAppInvitation consumes the one-time management token and installs the
// controller in one transaction. A device accepting again replaces its old controller.
func (s *Store) AcceptAppInvitation(ctx context.Context, invitationID, tokenHash string, c AppController, credentialHash string) (AppController, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	i, err := scanInvitation(tx.QueryRowContext(ctx, "SELECT "+invitationColumns+" FROM app_invitations WHERE id=?", invitationID))
	if err != nil {
		return c, err
	}
	if !i.Management || subtle.ConstantTimeCompare([]byte(i.TokenHash), []byte(tokenHash)) != 1 {
		return c, fault.New("unauthorized")
	}
	now := time.Now().Unix()
	switch {
	case i.UsedAt > 0:
		return c, fault.New("invitation_used")
	case i.CancelledAt > 0 || i.ExpiresAt <= now:
		return c, fault.New("invitation_expired")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE app_invitations SET used_at=?,device_name=? WHERE id=?", now, c.DeviceName, i.ID); err != nil {
		return c, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM app_controllers WHERE device_id=?", c.DeviceID); err != nil {
		return c, err
	}
	c.Scopes, c.CreatedAt = i.Scopes, now
	scopes, err := json.Marshal(c.Scopes)
	if err != nil {
		return c, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO app_controllers(id,device_id,device_name,scopes,credential_hash,created_at) VALUES(?,?,?,?,?,?)", c.ID, c.DeviceID, c.DeviceName, string(scopes), credentialHash, c.CreatedAt); err != nil {
		return c, err
	}
	return c, tx.Commit()
}

const controllerColumns = "id,device_id,device_name,scopes,created_at,last_used_at"

func scanController(row interface{ Scan(...any) error }) (AppController, error) {
	var c AppController
	var scopes string
	if err := row.Scan(&c.ID, &c.DeviceID, &c.DeviceName, &scopes, &c.CreatedAt, &c.LastUsedAt); err != nil {
		return c, err
	}
	return c, json.Unmarshal([]byte(scopes), &c.Scopes)
}

// AuthenticateAppController resolves a credential hash and records its use.
func (s *Store) AuthenticateAppController(ctx context.Context, credentialHash string) (AppController, error) {
	c, err := scanController(s.db.QueryRowContext(ctx, "UPDATE app_controllers SET last_used_at=? WHERE credential_hash=? RETURNING "+controllerColumns, time.Now().Unix(), credentialHash))
	if errors.Is(err, sql.ErrNoRows) {
		return c, fault.New("unauthorized")
	}
	return c, err
}

func (s *Store) AppControllers(ctx context.Context) ([]AppController, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+controllerColumns+" FROM app_controllers ORDER BY created_at DESC,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AppController{}
	for rows.Next() {
		c, err := scanController(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// DeleteAppController revokes a controller. Its credential stops working immediately.
func (s *Store) DeleteAppController(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM app_controllers WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fault.New("controller_not_found")
	}
	return nil
}
