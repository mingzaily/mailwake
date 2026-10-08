package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
)

func TestNativeCheckScheduleAndPrivacy(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n := event.Notification{ID: "native-event", Sender: "private", Subject: "private"}
	if err = s.Enqueue(ctx, n); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("INSERT INTO native_pairings(id,state,token_hash,expires_at,created_at) VALUES('pairing','active','',0,0)"); err != nil {
		t.Fatal(err)
	}
	s.MarkNativeEvent(ctx, n.ID, false)
	d := NativeDelivery{EventID: n.ID, PairingID: "pairing", Envelope: "ciphertext", Signature: "signature", RelayID: "relay-id", State: "queued"}
	if err = s.SaveNativePlan(ctx, []NativeDelivery{d}); err != nil {
		t.Fatal(err)
	}
	if err = s.NativeAccepted(ctx, d); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.NativeDeliveries(ctx, n.ID)
	d = rows[0]
	if d.NextCheck-d.AcceptedAt != 10000 {
		t.Fatal("first check")
	}
	if err = s.NativeChecked(ctx, d, "rejected", "apns_device_invalid"); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.NativeDeliveries(ctx, n.ID)
	d = rows[0]
	if d.NextCheck-d.AcceptedAt != 60000 {
		t.Fatal("second check")
	}
	if err = s.NativeChecked(ctx, d, "rejected", "apns_device_invalid"); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.NativeDeliveries(ctx, n.ID)
	if rows[0].NextCheck != 0 || rows[0].CheckStep != 2 {
		t.Fatal("polling did not stop")
	}
	if err = s.Failed(ctx, n.ID, fault.New("failed"), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	var payload string
	s.db.QueryRow("SELECT payload FROM outbox WHERE id=?", n.ID).Scan(&payload)
	if payload != "{}" {
		t.Fatal("native plaintext retained")
	}
	if err = s.Prune(ctx, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.NativeDeliveries(ctx, n.ID)
	if len(rows) != 0 {
		t.Fatal("orphan device delivery")
	}
}
func TestNativePairingDeviceCapacity(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 16; i++ {
		id := fmt.Sprint(i)
		p := NativePairing{ID: id, TokenHash: id, ExpiresAt: time.Now().Unix() + 300}
		if err = s.CreateNativePairing(ctx, p); err != nil {
			t.Fatal(err)
		}
		p.DeviceID = id
		if err = s.SelectNativeDevice(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err = s.FinishNativePairing(ctx, id, "active", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.CreateNativePairing(ctx, NativePairing{ID: "overflow", ExpiresAt: time.Now().Unix() + 300}); fault.From(err, "").Code != "native_pairing_limit" {
		t.Fatal(err)
	}
}
