package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
)

func TestLabelOnlyKeepsLiveMailbox(t *testing.T) {
	source := &multiSource{}
	var probes atomic.Int32
	m, store, _ := managerFixture(t, func(settings.Mailbox) mail.Source { probes.Add(1); return source })
	id, input := addMailbox(t, m, "mail", 2)
	subscribeMailbox(t, m, id)
	waitFor(t, func() bool { cp, _ := store.Checkpoint(t.Context(), id, "INBOX"); return cp == "progress" })
	input.Password = nil
	input.Label = ptr("New label")
	if err := m.UpdateMailbox(t.Context(), id, input); err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 1 || source.opened.Load() != 1 || source.closed.Load() != 0 {
		t.Fatal("label update tested or reconnected")
	}
	if cp, _ := store.Checkpoint(t.Context(), id, "INBOX"); cp != "progress" {
		t.Fatal("label reset progress")
	}
	if m.Status()[0].MailboxLabel != "New label" {
		t.Fatal("live label not updated")
	}
	if err := m.UpdateMailbox(t.Context(), id, input); fault.From(err, "").Code != "settings_conflict" {
		t.Fatal("label bypassed revision", err)
	}
}
func TestConcurrentDuplicateMailboxes(t *testing.T) {
	m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return &multiSource{} })
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, host := range []string{"IMAP.EXAMPLE", "imap.example"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.CreateMailbox(t.Context(), settings.MailboxUpdate{Host: host, Port: 993, Username: host, Password: ptr("synthetic")})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if fault.From(err, "").Code != "mailbox_duplicate" {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || len(m.Mailboxes()) != 1 {
		t.Fatal("duplicate account accepted")
	}
	id, input := addMailbox(t, m, "other", 10)
	input.Host = "IMAP.EXAMPLE"
	input.Username = "IMAP.EXAMPLE"
	if err := m.UpdateMailbox(t.Context(), id, input); fault.From(err, "").Code != "mailbox_duplicate" {
		t.Fatal("duplicate update accepted", err)
	}
}

type discoverySource struct {
	*multiSource
	entered chan struct{}
	block   atomic.Bool
}

func (s *discoverySource) Folders(ctx context.Context) ([]string, error) {
	if s.block.Load() {
		s.entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []string{"INBOX"}, nil
}
func TestDiscoverySharedAdmissionAndCancellation(t *testing.T) {
	source := &discoverySource{multiSource: &multiSource{}, entered: make(chan struct{}, 2)}
	m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
	id, input := addMailbox(t, m, "mail", 10)
	source.block.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{}, 2)
	go func() { m.TestMailbox(ctx, "", input); done <- struct{}{} }()
	go func() { m.TestMailbox(ctx, id, input); done <- struct{}{} }()
	<-source.entered
	<-source.entered
	probeCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	for _, op := range []func() error{
		func() error { _, err := m.Folders(probeCtx, id); return err },
		func() error { _, err := m.TestMailbox(probeCtx, id, input); return err },
		func() error { _, err := m.TestMailbox(probeCtx, "", input); return err },
	} {
		if err := op(); fault.From(err, "").Code != "discovery_busy" {
			t.Fatal("global admission exceeded", err)
		}
	}
	cancel()
	<-done
	<-done
	source.block.Store(false)
	if _, err := m.Folders(t.Context(), id); err != nil {
		t.Fatal("cancel leaked admission", err)
	}
}

func (s *discoverySource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}
