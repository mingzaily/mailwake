package mail

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

type historyBudgetSource struct {
	budgetTestSource
	readHistory func(context.Context) (HistoryBatch, error)
}

func (s historyBudgetSource) OpenHistory(context.Context, *atomic.Int64) (HistoryConnection, error) {
	return s, nil
}
func (s historyBudgetSource) Close() error                               { return nil }
func (s historyBudgetSource) Probe(context.Context, *atomic.Int64) error { return nil }
func (s historyBudgetSource) Disconnected() bool                         { return false }
func (s historyBudgetSource) ReadHistory(ctx context.Context, _ string, _ HistoryScan, _ *atomic.Int64) (HistoryBatch, error) {
	return s.readHistory(ctx)
}

func TestActiveHistoryRejectsAnotherPageAndKeepsWatcherCapacity(t *testing.T) {
	budget := NewConnectionBudget(3)
	entered := make(chan struct{})
	source := budget.Wrap(historyBudgetSource{readHistory: func(ctx context.Context) (HistoryBatch, error) {
		close(entered)
		<-ctx.Done()
		return HistoryBatch{}, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := source.ReadHistory(ctx, "INBOX", HistoryScan{}); done <- err }()
	<-entered
	_, err := source.ReadHistory(t.Context(), "Archive", HistoryScan{})
	failure := fault.From(err, "")
	if failure.Code != HistoryBusy || failure.Params["retry_after"] != "1" || budget.InUse() != 1 {
		t.Fatalf("concurrent history result %v, leases %d", failure, budget.InUse())
	}
	first, err := source.Open(t.Context(), "one")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := source.Open(t.Context(), "two")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if budget.InUse() != 2 {
		t.Fatal("cancelled history retained its lease")
	}
	if _, err := source.Folders(t.Context()); err != nil {
		t.Fatal("management slot remained occupied", err)
	}
}

type historyTestProvider struct {
	budgetTestSource
	open func(context.Context, *atomic.Int64) (HistoryConnection, error)
}

func (p historyTestProvider) OpenHistory(ctx context.Context, remaining *atomic.Int64) (HistoryConnection, error) {
	return p.open(ctx, remaining)
}

type historyTestConnection struct {
	read         func(context.Context, string, HistoryScan, *atomic.Int64) (HistoryBatch, error)
	disconnected bool
	closed       bool
}

func (c *historyTestConnection) ReadHistory(ctx context.Context, folder string, scan HistoryScan, remaining *atomic.Int64) (HistoryBatch, error) {
	if c.read != nil {
		return c.read(ctx, folder, scan, remaining)
	}
	return HistoryBatch{Validity: 42}, nil
}
func (s *historyTestConnection) Probe(context.Context, *atomic.Int64) error { return nil }
func (c *historyTestConnection) Disconnected() bool                         { return c.disconnected }
func (c *historyTestConnection) Close() error {
	c.closed = true
	return nil
}

func TestHistoryReusesAcrossFoldersUntilIdleExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		budget := NewConnectionBudget(2)
		var connections []*historyTestConnection
		source := budget.Wrap(historyTestProvider{open: func(context.Context, *atomic.Int64) (HistoryConnection, error) {
			conn := &historyTestConnection{}
			connections = append(connections, conn)
			return conn, nil
		}})
		for _, folder := range []string{"INBOX", "INBOX", "Archive"} {
			if _, err := source.ReadHistory(t.Context(), folder, HistoryScan{}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(historyIdleTTL - time.Second)
		}
		if len(connections) != 1 || budget.InUse() != 1 {
			t.Fatalf("history used %d connections with %d leases", len(connections), budget.InUse())
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if budget.InUse() != 0 || !connections[0].closed {
			t.Fatal("idle history retained a connection")
		}
		if _, err := source.ReadHistory(t.Context(), "INBOX", HistoryScan{}); err != nil {
			t.Fatal(err)
		}
		source.RetireHistory()
		if len(connections) != 2 || budget.InUse() != 0 || !connections[1].closed {
			t.Fatal("idle expiry or retirement did not release a connection")
		}
	})
}

func TestIdleHistoryYieldsToManagementWithoutTakingWatcherCapacity(t *testing.T) {
	budget := NewConnectionBudget(2)
	conn := &historyTestConnection{}
	source := budget.Wrap(historyTestProvider{open: func(context.Context, *atomic.Int64) (HistoryConnection, error) {
		return conn, nil
	}})
	if _, err := source.ReadHistory(t.Context(), "INBOX", HistoryScan{}); err != nil {
		t.Fatal(err)
	}
	watcher, err := source.Open(t.Context(), "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	if conn.closed || budget.InUse() != 2 {
		t.Fatal("history and the watcher should coexist within the budget")
	}
	if _, err := source.Folders(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !conn.closed || budget.InUse() != 1 {
		t.Fatal("management could not reclaim the idle connection")
	}
}

func TestHistoryBackoffIsSharedAcrossFolders(t *testing.T) {
	for _, code := range []string{"imap_login_failed", "imap_connection_failed", "outlook_unavailable"} {
		t.Run(code, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				budget := NewConnectionBudget(2)
				var logins []time.Time
				source := budget.Wrap(historyTestProvider{open: func(context.Context, *atomic.Int64) (HistoryConnection, error) {
					logins = append(logins, time.Now())
					if len(logins) == 1 {
						return nil, fault.New(code)
					}
					return &historyTestConnection{}, nil
				}})
				defer source.RetireHistory()
				_, err := source.ReadHistory(t.Context(), "INBOX", HistoryScan{})
				failure := fault.From(err, "")
				if failure.Code != HistoryUpstreamUnavailable || budget.InUse() != 0 {
					t.Fatalf("failure %v, leases %d", err, budget.InUse())
				}
				seconds, err := strconv.Atoi(failure.Params["retry_after"])
				if err != nil || seconds < 5 || seconds > 10 {
					t.Fatal("retry-after", failure.Params)
				}
				_, err = source.ReadHistory(t.Context(), "Archive", HistoryScan{})
				if fault.From(err, "").Code != HistoryUpstreamUnavailable || len(logins) != 1 {
					t.Fatalf("backoff admitted a new login: %v, %v", err, logins)
				}
				time.Sleep(time.Duration(seconds) * time.Second)
				if _, err := source.ReadHistory(t.Context(), "Archive", HistoryScan{}); err != nil {
					t.Fatal(err)
				}
				if len(logins) != 2 || logins[1].Sub(logins[0]) < historyLoginInterval {
					t.Fatalf("unpaced login attempts: %v", logins)
				}
			})
		})
	}
}

