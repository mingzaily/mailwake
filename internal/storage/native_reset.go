package storage

import "context"

// NativeResetTargets lists pairings that may still exist at the configured Relay.
func (s *Store) NativeResetTargets(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM native_pairings WHERE device_id<>'' AND (state IN ('waiting','active') OR unlink_pending=1) ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ResetNativePush clears the Relay binding and all Native state. Delivery
// history and ordinary-channel outbox records keep their own lifecycle.
func (s *Store) ResetNativePush(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		"DELETE FROM configuration WHERE name='native.relay'",
		"DELETE FROM native_pairings",
		"DELETE FROM native_activities",
		"DELETE FROM native_deliveries WHERE next_check>0 OR state IN ('pending','queued','sending')",
		"DELETE FROM outbox WHERE channel='native' AND state='pending'",
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// InitializeConfiguration preserves the first committed identity or binding.
func (s *Store) InitializeConfiguration(ctx context.Context, name string, encrypted []byte) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO configuration(name,encrypted) VALUES(?,?) ON CONFLICT DO NOTHING", name, encrypted)
	return err
}
