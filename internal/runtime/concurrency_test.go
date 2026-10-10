package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

type slowSource struct {
	observedSource
	started chan struct{}
	release chan struct{}
	failure bool
}

func (s slowSource) Folders(ctx context.Context) (*mail.FolderDiscovery, error) {
	close(s.started)
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if s.failure {
		return nil, errors.New("test failed")
	}
	return &mail.FolderDiscovery{Folders: []string{"INBOX"}}, nil
}
func responsiveStatus(t *testing.T, s *singleMailboxService) {
	t.Helper()
	done := make(chan struct{})
	go func() { s.Status(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Status blocked by network test")
	}
}
func TestMailboxTestingPreservesAvailabilityAndDetectsConflict(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "test failure", false: "conflict"}[fail], func(t *testing.T) {
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
			started, release := make(chan struct{}), make(chan struct{})
			s, err := newService(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), func(m Mailbox) mail.Source {
				base := observedSource{opened, &closed, m.Host}
				if m.Host == "slow" {
					return slowSource{base, started, release, fail}
				}
				return base
			}, sender)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err := s.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"INBOX"})}); err != nil {
				t.Fatal(err)
			}
			input := MailboxUpdate{Host: "initial", Port: 993, Username: "user", Password: ptr("synthetic password")}
			if err := s.UpdateMailbox(t.Context(), input); err != nil {
				t.Fatal(err)
			}
			awaitValue(t, opened, "initial")
			result := make(chan error, 1)
			input.Host = "slow"
			go func() { result <- s.UpdateMailbox(t.Context(), input) }()
			<-started
			responsiveStatus(t, s)
			if closed.Load() != 0 {
				t.Error("live watcher stopped during test")
			}
			if !fail {
				concurrent := input
				concurrent.Host = "winner"
				done := make(chan error, 1)
				go func() { done <- s.UpdateMailbox(t.Context(), concurrent) }()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(time.Second):
					t.Error("concurrent update blocked")
				}
			}
			close(release)
			err = <-result
			want := "settings_conflict"
			if fail {
				want = "imap_connection_failed"
			}
			if fault.From(err, "").Code != want {
				t.Fatal(err, want)
			}
			if fail && closed.Load() != 0 {
				t.Fatal("failed test stopped live watcher")
			}
		})
	}
}

type slowSender struct{ started, release chan struct{} }

func (s slowSender) Send(ctx context.Context, _ event.Notification) error {
	close(s.started)
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestDeliveryTestingPreservesAvailabilityAndDetectsConflict(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := OpenVault(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	s, err := newService(t.Context(), store, vault, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, func(d Delivery) delivery.Sender {
		if d.Bark.Key == "slow" {
			return slowSender{started, release}
		}
		return channelSender{d.Channel, make(chan string, 1)}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	input := DeliveryUpdate{Channel: "bark", Preview: "off"}
	input.Bark.Key = ptr("slow")
	result := make(chan error, 1)
	go func() { result <- s.UpdateDelivery(t.Context(), input) }()
	<-started
	responsiveStatus(t, s)
	winner := input
	winner.Bark.Key = ptr("winner")
	done := make(chan error, 1)
	go func() { done <- s.UpdateDelivery(t.Context(), winner) }()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		t.Error("concurrent update blocked")
	}
	close(release)
	if err := <-result; fault.From(err, "").Code != "settings_conflict" {
		t.Fatal(err)
	}
}

func TestFullSubscriptionsAllowMailboxManagement(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	vault, err := OpenVault(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan string, 4)
	var closed atomic.Int32
	s, err := newService(t.Context(), store, vault, slog.Default(), func(m Mailbox) mail.Source { return observedSource{opened, &closed, m.Host} }, sender)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	limit := 2
	input := MailboxUpdate{Host: "imap", Port: 993, Username: "user", Password: ptr("synthetic"), ConnectionLimit: &limit}
	if err := s.UpdateMailbox(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"INBOX"})}); err != nil {
		t.Fatal(err)
	}
	awaitValue(t, opened, "imap")
	if _, err := s.Folders(t.Context()); err != nil {
		t.Fatal("discovery", err)
	}
	if _, err := s.TestMailbox(t.Context(), input); err != nil {
		t.Fatal("test", err)
	}
	input.Password = ptr("replacement")
	if err := s.UpdateMailbox(t.Context(), input); err != nil {
		t.Fatal("password update", err)
	}
	awaitValue(t, opened, "imap")
}

func (s slowSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}
