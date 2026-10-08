package storage

import (
	"context"
	"time"
)

type NativeDelivery struct {
	EventID    string `json:"event_id"`
	PairingID  string `json:"pairing_id"`
	DeviceName string `json:"device_name"`
	Envelope   string `json:"-"`
	Signature  string `json:"-"`
	ExpiresAt  int64  `json:"-"`
	RelayID    string `json:"relay_id,omitempty"`
	State      string `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
	AcceptedAt int64  `json:"-"`
	CheckStep  int    `json:"-"`
	NextCheck  int64  `json:"-"`
}

const nativeDeliveryColumns = "event_id,pairing_id,device_name,envelope,content_signature,expires_at,relay_id,state,error_code,accepted_at,check_step,next_check"

func (s *Store) nativeDeliveries(ctx context.Context, where string, args ...any) ([]NativeDelivery, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+nativeDeliveryColumns+" FROM native_deliveries WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []NativeDelivery{}
	for rows.Next() {
		var d NativeDelivery
		if err = rows.Scan(&d.EventID, &d.PairingID, &d.DeviceName, &d.Envelope, &d.Signature, &d.ExpiresAt, &d.RelayID, &d.State, &d.ErrorCode, &d.AcceptedAt, &d.CheckStep, &d.NextCheck); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}
func (s *Store) NativeDeliveries(ctx context.Context, eventID string) ([]NativeDelivery, error) {
	return s.nativeDeliveries(ctx, "event_id=? ORDER BY pairing_id", eventID)
}
func (s *Store) DueNativeChecks(ctx context.Context) ([]NativeDelivery, error) {
	return s.nativeDeliveries(ctx, "next_check>0 AND next_check<=? ORDER BY next_check LIMIT 32", time.Now().UnixMilli())
}
func (s *Store) RecentNativeDeliveries(ctx context.Context) ([]NativeDelivery, error) {
	return s.nativeDeliveries(ctx, "event_id IN (SELECT id FROM outbox ORDER BY created_at DESC,id LIMIT 50) ORDER BY event_id,pairing_id")
}
func (s *Store) MarkNativeEvent(ctx context.Context, id string, test bool) (bool, error) {
	own := false
	if test {
		r, err := s.db.ExecContext(ctx, `INSERT INTO outbox(id,payload,state,attempts,next_attempt,created_at,channel,message) VALUES(?,'{}','dead',1,0,?,'native','{"mailbox_id":"","mailbox_label":"","folder":"","subject":null,"test":true}') ON CONFLICT DO NOTHING`, id, time.Now().UnixMilli())
		if err != nil {
			return false, err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return false, err
		}
		own = n == 1
	}
	_, err := s.db.ExecContext(ctx, "UPDATE outbox SET channel='native' WHERE id=?", id)
	return own, err
}
func (s *Store) SaveNativePlan(ctx context.Context, items []NativeDelivery, activities ...NativeActivity) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, d := range items {
		if _, err = tx.ExecContext(ctx, `INSERT INTO native_deliveries(event_id,pairing_id,device_name,expires_at,envelope,content_signature,state,error_code)
 SELECT ?,?,?,?,CASE WHEN state='active' THEN ? ELSE '' END,CASE WHEN state='active' THEN ? ELSE '' END,
 CASE WHEN state='active' THEN 'pending' ELSE 'cancelled' END,CASE WHEN state='active' THEN '' ELSE 'pairing_revoked' END
 FROM native_pairings WHERE id=?`, d.EventID, d.PairingID, d.DeviceName, d.ExpiresAt, d.Envelope, d.Signature, d.PairingID); err != nil {
			return err
		}
	}
	for _, item := range activities {
		if _, err = tx.ExecContext(ctx, `INSERT INTO native_activities(event_id,pairing_id,request,expires_at) VALUES(?,?,?,?)`, item.EventID, item.PairingID, item.Request, item.ExpiresAt); err != nil {
			return err
		}
	}

	return tx.Commit()
}
func (s *Store) NativeAccepted(ctx context.Context, d NativeDelivery) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, "UPDATE native_deliveries SET relay_id=?,state=?,error_code='',accepted_at=?,next_check=?,envelope='',content_signature='' WHERE event_id=? AND pairing_id=? AND relay_id='' AND state='pending'", d.RelayID, d.State, now, now+10000, d.EventID, d.PairingID)
	return err
}
func (s *Store) NativeRejected(ctx context.Context, d NativeDelivery, code string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE native_deliveries SET state='failed',error_code=?,envelope='',content_signature='' WHERE event_id=? AND pairing_id=? AND relay_id='' AND state='pending'", code, d.EventID, d.PairingID)
	return err
}
func (s *Store) NativeChecked(ctx context.Context, d NativeDelivery, state, code string) error {
	step := d.CheckStep + 1
	next := int64(0)
	if step < 2 {
		next = d.AcceptedAt + 60000
	}
	_, err := s.db.ExecContext(ctx, "UPDATE native_deliveries SET state=?,error_code=?,check_step=?,next_check=? WHERE event_id=? AND pairing_id=? AND check_step=?", state, code, step, next, d.EventID, d.PairingID, d.CheckStep)
	return err
}
