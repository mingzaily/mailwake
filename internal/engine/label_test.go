package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

type labelSession struct {
	testSession
	key string
}

func (s labelSession) Poll(context.Context, string, bool) (mail.Batch, error) {
	return mail.Batch{Checkpoint: s.key, Messages: []mail.Message{{Key: s.key, ReceivedAt: time.Now()}}}, nil
}
func TestLiveLabelOnlyChangesFutureEvents(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := New(nil, store, "mbx_test", mail.Subscriptions{}, time.Minute, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.SetMailboxLabel("Before")
	if _, err := e.sync(t.Context(), "INBOX", labelSession{key: "1"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	e.SetMailboxLabel("After")
	if _, err := e.sync(t.Context(), "INBOX", labelSession{key: "2"}); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Before", "After"} {
		task, err := store.Due(t.Context(), time.Now())
		if err != nil || task == nil || task.Event.Account != label {
			raw, _ := json.Marshal(task)
			t.Fatalf("queued label = %s (%v), want %s", raw, err, label)
		}
		if err := store.Accepted(t.Context(), task.Event.ID); err != nil {
			t.Fatal(err)
		}
	}
}

type blockedCommit struct {
	testCheckpoints
	entered, release chan struct{}
	items            []event.Notification
}

func (s *blockedCommit) Commit(_ context.Context, _, _, _, _ string, items []event.Notification) error {
	s.items = items
	close(s.entered)
	<-s.release
	return nil
}
func TestLabelAndStatusRemainAvailableDuringDatabaseCommit(t *testing.T) {
	store := &blockedCommit{entered: make(chan struct{}), release: make(chan struct{})}
	e := New(nil, store, "mbx_test", mail.Subscriptions{}, time.Minute, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.SetMailboxLabel("Before")
	done := make(chan error, 1)
	go func() { _, err := e.sync(t.Context(), "INBOX", labelSession{key: "next"}); done <- err }()
	<-store.entered
	defer func() { close(store.release); <-done }()
	updated := make(chan struct{})
	go func() { e.SetMailboxLabel("After"); e.Status(); close(updated) }()
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Fatal("database commit held engine state lock")
	}
	if len(store.items) != 1 || store.items[0].Account != "Before" {
		t.Fatal("batch label snapshot changed", store.items)
	}
}