func TestHistoryCredentialFailureStaysBlockedUntilSourceReplacement(t *testing.T) {
	for _, code := range []string{CodeAuthFailed, OutlookReauthorize} {
		t.Run(code, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				budget := NewConnectionBudget(2)
				logins := 0
				source := budget.Wrap(historyTestProvider{open: func(context.Context, *atomic.Int64) (HistoryConnection, error) {
					logins++
					return nil, fault.New(code)
				}})
				for _, folder := range []string{"INBOX", "Archive"} {
					_, err := source.ReadHistory(t.Context(), folder, HistoryScan{})
					failure := fault.From(err, "")
					if failure.Code != code || len(failure.Params) != 0 || budget.InUse() != 0 {
						t.Fatalf("credential refusal %v, leases %d", failure, budget.InUse())
					}
					time.Sleep(time.Minute)
				}
				if logins != 1 {
					t.Fatalf("credential failure allowed more logins: %d", logins)
				}
				source.RetireHistory()
				replacement := budget.Wrap(historyTestProvider{open: func(context.Context, *atomic.Int64) (HistoryConnection, error) {
					return &historyTestConnection{}, nil
				}})
				defer replacement.RetireHistory()
				if _, err := replacement.ReadHistory(t.Context(), "INBOX", HistoryScan{}); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestHistoryReconnectsOnceWithinTheSamePageBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		budget := NewConnectionBudget(2)
		const transfer = 800 << 10
		var connections []*historyTestConnection
		source := budget.Wrap(historyTestProvider{open: func(_ context.Context, remaining *atomic.Int64) (HistoryConnection, error) {
			want := int64(HistoryReadBytes - len(connections)*transfer)
			if remaining.Load() != want {
				t.Fatalf("login byte budget %d, want %d", remaining.Load(), want)
			}
			conn := &historyTestConnection{read: func(_ context.Context, _ string, _ HistoryScan, remaining *atomic.Int64) (HistoryBatch, error) {
				remaining.Add(-transfer)
				return HistoryBatch{}, fault.New("imap_connection_closed")
			}, disconnected: true}
			connections = append(connections, conn)
			return conn, nil
		}})
		if _, err := source.ReadHistory(t.Context(), "INBOX", HistoryScan{}); fault.From(err, "").Code != "imap_connection_closed" {
			t.Fatal(err)
		}
		if budget.InUse() != 0 || len(connections) != 2 || !connections[0].closed || !connections[1].closed {
			t.Fatal("reconnect retained a lease or retried again")
		}
	})
}
