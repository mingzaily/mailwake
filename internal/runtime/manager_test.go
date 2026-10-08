package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

type multiSource struct{ opened, closed atomic.Int32 }

func (*multiSource) Folders(context.Context) ([]string, error) { return []string{"INBOX"}, nil }
func (s *multiSource) Open(context.Context, string) (mail.Session, error) {
	s.opened.Add(1)
	return multiSession{s}, nil
}

type multiSession struct{ source *multiSource }

func (s multiSession) Poll(_ context.Context, previous string, _ bool) (mail.Batch, error) {
	if previous != "" {
		return mail.Batch{Checkpoint: previous}, nil
	}
	return mail.Batch{Checkpoint: "progress", Messages: []mail.Message{{Key: "same-uid", Sender: "private", Subject: "private", ReceivedAt: time.Now()}}}, nil
}
func (multiSession) Wait(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() }
func (s multiSession) Close() error                                  { s.source.closed.Add(1); return nil }
func (multiSession) Mode() string                                    { return "idle" }

type gatedMailbox struct {
	*multiSource
	started, release chan struct{}
}

func (s gatedMailbox) Folders(ctx context.Context) ([]string, error) {
	close(s.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return []string{"INBOX"}, nil
	}
}

func managerFixture(t *testing.T, factory func(settings.Mailbox) mail.Source) (*Manager, *storage.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	vault, err := settings.OpenVault(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := newManager(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), "", factory, func(settings.Delivery) delivery.Sender { return &countingSender{} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, store, dir
}
func addMailbox(t *testing.T, m *Manager, host string, limit int) (string, settings.MailboxUpdate) {
	t.Helper()
	input := settings.MailboxUpdate{Host: host, Port: 993, Username: "user@example.test", Label: ptr(host), Password: ptr("test-password"), ConnectionLimit: &limit}
	view, err := m.CreateMailbox(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.Revision = 1
	return view["id"].(string), input
}
func subscribeMailbox(t *testing.T, m *Manager, id string) {
	t.Helper()
	if _, err := m.UpdateSubscriptions(t.Context(), id, mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"INBOX"})}); err != nil {
		t.Fatal(err)
	}
}
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMultipleMailboxLifetimesEventsAndProgress(t *testing.T) {
	first, second, replacement := &multiSource{}, &multiSource{}, &multiSource{}
	sources := map[string]*multiSource{"first": first, "second": second, "replacement": replacement}
	m, store, dir := managerFixture(t, func(c settings.Mailbox) mail.Source { return sources[c.Host] })
	a, input := addMailbox(t, m, "first", 2)
	b, _ := addMailbox(t, m, "second", 2)
	subscribeMailbox(t, m, a)
	subscribeMailbox(t, m, b)
	waitFor(t, func() bool { counts, _ := store.Summary(t.Context()); return counts.Pending == 2 })
	for _, id := range []string{a, b} {
		if cp, _ := store.Checkpoint(t.Context(), id, "INBOX"); cp != "progress" {
			t.Fatal("mailbox progress not isolated", id, cp)
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "mailwake.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT payload FROM outbox")
	if err != nil {
		t.Fatal(err)
	}
	events := map[string]event.Notification{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var n event.Notification
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Fatal(err)
		}
		events[n.MailboxID] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(events) != 2 || events[a].ID == events[b].ID || events[a].Account != "first" || events[b].Account != "second" {
		t.Fatal("mailboxes share event IDs or labels", events)
	}
	input.Label = ptr("renamed")
	input.Password = nil
	if err := m.UpdateMailbox(t.Context(), a, input); err != nil {
		t.Fatal(err)
	}
	input.Revision++
	if cp, _ := store.Checkpoint(t.Context(), a, "INBOX"); cp != "progress" {
		t.Fatal("label change reset progress")
	}
	if second.closed.Load() != 0 || second.opened.Load() != 1 {
		t.Fatal("changing one mailbox interrupted another")
	}
	input.Host = "replacement"
	if err := m.UpdateMailbox(t.Context(), a, input); err != nil {
		t.Fatal(err)
	}
	input.Revision++
	waitFor(t, func() bool { return replacement.opened.Load() == 1 })
	if second.closed.Load() != 0 {
		t.Fatal("identity change interrupted another mailbox")
	}
	if err := m.DeleteMailbox(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if second.closed.Load() != 0 || second.opened.Load() != 1 {
		t.Fatal("delete interrupted another mailbox")
	}
	if cp, _ := store.Checkpoint(t.Context(), b, "INBOX"); cp != "progress" {
		t.Fatal("delete changed another progress")
	}
	if cp, _ := store.Checkpoint(t.Context(), a, "INBOX"); cp != "" {
		t.Fatal("deleted progress retained")
	}
	counts, err := store.Summary(t.Context())
	if err != nil || counts.Pending < 2 {
		t.Fatal("delete lost queued notifications", counts, err)
	}
	if _, err := m.Mailbox(a); fault.From(err, "").Code != "mailbox_not_found" {
		t.Fatal(err)
	}
}

func TestMailboxBudgetAndSettingsRevisionsAreIndependent(t *testing.T) {
	first, second := &multiSource{}, &multiSource{}
	started, release := make(chan struct{}), make(chan struct{})
	m, _, _ := managerFixture(t, func(c settings.Mailbox) mail.Source {
		base := first
		if c.Host == "second" {
			base = second
		}
		if c.Password == "slow" {
			return gatedMailbox{base, started, release}
		}
		return base
	})
	a, input := addMailbox(t, m, "first", 2)
	b, other := addMailbox(t, m, "second", 2)
	subscribeMailbox(t, m, a)
	subscribeMailbox(t, m, b)
	waitFor(t, func() bool { return first.opened.Load() == 1 && second.opened.Load() == 1 })
	for _, id := range []string{a, b} {
		view, err := m.Mailbox(id)
		if err != nil || view["connections_in_use"] != 1 {
			t.Fatal(view, err)
		}
		if _, err := m.Folders(t.Context(), id); err != nil {
			t.Fatal("reserved management slot unavailable", err)
		}
	}
	if _, err := m.UpdateSubscriptions(t.Context(), a, mail.Subscriptions{Revision: 2, Folders: mail.RealtimeFolders([]string{"INBOX", "extra"})}); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("watchers consumed management slot", err)
	}
	input.Password = ptr("slow")
	done := make(chan error, 1)
	go func() { done <- m.UpdateMailbox(t.Context(), a, input) }()
	<-started
	if _, err := m.Folders(t.Context(), a); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("source exceeded total budget", err)
	}
	if _, err := m.Folders(t.Context(), b); err != nil {
		t.Fatal("another mailbox used this budget", err)
	}
	if err := m.UpdateMailbox(t.Context(), b, other); err != nil {
		t.Fatal("another mailbox revision conflicted", err)
	}
	revision := int64(0)
	d := settings.DeliveryUpdate{Revision: &revision, Channel: "bark", Preview: "off"}
	d.Bark.Key = ptr("synthetic")
	if err := m.UpdateDelivery(t.Context(), d); err != nil {
		t.Fatal("delivery blocked by mailbox test", err)
	}
	statusDone := make(chan struct{})
	go func() { m.Status(); close(statusDone) }()
	select {
	case <-statusDone:
	case <-time.After(time.Second):
		t.Fatal("status blocked during testing")
	}
	if first.closed.Load() != 0 {
		t.Fatal("test stopped live watcher")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal("unrelated settings caused conflict", err)
	}
	input.Password = ptr("fresh")
	if err := m.UpdateMailbox(t.Context(), a, input); fault.From(err, "").Code != "settings_conflict" {
		t.Fatal("stale mailbox revision accepted", err)
	}
	if err := m.UpdateDelivery(t.Context(), d); fault.From(err, "").Code != "settings_conflict" {
		t.Fatal("stale delivery revision accepted", err)
	}
}

