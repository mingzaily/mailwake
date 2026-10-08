package storage

import "context"

type NativeActivity struct {
	EventID, PairingID, Request string
	ExpiresAt                   int64
}

func (s *Store) PendingNativeActivities(ctx context.Context) ([]NativeActivity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,pairing_id,request,expires_at FROM native_activities WHERE state='pending' LIMIT 32`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []NativeActivity
	for rows.Next() {
		var item NativeActivity
		if err := rows.Scan(&item.EventID, &item.PairingID, &item.Request, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) FinishNativeActivity(ctx context.Context, item NativeActivity, state string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE native_activities SET state=?,request='' WHERE event_id=? AND pairing_id=? AND state='pending'`, state, item.EventID, item.PairingID)
	return err
}
