package runtime

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

type countingSender struct {
	calls atomic.Int32
	fail  atomic.Bool
}

func (s *countingSender) Send(context.Context, event.Notification) error {
	s.calls.Add(1)
	if s.fail.Load() {
		return errors.New("offline")
	}
	return nil
}
func TestDeliveryTestsOnlyActiveConnectionChanges(t *testing.T) {
	for _, channel := range []string{"bark", "pushover", "webhook"} {
		t.Run(channel, func(t *testing.T) {
			store, err := storage.Open(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			vault, err := OpenVault(t.TempDir(), false)
			if err != nil {
				t.Fatal(err)
			}
			counter := &countingSender{}
			s, err := newService(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, func(Delivery) delivery.Sender { return counter })
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			input := DeliveryUpdate{Channel: channel, Preview: "off"}
			input.Bark.Key = ptr("bark-key")
			input.Pushover.Token = ptr("application")
			input.Pushover.User = ptr("user")
			input.Webhook.URL = ptr("https://hooks.example.org")
			input.Webhook.Secret = ptr(strings.Repeat("a", 32))
			if err := s.UpdateDelivery(t.Context(), input); err != nil {
				t.Fatal(err)
			}
			if counter.calls.Load() != 1 {
				t.Fatal("initial test missing")
			}
			counter.fail.Store(true)
			input.Preview = "subject"
			input.Language = "zh-CN"
			input.RetryCount = 3
			if err := s.UpdateDelivery(t.Context(), input); err != nil {
				t.Fatal("metadata depends on provider availability", err)
			}
			if counter.calls.Load() != 1 {
				t.Fatal("metadata sent test")
			}
			var changes []func(*DeliveryUpdate)
			switch channel {
			case "bark":
				changes = []func(*DeliveryUpdate){func(d *DeliveryUpdate) { d.Bark.Endpoint = "https://bark.example.org" }, func(d *DeliveryUpdate) { d.Bark.Key = ptr("new-key") }}
			case "pushover":
				changes = []func(*DeliveryUpdate){func(d *DeliveryUpdate) { d.Pushover.Token = ptr("new-token") }, func(d *DeliveryUpdate) { d.Pushover.User = ptr("new-user") }}
			case "webhook":
				changes = []func(*DeliveryUpdate){func(d *DeliveryUpdate) { d.Webhook.URL = ptr("https://new.example.org") }, func(d *DeliveryUpdate) { d.Webhook.Secret = ptr(strings.Repeat("b", 32)) }}
			}
			for i, change := range changes {
				candidate := input
				change(&candidate)
				if err := s.UpdateDelivery(t.Context(), candidate); fault.From(err, "").Code != "delivery_test_failed" {
					t.Fatal("changed connection skipped test", err)
				}
				if counter.calls.Load() != int32(i+2) {
					t.Fatal("incorrect test count")
				}
			}
			counter.fail.Store(false)
			if channel == "bark" {
				input.Pushover.User = ptr("inactive-change")
			} else {
				input.Bark.Key = ptr("inactive-change")
			}
			if err := s.UpdateDelivery(t.Context(), input); err != nil {
				t.Fatal(err)
			}
			if counter.calls.Load() != 3 {
				t.Fatal("inactive channel change sent test")
			}
			if channel == "bark" {
				input.Channel = "pushover"
			} else {
				input.Channel = "bark"
			}
			if err := s.UpdateDelivery(t.Context(), input); err != nil {
				t.Fatal(err)
			}
			if counter.calls.Load() != 4 {
				t.Fatal("channel switch skipped test")
			}
		})
	}
}

func TestDisablePreviewRemovesSavedMetadata(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newService(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, func(Delivery) delivery.Sender { return &countingSender{} })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	input := DeliveryUpdate{Channel: "bark", Preview: "subject"}
	input.Bark.Key = ptr("synthetic-key")
	if err := s.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "mailwake.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO outbox(id,payload,state,next_attempt,created_at) VALUES('dead','{"sender":"private","subject":"private","folder":"INBOX"}','dead',0,?)`, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	input.Preview = "off"
	if err := s.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	var hasPreview bool
	if err := db.QueryRow("SELECT json_type(payload,'$.sender') IS NOT NULL OR json_type(payload,'$.subject') IS NOT NULL FROM outbox WHERE id='dead'").Scan(&hasPreview); err != nil || hasPreview {
		t.Fatal("preview stored after disabling", err)
	}
}
