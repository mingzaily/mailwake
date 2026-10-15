package storage

import (
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
)

func TestUnconfiguredHistoryKeepsTitleUnavailableAfterEnablingPreview(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	before := event.Notification{ID: "before-configuration", Subject: "Private subject", Sender: "sender@example.org", Code: "482913"}
	if err := s.Enqueue(t.Context(), before); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDelivery(t.Context(), []byte("synthetic"), "subject", "bark", false, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Due(t.Context(), time.Now().Add(time.Second))
	if err != nil || task == nil || task.Event.Subject != "" || task.Event.Sender != "" || task.Event.Code != "" {
		t.Fatalf("old preview restored: %+v %v", task, err)
	}
	if err := s.Accepted(t.Context(), before.ID); err != nil {
		t.Fatal(err)
	}
	for _, n := range []event.Notification{{ID: "with-title", Subject: "New subject"}, {ID: "empty-title", Subject: ""}} {
		if err := s.Enqueue(t.Context(), n); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.Recent(t.Context())
	if err != nil || len(items) != 3 {
		t.Fatalf("history: %+v %v", items, err)
	}
	for _, item := range items {
		if item.Message == nil {
			t.Fatal("missing history")
		}
		subject := item.Message.Subject
		switch item.ID {
		case before.ID:
			if subject != nil || item.State != "accepted" {
				t.Fatalf("unavailable title became empty: %+v", item)
			}
		case "with-title":
			if subject == nil || *subject != "New subject" {
				t.Fatal("new title lost")
			}
		case "empty-title":
			if subject == nil || *subject != "" {
				t.Fatal("genuine empty title lost")
			}
		}
	}
}
