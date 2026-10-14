package storage

import (
	"context"
	"time"
)

type NativeActivity struct {
	EventID, PairingID, Request string
	ExpiresAt                   int64
	Failures                    int
}

func (s *Store) DueNativeActivities(ctx context.Context, now time.Time) ([]NativeActivity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,pairing_id,request,expires_at,failures FROM native_activities WHERE state='pending' AND next_attempt<=? ORDER BY next_attempt,event_id,pairing_id LIMIT 32`, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []NativeActivity
	for rows.Next() {
		var item NativeActivity
		if err := rows.Scan(&item.EventID, &item.PairingID, &item.Request, &item.ExpiresAt, &item.Failures); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) FinishNativeActivity(ctx context.Context, item NativeActivity, state string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE native_activities SET state=?,request='',next_attempt=0 WHERE event_id=? AND pairing_id=? AND state='pending'`, state, item.EventID, item.PairingID)
	return err
}

// DelayNativeActivity preserves the original request and deadline across restart.
func (s *Store) DelayNativeActivity(ctx context.Context, item NativeActivity, next time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE native_activities SET failures=failures+1,next_attempt=? WHERE event_id=? AND pairing_id=? AND state='pending'`, next.UnixMilli(), item.EventID, item.PairingID)
	return err
}
