package storage

import (
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
)

func TestClearDeliveryHistoryRetainsActiveWorkAndMailboxProgress(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ids := []string{"accepted", "dead", "pending", "retry", "checking", "queued", "sending", "native-pending", "activity", "finished-native"}
	notifications := make([]event.Notification, 0, len(ids))
	for _, id := range ids {
		notifications = append(notifications, event.Notification{ID: id})
	}
	if err := s.Commit(t.Context(), "box", "INBOX", "", "checkpoint", notifications); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE outbox SET state='accepted' WHERE id<>'pending'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE outbox SET state='dead' WHERE id IN ('dead','retry')`); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Retry(t.Context(), "retry"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	for _, item := range []struct {
		id, state string
		next      int64
	}{
		{"checking", "delivered", time.Now().UnixMilli()}, {"queued", "queued", 0}, {"sending", "sending", 0}, {"native-pending", "pending", 0}, {"finished-native", "delivered", 0},
	} {
		if _, err := s.db.Exec(`INSERT INTO native_deliveries(event_id,pairing_id,device_name,expires_at,state,next_check) VALUES(?,'phone','Phone',0,?,?)`, item.id, item.state, item.next); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO native_activities(event_id,pairing_id,request,expires_at) VALUES('activity','phone','{}',0)`); err != nil {
		t.Fatal(err)
	}
	n, err := s.ClearDeliveryHistory(t.Context())
	if err != nil || n != 3 {
		t.Fatalf("deleted=%d error=%v", n, err)
	}
	for _, id := range []string{"pending", "retry", "checking", "queued", "sending", "native-pending", "activity"} {
		var count int
		if err := s.db.QueryRow(`SELECT count(*) FROM outbox WHERE id=?`, id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("active %s: %d %v", id, count, err)
		}
	}
	if items, err := s.NativeDeliveries(t.Context(), "finished-native"); err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	if checkpoint, err := s.Checkpoint(t.Context(), "box", "INBOX"); err != nil || checkpoint != "checkpoint" {
		t.Fatal(checkpoint, err)
	}
	if n, err := s.ClearDeliveryHistory(t.Context()); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if ok, err := s.Retry(t.Context(), "dead"); err != nil || ok {
		t.Fatal("cleared failure retried", ok, err)
	}
}
