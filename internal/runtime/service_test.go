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
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

type observedSource struct {
	opened chan string
	closed *atomic.Int32
	name   string
}

func (s observedSource) Folders(context.Context) (*mail.FolderDiscovery, error) {
	if s.name == "fail" {
		return nil, errors.New("synthetic credential private")
	}
	return &mail.FolderDiscovery{Folders: []string{"INBOX"}}, nil
}
func (s observedSource) Open(context.Context, string) (mail.Session, error) {
	s.opened <- s.name
	return observedSession{s.closed}, nil
}

type observedSession struct{ closed *atomic.Int32 }

func (s observedSession) Poll(_ context.Context, previous string, _ bool) (mail.Batch, error) {
	if previous == "" {
		return mail.Batch{Checkpoint: "baseline", Reset: true}, nil
	}
	return mail.Batch{Checkpoint: previous}, nil
}
func (s observedSession) Wait(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}
func (s observedSession) Close() error { s.closed.Add(1); return nil }
func (s observedSession) Mode() string { return "idle" }

type channelSender struct {
	name string
	sent chan string
}

func (s channelSender) Send(_ context.Context, n event.Notification) error {
	if !n.Test {
		s.sent <- s.name
	}
	return nil
}
func ptr(s string) *string { return &s }
func awaitValue(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatal(got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out", want)
	}
}
func TestMailboxReplacementAndEncryptedViews(t *testing.T) {
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
	opened := make(chan string, 20)
	var closed atomic.Int32
	factory := func(m Mailbox) mail.Source { return observedSource{opened, &closed, m.Host} }
	s, err := newService(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), factory, sender)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"INBOX"})}); err != nil {
		t.Fatal(err)
	}
	input := MailboxUpdate{Host: "first", Port: 993, Username: "user", Password: ptr("synthetic credential private")}
	if err := s.UpdateMailbox(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	awaitValue(t, opened, "first")
	// Wait until the first source establishes its durable baseline.
	deadline := time.Now().Add(3 * time.Second)
	for {
		cp, err := store.Checkpoint(t.Context(), "mbx_primary", "INBOX")
		if err != nil {
			t.Fatal(err)
		}
		if cp == "baseline" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("baseline missing")
		}
		time.Sleep(time.Millisecond)
	}
	if err := store.Commit(t.Context(), "mbx_primary", "INBOX", "baseline", "old-progress", nil); err != nil {
		t.Fatal(err)
	}
	input.Password = nil // Omitted credentials retain their encrypted value.
	if err := s.UpdateMailbox(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 0 {
		t.Fatal("label-only update closed connection")
	}
	if cp, err := store.Checkpoint(t.Context(), "mbx_primary", "INBOX"); err != nil || cp != "old-progress" {
		t.Fatal("password-only replacement reset progress", cp, err)
	}
	input.Host = "second"
	if err := s.UpdateMailbox(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	awaitValue(t, opened, "second")
	if closed.Load() < 1 {
		t.Fatal("old sessions still running")
	}
	if cp, err := store.Checkpoint(t.Context(), "mbx_primary", "INBOX"); err != nil || cp == "old-progress" {
		t.Fatal("old identity progress reused", cp, err)
	}
	beforeFailedTest := closed.Load()
	input.Host = "fail"
	if err := s.UpdateMailbox(t.Context(), input); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("failed connection accepted or exposed")
	}
	if closed.Load() != beforeFailedTest {
		t.Fatal("failed test interrupted watchers")
	}
	view, _ := json.Marshal(s.MailboxView())
	if strings.Contains(string(view), "synthetic") || !strings.Contains(string(view), `"configured":true`) {
		t.Fatal("credential view", string(view))
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "mailwake.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var encrypted []byte
	if err := db.QueryRow("SELECT encrypted FROM configuration WHERE name='mailbox:mbx_primary'").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "synthetic credential private") {
		t.Fatal("database contains plaintext")
	}
	plain, err := vault.Open("mailbox:mbx_primary", encrypted)
	if err != nil || !strings.Contains(string(plain), "synthetic credential private") {
		t.Fatal("omitted credential lost", err)
	}
}
func TestDeliveryHotSwitchAndSecretRetention(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := OpenVault(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	sent := make(chan string, 10)
	s, err := newService(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, func(d Delivery) delivery.Sender { return channelSender{d.Channel, sent} })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := store.Enqueue(t.Context(), event.Notification{ID: "pending-before-configuration"}); err != nil {
		t.Fatal(err)
	}
	input := DeliveryUpdate{Channel: "bark", Preview: "off"}
	input.Bark.Key = ptr("synthetic-bark-credential")
	if err := s.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	awaitValue(t, sent, "bark")
	input = DeliveryUpdate{Channel: "webhook", Preview: "subject", RetryCount: 2}
	input.Webhook.URL = ptr("https://hooks.test/path?token=synthetic-route")
	input.Webhook.Secret = ptr(strings.Repeat("s", 32))
	if err := s.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(t.Context(), event.Notification{ID: "after-switch"}); err != nil {
		t.Fatal(err)
	}
	awaitValue(t, sent, "webhook")
	input.Webhook.URL = nil
	input.Webhook.Secret = nil
	if err := s.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	view, _ := json.Marshal(s.DeliveryView())
	for _, secret := range []string{"synthetic-bark", "synthetic-route", strings.Repeat("s", 32)} {
		if strings.Contains(string(view), secret) {
			t.Fatal("secret in view")
		}
	}
	var saved Delivery
	encrypted, err := store.Configuration(t.Context(), "delivery")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open("delivery", encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(plain, &saved); err != nil || saved.Webhook.Secret != strings.Repeat("s", 32) {
		t.Fatal("credential retention", err)
	}
}
func TestConfiguredConnectionBudget(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := OpenVault(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan string, 20)
	var closed atomic.Int32
	s, err := newService(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), func(m Mailbox) mail.Source { return observedSource{opened, &closed, m.Host} }, sender)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	names := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	if _, err := s.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders(names[:9])}); err != nil {
		t.Fatal("default budget rejected nine", err)
	}
	if _, err := s.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders(append(names, "11"))}); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("budget allowed eleven", err)
	}
	limit := 5
	input := MailboxUpdate{Host: "imap", Port: 993, Username: "user", Password: ptr("synthetic-password"), ConnectionLimit: &limit}
	if err := s.UpdateMailbox(t.Context(), input); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("limit reduction accepted", err)
	}
	limit = 12
	if err := s.UpdateMailbox(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders(append(names, "11"))}); err != nil {
		t.Fatal("custom budget rejected eleven", err)
	}
}

func (s observedSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}
