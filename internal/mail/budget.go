package mail

import (
	"context"
	"github.com/mingzaily/mailwake/internal/fault"
	"sync"
)

const DefaultConnectionLimit = 10
const MaxConnectionLimit = 100

func CheckConnectionBudget(folders []Folder, limit int) error {
	if limit < 2 || limit > MaxConnectionLimit {
		return fault.New("connection_limit_invalid")
	}
	if RequiredConnections(folders) > limit-1 {
		return fault.New("connection_budget_exceeded")
	}
	return nil
}

// ConnectionBudget accounts for selected-folder sessions and short-lived discovery
// or connection-test sessions. Every source for the same mailbox shares one budget.
type ConnectionBudget struct {
	mu                    sync.Mutex
	limit, used, watchers int
}

func NewConnectionBudget(limit int) *ConnectionBudget { return &ConnectionBudget{limit: limit} }
func (b *ConnectionBudget) InUse() int                { b.mu.Lock(); defer b.mu.Unlock(); return b.used }

// Resize changes admission for new connections. Existing test/discovery leases
// finish normally and remain counted across configuration generations.
func (b *ConnectionBudget) Resize(limit int)          { b.mu.Lock(); defer b.mu.Unlock(); b.limit = limit }
func (b *ConnectionBudget) Wrap(source Source) Source { return budgetSource{source, b} }
func (b *ConnectionBudget) acquire(ctx context.Context, watcher bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.limit || (watcher && b.watchers >= b.limit-1) {
		return fault.New("connection_budget_exceeded")
	}
	b.used++
	if watcher {
		b.watchers++
	}
	return nil
}
func (b *ConnectionBudget) release(watcher bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used--
	if watcher {
		b.watchers--
	}
}

type budgetSource struct {
	source Source
	budget *ConnectionBudget
}

func (s budgetSource) Folders(ctx context.Context) ([]string, error) {
	if err := s.budget.acquire(ctx, false); err != nil {
		return nil, err
	}
	defer s.budget.release(false)
	return s.source.Folders(ctx)
}
func (s budgetSource) Open(ctx context.Context, folder string) (Session, error) {
	if err := s.budget.acquire(ctx, true); err != nil {
		return nil, err
	}
	session, err := s.source.Open(ctx, folder)
	if err != nil {
		s.budget.release(true)
		return nil, err
	}
	return &budgetSession{Session: session, budget: s.budget}, nil
}

type budgetSession struct {
	Session
	budget *ConnectionBudget
	once   sync.Once
	err    error
}

func (s *budgetSession) Close() error {
	s.once.Do(func() { s.err = s.Session.Close(); s.budget.release(true) })
	return s.err
}

func (s budgetSource) OpenScheduled(ctx context.Context) (FolderConnection, error) {
	provider, ok := s.source.(ScheduledSource)
	if !ok {
		return nil, fault.New("scheduled_check_unavailable")
	}
	if err := s.budget.acquire(ctx, true); err != nil {
		return nil, err
	}
	conn, err := provider.OpenScheduled(ctx)
	if err != nil {
		s.budget.release(true)
		return nil, err
	}
	return &budgetConnection{FolderConnection: conn, budget: s.budget}, nil
}

type budgetConnection struct {
	FolderConnection
	budget *ConnectionBudget
	once   sync.Once
	err    error
}

func (c *budgetConnection) Close() error {
	c.once.Do(func() { c.err = c.FolderConnection.Close(); c.budget.release(true) })
	return c.err
}

func (s budgetSource) ReadContent(ctx context.Context, folder string, location Location) (Body, error) {
	if err := s.budget.acquire(ctx, false); err != nil {
		return Body{}, err
	}
	defer s.budget.release(false)
	return s.source.ReadContent(ctx, folder, location)
}
