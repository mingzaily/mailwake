package storage

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
)

func TestRevokedPairingConcurrencyAndPlanRace(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.db.Exec("INSERT INTO native_pairings(id,state,token_hash,expires_at,created_at) VALUES('p','active','',0,0)"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var changed atomic.Int32
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			yes, err := s.RevokeNativePairing(t.Context(), "p")
			if err != nil {
				t.Error(err)
			}
			if yes {
				changed.Add(1)
			}
		}()
	}
	wg.Wait()
	if changed.Load() != 1 {
		t.Fatal("state changed", changed.Load(), "times")
	}
	p, _ := s.NativePairing(t.Context(), "p")
	if p.RevokedAt == 0 {
		t.Fatal("missing revocation time")
	}
	if err = s.Enqueue(t.Context(), event.Notification{ID: "late-plan", ReceivedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveNativePlan(t.Context(), []NativeDelivery{{EventID: "late-plan", PairingID: "p", Envelope: "cipher", Signature: "sig"}}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.NativeDeliveries(t.Context(), "late-plan")
	if len(rows) != 1 || rows[0].State != "cancelled" || rows[0].Envelope != "" || rows[0].ErrorCode != "pairing_revoked" {
		t.Fatalf("stale active snapshot created pending work: %+v", rows)
	}
}
func TestNativeEnqueueRequiresActivePairing(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.db.Exec("INSERT INTO settings(name,value) VALUES('delivery_channel','native')"); err != nil {
		t.Fatal(err)
	}
	send := func(id string) {
		t.Helper()
		if err = s.Enqueue(t.Context(), event.Notification{ID: id, ReceivedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(want int) {
		t.Helper()
		var got int
		if err = s.db.QueryRow("SELECT COUNT(*) FROM outbox").Scan(&got); err != nil || got != want {
			t.Fatalf("outbox=%d want=%d %v", got, want, err)
		}
	}
	send("no-device")
	count(0)
	if _, err = s.db.Exec("INSERT INTO native_pairings(id,state,token_hash,expires_at,created_at) VALUES('p','active','',0,0)"); err != nil {
		t.Fatal(err)
	}
	send("active")
	count(1)
	if _, err = s.RevokeNativePairing(t.Context(), "p"); err != nil {
		t.Fatal(err)
	}
	if err = s.Commit(t.Context(), "mailbox", "INBOX", "", "next", []event.Notification{{ID: "revoked", ReceivedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := s.Checkpoint(t.Context(), "mailbox", "INBOX")
	if err != nil || checkpoint != "next" {
		t.Fatal("no-device mail did not advance checkpoint", err)
	}
	count(1)
	if _, err = s.db.Exec("UPDATE settings SET value='webhook' WHERE name='delivery_channel'"); err != nil {
		t.Fatal(err)
	}
	send("ordinary")
	count(2)
}

func TestRevocationTransactionRollsBack(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.db.Exec("INSERT INTO native_pairings(id,state,token_hash,expires_at,created_at) VALUES('p','active','',0,0)"); err != nil {
		t.Fatal(err)
	}
	if err = s.Enqueue(t.Context(), event.Notification{ID: "queued"}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveNativePlan(t.Context(), []NativeDelivery{{EventID: "queued", PairingID: "p", Envelope: "cipher"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("CREATE TRIGGER fail_cancel BEFORE UPDATE ON native_deliveries BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RevokeNativePairing(t.Context(), "p"); err == nil {
		t.Fatal("partial revocation committed")
	}
	p, err := s.NativePairing(t.Context(), "p")
	if err != nil || p.State != "active" || p.RevokedAt != 0 {
		t.Fatal("pairing escaped rollback")
	}
	rows, _ := s.NativeDeliveries(t.Context(), "queued")
	if len(rows) != 1 || rows[0].State != "pending" || rows[0].Envelope != "cipher" {
		t.Fatal("delivery escaped rollback")
	}
}

func TestOutboxFailurePreservesDeviceRevocation(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.db.Exec("INSERT INTO native_pairings(id,state,token_hash,expires_at,created_at) VALUES('p','active','',0,0)"); err != nil {
		t.Fatal(err)
	}
	if err = s.Enqueue(t.Context(), event.Notification{ID: "queued"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MarkNativeEvent(t.Context(), "queued", false); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveNativePlan(t.Context(), []NativeDelivery{{EventID: "queued", PairingID: "p", Envelope: "cipher"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RevokeNativePairing(t.Context(), "p"); err != nil {
		t.Fatal(err)
	}
	if err = s.Failed(t.Context(), "queued", fault.New("another_device_error"), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.NativeDeliveries(t.Context(), "queued")
	if len(rows) != 1 || rows[0].State != "cancelled" || rows[0].ErrorCode != "pairing_revoked" {
		t.Fatal("outbox overwrote terminal device reason", rows)
	}
}
