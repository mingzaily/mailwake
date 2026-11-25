package storage

import (
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
)

func TestOmittedNotificationsRemainDeviceBoundUntilAcknowledged(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	for _, sql := range []string{
		`INSERT INTO app_controllers(id,device_id,device_name,scopes,credential_hash,created_at,omit_activity_notification) VALUES('controller','phone','Phone','[]','credential',0,1)`,
		`INSERT INTO native_pairings(id,state,device_id,token_hash,expires_at,created_at) VALUES('pair','active','phone','',0,0)`,
	} {
		if _, err := s.db.ExecContext(t.Context(), sql); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"first", "expired", "revoked"} {
		if err := s.Enqueue(t.Context(), event.Notification{ID: id}); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveNativePlan(t.Context(), []NativeDelivery{{EventID: id, PairingID: "pair", Envelope: `{"ciphertext":"original"}`, Signature: "original"}}); err != nil {
			t.Fatal(err)
		}
		if err := s.NativeActivityNotification(t.Context(), id, "pair", "activity", `{"ciphertext":"quiet"}`, "signature"); err != nil {
			t.Fatal(err)
		}
		if err := s.Accepted(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(t.Context(), `UPDATE native_deliveries SET accepted_at=? WHERE event_id='expired'`, time.Now().Add(-8*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	// An accepted device delivery remains fetchable when the aggregate outbox fails.
	if err := s.Failed(t.Context(), "first", fault.New("delivery_failed"), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.PendingNativeNotifications(t.Context(), "other")
	if err != nil || len(items) != 0 {
		t.Fatalf("other device: %v %v", items, err)
	}
	if err := s.AcknowledgeNativeNotification(t.Context(), "other", "pair", "first"); err != nil {
		t.Fatal(err)
	}
	items, err = s.PendingNativeNotifications(t.Context(), "phone")
	if err != nil || len(items) != 2 || string(items[0].Envelope) != `{"ciphertext":"quiet"}` {
		t.Fatalf("retained: %v %v", items, err)
	}
	for range 2 {
		if err := s.AcknowledgeNativeNotification(t.Context(), "phone", "pair", "first"); err != nil {
			t.Fatal(err)
		}
	}
	var envelope string
	if err := s.db.QueryRowContext(t.Context(), `SELECT envelope FROM native_deliveries WHERE event_id='first'`).Scan(&envelope); err != nil || envelope != "" {
		t.Fatalf("ack cleanup: %s %v", envelope, err)
	}
	if _, err := s.RevokeNativePairing(t.Context(), "pair"); err != nil {
		t.Fatal(err)
	}
	items, err = s.PendingNativeNotifications(t.Context(), "phone")
	if err != nil || len(items) != 0 {
		t.Fatalf("revoked: %v %v", items, err)
	}
	if err := s.db.QueryRowContext(t.Context(), `SELECT envelope FROM native_deliveries WHERE event_id='revoked'`).Scan(&envelope); err != nil || envelope != "" {
		t.Fatalf("revocation cleanup: %s %v", envelope, err)
	}
}
