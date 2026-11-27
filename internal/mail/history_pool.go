package mail

import (
	"context"
	"math/rand/v2"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
)

const (
	historyIdleTTL             = 45 * time.Second
	historyLoginInterval       = time.Second
	historyBackoffBase         = 5 * time.Second
	historyBackoffMax          = 40 * time.Second
	HistoryUpstreamUnavailable = "upstream_unavailable"
)

// The budget lock protects admission, cancellation and the idle slot. The
// admitted reader alone updates login timing for its configuration generation.
type historyState struct {
	retired bool
	cancel  context.CancelFunc
	done    chan struct{}

	nextLogin, retryAt time.Time
	backoff            time.Duration
}

type idleHistory struct {
	source *BudgetedSource
	conn   HistoryConnection
	timer  *time.Timer
}

func (s *BudgetedSource) ReadHistory(ctx context.Context, folder string, scan HistoryScan) (HistoryBatch, error) {
	provider, ok := s.source.(HistoryProvider)
	if !ok {
		return HistoryBatch{}, fault.New("message_unavailable")
	}
	logging.RequestStage(ctx, "connection_budget")
	ctx, conn, err := s.beginHistory(ctx)
	if err != nil {
		return HistoryBatch{}, err
	}
	defer func() { s.finishHistory(ctx, conn) }()

	var remaining atomic.Int64
	remaining.Store(HistoryReadBytes)
	if conn == nil {
		conn, err = s.openHistory(ctx, provider, &remaining)
		if err != nil {
			return HistoryBatch{}, err
		}
	}
	result, err := conn.ReadHistory(ctx, folder, scan, &remaining)
	if err != nil && ctx.Err() == nil && remaining.Load() > 0 && conn.Disconnected() {
		conn.Close()
		conn, err = s.openHistory(ctx, provider, &remaining)
		if err != nil {
			return HistoryBatch{}, err
		}
		return conn.ReadHistory(ctx, folder, scan, &remaining)
	}
	return result, err
}

func (s *BudgetedSource) beginHistory(ctx context.Context) (context.Context, HistoryConnection, error) {
	b := s.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s.history.retired {
		return nil, nil, fault.New("sync_cursor_invalid")
	}
	var conn HistoryConnection
	if idle := b.idleHistory; idle != nil && idle.source == s {
		idle.timer.Stop()
		b.idleHistory = nil
		conn = idle.conn
	} else {
		if b.used >= b.limit || b.used > b.watchers {
			return nil, nil, fault.New("connection_budget_exceeded")
		}
		b.used++
	}
	ctx, s.history.cancel = context.WithCancel(ctx)
	s.history.done = make(chan struct{})
	return ctx, conn, nil
}

func (s *BudgetedSource) finishHistory(ctx context.Context, conn HistoryConnection) {
	b := s.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	defer close(s.history.done)
	defer s.history.cancel()
	s.history.done, s.history.cancel = nil, nil
	if conn == nil {
		b.used--
		return
	}
	if ctx.Err() != nil || conn.Disconnected() || b.used > b.limit {
		conn.Close()
		b.used--
		return
	}
	idle := &idleHistory{source: s, conn: conn}
	idle.timer = time.AfterFunc(historyIdleTTL, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.idleHistory == idle {
			b.closeIdleHistory()
		}
	})
	b.idleHistory = idle
}

// closeIdleHistory runs with the budget lock held. It closes the transport
// before making its slot available to another connection.
func (b *ConnectionBudget) closeIdleHistory() {
	idle := b.idleHistory
	idle.timer.Stop()
	idle.conn.Close()
	b.idleHistory = nil
	b.used--
}

// RetireHistory closes idle connections and cancels a page in progress. The
// returned channel signals that an admitted page has released its lease.
func (s *BudgetedSource) RetireHistory() <-chan struct{} {
	b := s.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	s.history.retired = true
	if s.history.cancel != nil {
		s.history.cancel()
	}
	if b.idleHistory != nil && b.idleHistory.source == s {
		b.closeIdleHistory()
	}
	return s.history.done
}

func (s *BudgetedSource) openHistory(ctx context.Context, provider HistoryProvider, remaining *atomic.Int64) (HistoryConnection, error) {
	h := &s.history
	if delay := time.Until(h.retryAt); delay > 0 {
		return nil, historyRetry(delay)
	}
	if delay := time.Until(h.nextLogin); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := provider.OpenHistory(ctx, remaining)
	h.nextLogin = time.Now().Add(historyLoginInterval)
	if err == nil {
		h.retryAt, h.backoff = time.Time{}, 0
		return conn, nil
	}
	switch fault.From(err, "message_unavailable").Code {
	case "imap_login_failed", "outlook_unavailable":
		if ctx.Err() == nil {
			h.backoff = min(max(historyBackoffBase, h.backoff*2), historyBackoffMax)
			delay := h.backoff + rand.N(h.backoff)
			h.retryAt = time.Now().Add(delay)
			return nil, historyRetry(delay)
		}
	}
	return nil, err
}

func historyRetry(delay time.Duration) *fault.Error {
	return &fault.Error{Code: HistoryUpstreamUnavailable, Params: map[string]string{
		"retry_after": strconv.FormatInt(int64((delay+time.Second-1)/time.Second), 10),
	}}
}
