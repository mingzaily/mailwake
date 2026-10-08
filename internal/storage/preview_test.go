package storage

import (
	"testing"

	"github.com/mingzaily/mailwake/internal/event"
)

func TestDisablePreviewScrubsQueueAtomically(t *testing.T) {
	s, err := Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, state := range []string{"pending", "dead", "accepted"} {
		if _, err := s.db.Exec(`INSERT INTO outbox(id,payload,state,next_attempt,created_at) VALUES(?, '{"sender":"private-sender","subject":"private-subject","folder":"INBOX"}',?,0,0)`, state, state); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveDelivery(t.Context(), []byte("old-encrypted"), "subject", "webhook", false, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_scrub BEFORE UPDATE ON outbox BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDelivery(t.Context(), []byte("new-encrypted"), "off", "webhook", true, 1); err == nil {
		t.Fatal("injected failure succeeded")
	}
	saved, err := s.Configuration(t.Context(), "delivery")
	if err != nil || string(saved) != "old-encrypted" {
		t.Fatal("configuration escaped rollback", err)
	}
	var policy string
	if err := s.db.QueryRow("SELECT value FROM settings WHERE name='delivery_preview'").Scan(&policy); err != nil || policy != "subject" {
		t.Fatal("policy escaped rollback", err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_scrub"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDelivery(t.Context(), []byte("new-encrypted"), "off", "webhook", true, 1); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"pending", "dead", "accepted"} {
		var sender, subject *string
		var folder string
		if err := s.db.QueryRow("SELECT json_type(payload,'$.sender'),json_type(payload,'$.subject'),json_extract(payload,'$.folder') FROM outbox WHERE id=?", state).Scan(&sender, &subject, &folder); err != nil {
			t.Fatal(err)
		}
		if state != "accepted" && (sender != nil || subject != nil) {
			t.Fatal("preview retained", state)
		}
		if state == "accepted" && (sender == nil || subject == nil) {
			t.Fatal("accepted row rewritten")
		}
		if folder != "INBOX" {
			t.Fatal("other data removed")
		}
	}
	// A batch captured before disabling preview may commit after the scrub transaction.
	if err := s.Commit(t.Context(), "primary", "INBOX", "", "next", []event.Notification{{ID: "late", Sender: "late sender", Subject: "late subject"}}); err != nil {
		t.Fatal(err)
	}
	var hasPreview bool
	if err := s.db.QueryRow("SELECT json_type(payload,'$.sender') IS NOT NULL OR json_type(payload,'$.subject') IS NOT NULL FROM outbox WHERE id='late'").Scan(&hasPreview); err != nil || hasPreview {
		t.Fatal("late batch restored preview", err)
	}
}
