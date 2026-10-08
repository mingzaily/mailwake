package storage

import (
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
)

func TestDeliveryMessageLifecycle(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"accepted", "native-dead", "pending"} {
		if err := s.Enqueue(t.Context(), event.Notification{ID: id, MailboxID: "mbx_work", Account: "Original name", Folder: "Clients", Subject: "Report", Sender: "private sender"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Accepted(t.Context(), "accepted"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkNativeEvent(t.Context(), "native-dead", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Failed(t.Context(), "native-dead", fault.New("native_no_devices"), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkNativeEvent(t.Context(), "native-test", true); err != nil {
		t.Fatal(err)
	}
	items, err := s.Recent(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		m := item.Message
		if m == nil {
			t.Fatal("message missing", item.ID)
		}
		if item.ID == "native-test" {
			if !m.Test {
				t.Fatal("test notification missing")
			}
			continue
		}
		if m.Subject == nil || *m.Subject != "Report" || m.MailboxID != "mbx_work" || m.MailboxLabel != "Original name" || m.Folder != "Clients" {
			t.Fatalf("lost message: %+v", m)
		}
		if item.State != "pending" {
			var payload string
			if err := s.db.QueryRow("SELECT payload FROM outbox WHERE id=?", item.ID).Scan(&payload); err != nil || payload != "{}" {
				t.Fatal("terminal payload retained", payload, err)
			}
		}
	}
	if err := s.SaveDelivery(t.Context(), []byte("encrypted"), "off", "webhook", true, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(t.Context(), event.Notification{ID: "late", Subject: "Late subject", Account: "Other"}); err != nil {
		t.Fatal(err)
	}
	items, err = s.Recent(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Message == nil {
			t.Fatal("message missing", item.ID)
		}
		if item.ID == "late" || item.ID == "native-test" {
			if item.Message.Subject != nil {
				t.Fatal("new record retained disabled title", item.ID)
			}
		} else if item.Message.Subject == nil || *item.Message.Subject != "Report" {
			t.Fatal("preview change rewrote historical title", item.ID)
		}
	}
	if err := s.Prune(t.Context(), time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM outbox WHERE state IN ('accepted','dead')").Scan(&count); err != nil || count != 0 {
		t.Fatal("summary outlived retention", count, err)
	}
}