func TestMailboxTestDetectsSubscriptionChangesAndDeletion(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "subscription", true: "delete"}[remove], func(t *testing.T) {
			source := &multiSource{}
			started, release := make(chan struct{}), make(chan struct{})
			m, _, _ := managerFixture(t, func(c settings.Mailbox) mail.Source {
				if c.Password == "slow" {
					return gatedMailbox{source, started, release}
				}
				return source
			})
			id, input := addMailbox(t, m, "imap", 10)
			input.Password = ptr("slow")
			done := make(chan error, 1)
			go func() { done <- m.UpdateMailbox(t.Context(), id, input) }()
			<-started
			want := "settings_conflict"
			if remove {
				want = "mailbox_not_found"
				if err := m.DeleteMailbox(t.Context(), id); err != nil {
					t.Fatal(err)
				}
			} else {
				subscribeMailbox(t, m, id)
			}
			close(release)
			if err := <-done; fault.From(err, "").Code != want {
				t.Fatal("concurrent change missed", err, want)
			}
		})
	}
}

func TestMailboxFailedTestKeepsBothWatchers(t *testing.T) {
	source := &multiSource{}
	m, _, _ := managerFixture(t, func(c settings.Mailbox) mail.Source {
		if c.Password == "fail" {
			return observedSource{name: "fail"}
		}
		return source
	})
	a, input := addMailbox(t, m, "first", 10)
	b, _ := addMailbox(t, m, "second", 10)
	subscribeMailbox(t, m, a)
	subscribeMailbox(t, m, b)
	waitFor(t, func() bool { return source.opened.Load() == 2 })
	input.Password = ptr("fail")
	if err := m.UpdateMailbox(t.Context(), a, input); fault.From(err, "").Code != "imap_connection_failed" {
		t.Fatal(err)
	}
	if source.closed.Load() != 0 {
		t.Fatal("failed test stopped a watcher")
	}
}

