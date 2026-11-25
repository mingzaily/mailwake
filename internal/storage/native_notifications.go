package storage

import (
	"context"
	"encoding/json"
	"time"
)

// NativeActivityNotification persists the accepted-activity decision before ordinary submission.
// Controller revocation and pairing revocation serialize with this update.
func (s *Store) NativeActivityNotification(ctx context.Context, eventID, pairingID, relayID, envelope, signature string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var omit bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM app_controllers c JOIN native_pairings p ON p.device_id=c.device_id WHERE p.id=? AND p.state='active' AND c.omit_activity_notification=1)`, pairingID).Scan(&omit)
	if err != nil {
		return err
	}
	state, deliveredID := "pending", ""
	if omit {
		state, deliveredID = "accepted", relayID
	}
	_, err = tx.ExecContext(ctx, `UPDATE native_deliveries SET envelope=?,content_signature=?,pull_pending=?,relay_id=?,state=?,accepted_at=?,next_check=0 WHERE event_id=? AND pairing_id=? AND state='pending' AND relay_id=''`, envelope, signature, omit, deliveredID, state, time.Now().UnixMilli(), eventID, pairingID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ActivityNotificationPreference(ctx context.Context, controllerID string) (bool, error) {
	var omit bool
	err := s.db.QueryRowContext(ctx, `SELECT omit_activity_notification FROM app_controllers WHERE id=?`, controllerID).Scan(&omit)
	return omit, err
}
func (s *Store) SetActivityNotificationPreference(ctx context.Context, controllerID string, omit bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE app_controllers SET omit_activity_notification=? WHERE id=?`, omit, controllerID)
	return err
}

type NativeNotification struct {
	EventID   string          `json:"event_id"`
	PairingID string          `json:"pairing_id"`
	Envelope  json.RawMessage `json:"envelope"`
	Signature string          `json:"content_signature"`
}

// PendingNativeNotifications returns only ciphertext addressed to this controller's device.
// The ordinary outbox retention bounds these records; code expiration affects presentation only.
func (s *Store) PendingNativeNotifications(ctx context.Context, deviceID string) ([]NativeNotification, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.event_id,d.pairing_id,d.envelope,d.content_signature
 FROM native_deliveries d JOIN native_pairings p ON p.id=d.pairing_id
 WHERE p.device_id=? AND p.state='active' AND d.pull_pending=1 AND d.accepted_at>?
 ORDER BY d.accepted_at,d.event_id,d.pairing_id LIMIT 100`, deviceID, time.Now().Add(-7*24*time.Hour).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []NativeNotification{}
	for rows.Next() {
		var item NativeNotification
		var envelope string
		if err := rows.Scan(&item.EventID, &item.PairingID, &envelope, &item.Signature); err != nil {
			return nil, err
		}
		item.Envelope = json.RawMessage(envelope)
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) AcknowledgeNativeNotification(ctx context.Context, deviceID, pairingID, eventID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE native_deliveries SET pull_pending=0,envelope='',content_signature='' WHERE event_id=? AND pairing_id=? AND pull_pending=1 AND pairing_id IN (SELECT id FROM native_pairings WHERE device_id=? AND state='active')`, eventID, pairingID, deviceID)
	return err
}
