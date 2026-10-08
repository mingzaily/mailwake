package storage

import (
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
)

func TestResetNativePushTransactionAndIsolation(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	s.SaveConfiguration(ctx, "native.relay", []byte("old binding"))
	s.SaveConfiguration(ctx, "native.identity", []byte("stable identity"))
	s.CreateNativePairing(ctx, NativePairing{ID: "phone", ExpiresAt: time.Now().Unix() + 300})
	s.FinishNativePairing(ctx, "phone", "active", "")
	for _, id := range []string{"ordinary", "native", "accepted"} {
		s.Enqueue(ctx, event.Notification{ID: id, Subject: "private"})
	}
	s.MarkNativeEvent(ctx, "native", false)
	s.MarkNativeEvent(ctx, "accepted", false)
	s.SaveNativePlan(ctx, []NativeDelivery{{EventID: "native", PairingID: "phone", Envelope: "private"}, {EventID: "accepted", PairingID: "phone", Envelope: "private"}})
	s.NativeAccepted(ctx, NativeDelivery{EventID: "accepted", PairingID: "phone", RelayID: "delivery", State: "queued"})
	s.Accepted(ctx, "accepted")
	if _, err = s.db.Exec(`UPDATE native_pairings SET state='expired' WHERE id='phone'`); err != nil {
		t.Fatal(err)
	}
	// Inject failures at successive destructive steps, after earlier writes occurred.
	for _, table := range []string{"native_pairings", "native_deliveries", "outbox"} {
		if _, err = s.db.Exec("CREATE TRIGGER fail_reset BEFORE DELETE ON " + table + " BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
			t.Fatal(err)
		}
		if err = s.ResetNativePush(ctx); err == nil {
			t.Fatal("partial reset committed")
		}
		var bindings, pairings, deliveries, outbox int
		s.db.QueryRow("SELECT COUNT(*) FROM configuration").Scan(&bindings)
		s.db.QueryRow("SELECT COUNT(*) FROM native_pairings").Scan(&pairings)
		s.db.QueryRow("SELECT COUNT(*) FROM native_deliveries").Scan(&deliveries)
		s.db.QueryRow("SELECT COUNT(*) FROM outbox").Scan(&outbox)
		if bindings != 2 || pairings != 1 || deliveries != 2 || outbox != 3 {
			t.Fatalf("partial reset escaped rollback: %d %d %d %d", bindings, pairings, deliveries, outbox)
		}
		s.db.Exec("DROP TRIGGER fail_reset")
	}
	var ordinaryBefore string
	if err = s.db.QueryRow("SELECT payload FROM outbox WHERE id='ordinary'").Scan(&ordinaryBefore); err != nil {
		t.Fatal(err)
	}
	if err = s.ResetNativePush(ctx); err != nil {
		t.Fatal(err)
	}
	var ordinaryAfter string
	if err = s.db.QueryRow("SELECT payload FROM outbox WHERE id='ordinary'").Scan(&ordinaryAfter); err != nil {
		t.Fatal(err)
	}
	if ordinaryBefore != ordinaryAfter {
		t.Fatal("ordinary payload changed")
	}
	var pairings int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM native_pairings").Scan(&pairings); err != nil || pairings != 0 {
		t.Fatal("pairings survived environment reset", pairings, err)
	}
	binding, _ := s.Configuration(ctx, "native.relay")
	identity, _ := s.Configuration(ctx, "native.identity")
	if binding != nil || string(identity) != "stable identity" {
		t.Fatal("binding/identity reset boundary")
	}
	records, _ := s.Recent(ctx)
	if len(records) != 2 {
		t.Fatal("ordinary or accepted history removed", records)
	}
	for _, r := range records {
		if r.ID == "ordinary" && r.State != "pending" {
			t.Fatal("ordinary record changed")
		}
	}
	pairs, _ := s.NativePairings(ctx, "waiting")
	if len(pairs) != 0 {
		t.Fatal("pairings survived")
	}
	due, _ := s.DueNativeChecks(ctx)
	if len(due) != 0 {
		t.Fatal("status checks survived reset")
	}
	rows, _ := s.NativeDeliveries(ctx, "accepted")
	if len(rows) != 0 {
		t.Fatal("pending native status survived")
	}
}