func TestMailboxPersistenceFailureRestoresOnlyItsWatcher(t *testing.T) {
	for _, operation := range []string{"UPDATE", "DELETE"} {
		t.Run(operation, func(t *testing.T) {
			first, second := &multiSource{}, &multiSource{}
			m, store, dir := managerFixture(t, func(c settings.Mailbox) mail.Source {
				if c.Host == "second" {
					return second
				}
				return first
			})
			a, input := addMailbox(t, m, "first", 10)
			b, _ := addMailbox(t, m, "second", 10)
			subscribeMailbox(t, m, a)
			subscribeMailbox(t, m, b)
			waitFor(t, func() bool {
				aProgress, _ := store.Checkpoint(t.Context(), a, "INBOX")
				bProgress, _ := store.Checkpoint(t.Context(), b, "INBOX")
				return aProgress == "progress" && bProgress == "progress"
			})
			db, err := sql.Open("sqlite", filepath.Join(dir, "mailwake.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec("CREATE TRIGGER fail_write BEFORE " + operation + " ON configuration BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
				t.Fatal(err)
			}
			input.Password = ptr("replacement")
			if operation == "UPDATE" {
				err = m.UpdateMailbox(t.Context(), a, input)
			} else {
				err = m.DeleteMailbox(t.Context(), a)
			}
			if err == nil {
				t.Fatal("failure injection ineffective")
			}
			waitFor(t, func() bool { return first.opened.Load() == 2 })
			if second.closed.Load() != 0 || second.opened.Load() != 1 {
				t.Fatal("rollback interrupted another mailbox")
			}
			view, err := m.Mailbox(a)
			if err != nil || view["revision"] != int64(1) {
				t.Fatal("failed write advanced revision", view, err)
			}
			if cp, _ := store.Checkpoint(t.Context(), b, "INBOX"); cp != "progress" {
				t.Fatal("rollback changed another cursor")
			}
		})
	}
}

type codeRequestSource struct{ requests chan bool }

func (s codeRequestSource) Folders(context.Context) ([]string, error) { return []string{"INBOX"}, nil }
func (s codeRequestSource) Open(context.Context, string) (mail.Session, error) {
	return codeRequestSession{s}, nil
}

type codeRequestSession struct{ codeRequestSource }

func (s codeRequestSession) Poll(ctx context.Context, previous string, detectCodes bool) (mail.Batch, error) {
	select {
	case s.requests <- detectCodes:
		return mail.Batch{Checkpoint: "baseline"}, nil
	case <-ctx.Done():
		return mail.Batch{}, ctx.Err()
	}
}
func (codeRequestSession) Wait(context.Context, time.Duration) error { return nil }
func (codeRequestSession) Close() error                              { return nil }
func (codeRequestSession) Mode() string                              { return "poll" }

func TestCodeDetectionFollowsSavedAndUpdatedChannel(t *testing.T) {
	source := codeRequestSource{requests: make(chan bool)}
	factory := func(settings.Mailbox) mail.Source { return source }
	m, store, dir := managerFixture(t, factory)
	id, _ := addMailbox(t, m, "mail.example.org", 2)
	subscribeMailbox(t, m, id)
	m.Close()
	saved := settings.Delivery{Channel: "native", Preview: "off", Language: "en"}
	if err := m.settings.SaveDelivery(t.Context(), m.delivery, saved, 0); err != nil {
		t.Fatal(err)
	}
	vault, err := settings.OpenVault(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := newManager(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), "", factory, func(settings.Delivery) delivery.Sender { return &countingSender{} })
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	awaitRequest := func(want bool) {
		t.Helper()
		timeout := time.NewTimer(3 * time.Second)
		defer timeout.Stop()
		for {
			select {
			case got := <-source.requests:
				if got == want {
					return
				}
			case <-timeout.C:
				t.Fatalf("IMAP code detection never became %v", want)
			}
		}
	}
	awaitRequest(true)
	revision := restarted.delivery.Revision
	input := settings.DeliveryUpdate{Revision: &revision, Channel: "bark", Preview: "subject", Language: "en"}
	input.Bark.Key = ptr("synthetic-key")
	if err := restarted.UpdateDelivery(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	awaitRequest(false)
	if err := restarted.DeleteDelivery(t.Context(), revision+1); err != nil {
		t.Fatal(err)
	}
	awaitRequest(false)
}

func (*multiSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}

func (s gatedMailbox) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}

func (s codeRequestSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}
