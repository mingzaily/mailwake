package engine

import (
	"context"
	"fmt"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type scheduledPoll struct {
	name     string
	at       time.Time
	previous string
}
type scheduledSource struct {
	mu                           sync.Mutex
	opened, closed, active, peak int
	realtime, stopped            map[string]int
	arrivals                     map[string]int
	polls                        chan scheduledPoll
	openError                    error
}

func newScheduledSource() *scheduledSource {
	return &scheduledSource{realtime: map[string]int{}, stopped: map[string]int{}, arrivals: map[string]int{}, polls: make(chan scheduledPoll, 100)}
}
func (s *scheduledSource) Folders(context.Context) ([]string, error) { return nil, nil }
func (s *scheduledSource) Open(_ context.Context, name string) (mail.Session, error) {
	s.mu.Lock()
	s.realtime[name]++
	s.mu.Unlock()
	return &scheduledSession{source: s, name: name}, nil
}
func (s *scheduledSource) OpenScheduled(context.Context) (mail.FolderConnection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened++
	if s.openError != nil {
		return nil, s.openError
	}
	s.active++
	s.peak = max(s.peak, s.active)
	return &scheduledConnection{source: s}, nil
}

type scheduledConnection struct{ source *scheduledSource }

func (c *scheduledConnection) Select(_ context.Context, name string) (mail.Session, error) {
	return &scheduledSession{source: c.source, name: name, scheduled: true}, nil
}
func (c *scheduledConnection) Close() error {
	c.source.mu.Lock()
	defer c.source.mu.Unlock()
	c.source.closed++
	c.source.active--
	return nil
}

type scheduledSession struct {
	source    *scheduledSource
	name      string
	scheduled bool
}

func (s *scheduledSession) Poll(_ context.Context, previous string, _ bool) (mail.Batch, error) {
	s.source.mu.Lock()
	n := s.source.arrivals[s.name]
	s.source.mu.Unlock()
	s.source.polls <- scheduledPoll{s.name, time.Now(), previous}
	batch := mail.Batch{Checkpoint: strconv.Itoa(n)}
	if previous != "" {
		old, _ := strconv.Atoi(previous)
		for i := old + 1; i <= n; i++ {
			batch.Messages = append(batch.Messages, mail.Message{Key: fmt.Sprint(i), ReceivedAt: time.Now()})
		}
	}
	return batch, nil
}
func (s *scheduledSession) Wait(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}
func (s *scheduledSession) Mode() string { return "idle" }
func (s *scheduledSession) Close() error {
	s.source.mu.Lock()
	defer s.source.mu.Unlock()
	s.source.stopped[s.name]++
	return nil
}
func scheduledEngine(t *testing.T, source mail.Source, folders []mail.Folder) (*Engine, *storage.Store) {
	t.Helper()
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err := store.LoadSubscriptions(t.Context(), "mbx_test"); err != nil {
		t.Fatal(err)
	}
	state := mail.Subscriptions{Revision: 2, Folders: folders}
	if err := store.SaveSubscriptions(t.Context(), "mbx_test", state); err != nil {
		t.Fatal(err)
	}
	e := New(source, store, "mbx_test", state, time.Minute, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return e, store
}
func TestScheduledOrderIntervalsAndSingleConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newScheduledSource()
		e, _ := scheduledEngine(t, source, []mail.Folder{{Name: "B", Check: "15m"}, {Name: "A", Check: "5m"}})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { defer close(done); e.Run(ctx) }()
		defer func() { cancel(); <-done }()
		first, second := <-source.polls, <-source.polls
		if first.name != "A" || second.name != "B" || !first.at.Equal(second.at) {
			t.Fatal("initial scheduled order", first, second)
		}
		synctest.Wait()
		source.assertConnections(t, 1, 1, 1)
		for _, want := range []struct {
			name    string
			minutes int
		}{{"A", 5}, {"A", 10}, {"A", 15}, {"B", 15}} {
			got := <-source.polls
			if got.name != want.name || got.at.Sub(first.at) != time.Duration(want.minutes)*time.Minute {
				t.Fatal("deadline ordering", got, want)
			}
		}
		synctest.Wait()
		source.assertConnections(t, 4, 4, 1)
		for _, status := range e.Status() {
			if status.Check == "" || status.Mode != "scheduled" || status.NextCheck == nil || status.LastCheck == nil {
				t.Fatal("scheduled status", status)
			}
		}
	})
}
func TestCheckSwitchKeepsProgressAndOtherWatcher(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newScheduledSource()
		e, store := scheduledEngine(t, source, mail.RealtimeFolders([]string{"INBOX", "Other"}))
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { defer close(done); e.Run(ctx) }()
		defer func() { cancel(); <-done }()
		<-source.polls
		<-source.polls
		synctest.Wait()
		source.mu.Lock()
		source.arrivals["INBOX"] = 1
		source.mu.Unlock()
		next, err := e.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 2, Folders: []mail.Folder{{Name: "INBOX", Check: "5m"}, {Name: "Other", Check: mail.Realtime}}})
		if err != nil {
			t.Fatal(err)
		}
		got := <-source.polls
		if got.name != "INBOX" || got.previous != "0" {
			t.Fatal("switch lost baseline", got)
		}
		synctest.Wait()
		source.mu.Lock()
		source.arrivals["INBOX"] = 2
		source.mu.Unlock()
		next.Folders[0].Check = mail.Realtime
		if _, err := e.UpdateSubscriptions(t.Context(), next); err != nil {
			t.Fatal(err)
		}
		got = <-source.polls
		if got.name != "INBOX" || got.previous != "1" {
			t.Fatal("switch lost progress", got)
		}
		synctest.Wait()
		source.mu.Lock()
		otherOpened, otherClosed := source.realtime["Other"], source.stopped["Other"]
		source.mu.Unlock()
		if otherOpened != 1 || otherClosed != 0 {
			t.Fatal("switch interrupted another folder")
		}
		counts, err := store.Summary(t.Context())
		if err != nil || counts.Pending != 2 {
			t.Fatal("switch replayed or missed mail", counts, err)
		}
		if cp, _ := store.Checkpoint(t.Context(), "mbx_test", "INBOX"); cp != "2" {
			t.Fatal(cp)
		}
	})
}
func TestScheduledAuthPauseAndOneLoginPerRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newScheduledSource()
		source.openError = fault.New(mail.CodeAuthFailed)
		e, _ := scheduledEngine(t, source, []mail.Folder{{Name: "A", Check: "5m"}, {Name: "B", Check: "15m"}})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { defer close(done); e.Run(ctx) }()
		defer func() { cancel(); <-done }()
		synctest.Wait()
		source.assertConnections(t, 1, 0, 0)
		time.Sleep(29 * time.Minute)
		source.assertConnections(t, 1, 0, 0)
		time.Sleep(time.Minute)
		synctest.Wait()
		source.assertConnections(t, 2, 0, 0)
		for _, status := range e.Status() {
			if status.State != "auth_required" || status.NextCheck == nil {
				t.Fatal(status)
			}
		}
	})
}

func (s *scheduledSource) assertConnections(t *testing.T, opened, closed, peak int) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opened != opened || s.closed != closed || s.peak != peak {
		t.Fatalf("connection counts = %d/%d/%d, want %d/%d/%d", s.opened, s.closed, s.peak, opened, closed, peak)
	}
}

func (s *scheduledSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}
