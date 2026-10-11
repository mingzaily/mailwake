package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
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
func TestDeliverySavingIsIndependentOfProviderAvailability(t *testing.T) {
	for _, channel := range []string{"bark", "pushover", "webhook"} {
		t.Run(channel, func(t *testing.T) {
			m, store, dir := managerFixture(t, nil)
			vault, err := OpenVault(dir, true)
			if err != nil {
				t.Fatal(err)
			}
			counter := &countingSender{}
			counter.fail.Store(true)
			m.newSender = func(Delivery) delivery.Sender { return counter }
			revision := m.DeliveryView()["revision"].(int64)
			input := DeliveryUpdate{Revision: &revision, Channel: channel, Preview: "off"}
			input.Bark.Key = ptr("bark-key")
			input.Pushover.Token = ptr("application")
			input.Pushover.User = ptr("user")
			input.Webhook.URL = ptr("https://hooks.example.org")
			input.Webhook.Secret = ptr(strings.Repeat("a", 32))
			if err := m.UpdateDelivery(t.Context(), input); err != nil {
				t.Fatal(err)
			}
			if counter.calls.Load() != 0 {
				t.Fatal("save sent a notification")
			}
			if err := m.TestSavedDelivery(t.Context()); fault.From(err, "").Code != "delivery_test_failed" {
				t.Fatal(err)
			}
			revision++
			input.Bark.Key = ptr("changed-bark")
			input.Pushover.User = ptr("changed-user")
			input.Webhook.URL = ptr("https://changed.example.org")
			if err := m.UpdateDelivery(t.Context(), input); err != nil {
				t.Fatal("failed test blocked saving", err)
			}
			if counter.calls.Load() != 1 {
				t.Fatal("update sent a notification")
			}
			encrypted, err := store.Configuration(t.Context(), "delivery")
			if err != nil {
				t.Fatal(err)
			}
			plain, err := vault.Open("delivery", encrypted)
			if err != nil {
				t.Fatal(err)
			}
			var saved Delivery
			if err := json.Unmarshal(plain, &saved); err != nil {
				t.Fatal(err)
			}
			record, err := store.ConfigurationRecord(t.Context(), "delivery")
			if err != nil {
				t.Fatal(err)
			}
			if record.Revision != revision+1 || saved.Channel != channel || saved.Webhook.URL != *input.Webhook.URL {
				t.Fatal("configuration was not persisted", saved.Revision)
			}
			if err := m.UpdateDelivery(t.Context(), input); fault.From(err, "").Code != "settings_conflict" {
				t.Fatal("stale revision accepted", err)
			}
			revision++
			invalid := input
			invalid.Channel = "webhook"
			invalid.Webhook.URL = ptr("http://public.example.org")
			if err := m.UpdateDelivery(t.Context(), invalid); err == nil {
				t.Fatal("invalid address accepted")
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
