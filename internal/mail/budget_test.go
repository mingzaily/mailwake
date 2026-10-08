package mail

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

type budgetTestSource struct {
	fail bool
	read func(context.Context) (Body, error)
}

func (b budgetTestSource) Folders(context.Context) ([]string, error) { return []string{"INBOX"}, nil }
func (b budgetTestSource) Open(context.Context, string) (Session, error) {
	if b.fail {
		return nil, errors.New("failed")
	}
	return budgetTestSession{}, nil
}

type budgetTestSession struct{}

func (budgetTestSession) Poll(context.Context, string, bool) (Batch, error) { return Batch{}, nil }
func (budgetTestSession) Wait(context.Context, time.Duration) error         { return nil }
func (budgetTestSession) Mode() string                                      { return "idle" }
func (budgetTestSession) Close() error                                      { return nil }
func TestSharedConnectionBudget(t *testing.T) {
	budget := NewConnectionBudget(3)
	a, b := budget.Wrap(budgetTestSource{}), budget.Wrap(budgetTestSource{})
	first, err := a.Open(t.Context(), "one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Open(t.Context(), "two")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Folders(t.Context()); err != nil {
		t.Fatal("reserved discovery failed", err)
	}
	if _, err := b.Open(t.Context(), "three"); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("watch exceeded budget", err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); first.Close() }()
	}
	wg.Wait()
	if budget.InUse() != 1 {
		t.Fatal("double release")
	}
	if _, err := a.Folders(t.Context()); err != nil {
		t.Fatal(err)
	}
	second.Close()
	if budget.InUse() != 0 {
		t.Fatal("connection leak")
	}
	failed := budget.Wrap(budgetTestSource{fail: true})
	if _, err := failed.Open(t.Context(), "x"); err == nil {
		t.Fatal("failure missing")
	}
	if budget.InUse() != 0 {
		t.Fatal("failed connection held budget")
	}
}
func TestNormalizeFoldersUsesSeparateBudget(t *testing.T) {
	names := make([]string, 11)
	for i := range names {
		names[i] = fmt.Sprint(i)
	}
	if _, err := NormalizeFolders(names); err != nil {
		t.Fatal("normalization imposed a fixed limit", err)
	}
	if err := CheckConnectionBudget(RealtimeFolders(names[:9]), DefaultConnectionLimit); err != nil {
		t.Fatal(err)
	}
	if err := CheckConnectionBudget(RealtimeFolders(names), DefaultConnectionLimit); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal(err)
	}
	if err := CheckConnectionBudget(RealtimeFolders(names), 12); err != nil {
		t.Fatal("custom budget rejected", err)
	}
}

func TestReservedManagementConnection(t *testing.T) {
	b := NewConnectionBudget(2)
	source := b.Wrap(budgetTestSource{})
	watch, err := source.Open(t.Context(), "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	if _, err := source.Open(t.Context(), "overflow"); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("watcher used reserved slot", err)
	}
	if _, err := source.Folders(t.Context()); err != nil {
		t.Fatal("discovery failed at full subscriptions", err)
	}
	if err := b.acquire(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if b.InUse() != 2 {
		t.Fatal("wrong total")
	}
	if _, err := source.Folders(t.Context()); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("total budget exceeded", err)
	}
	b.release(false)
	if err := CheckConnectionBudget(RealtimeFolders([]string{"INBOX", "extra"}), 2); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("subscription used reserved slot", err)
	}
	if err := CheckConnectionBudget(nil, 1); fault.From(err, "").Code != "connection_limit_invalid" {
		t.Fatal("limit one accepted", err)
	}
}

type scheduledBudgetSource struct{ budgetTestSource }

func (s scheduledBudgetSource) OpenScheduled(context.Context) (FolderConnection, error) {
	return scheduledBudgetConnection{}, nil
}

type scheduledBudgetConnection struct{}

func (scheduledBudgetConnection) Select(context.Context, string) (Session, error) {
	return budgetTestSession{}, nil
}
func (scheduledBudgetConnection) Close() error { return nil }
func TestScheduledRoundUsesOneReservedWatcherSlot(t *testing.T) {
	budget := NewConnectionBudget(3)
	source := budget.Wrap(scheduledBudgetSource{})
	live, err := source.Open(t.Context(), "live")
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	round, err := source.(ScheduledSource).OpenScheduled(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A", "B", "C"} {
		if _, err := round.Select(t.Context(), name); err != nil {
			t.Fatal(err)
		}
	}
	if budget.InUse() != 2 {
		t.Fatal("scheduled slot count", budget.InUse())
	}
	if _, err := source.Open(t.Context(), "overflow"); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("scheduled round used management slot", err)
	}
	if _, err := source.Folders(t.Context()); err != nil {
		t.Fatal("management slot lost", err)
	}
	round.Close()
	round.Close()
	if budget.InUse() != 1 {
		t.Fatal("scheduled lease not released once")
	}
}

func (b budgetTestSource) ReadContent(ctx context.Context, _ string, _ Location) (Body, error) {
	if b.read != nil {
		return b.read(ctx)
	}
	return Body{}, nil
}

func TestContentReadUsesReservedConnectionAndReleasesItOnCancellation(t *testing.T) {
	budget := NewConnectionBudget(2)
	entered := make(chan struct{})
	source := budget.Wrap(budgetTestSource{read: func(ctx context.Context) (Body, error) { close(entered); <-ctx.Done(); return Body{}, ctx.Err() }})
	watch, err := source.Open(t.Context(), "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := source.ReadContent(ctx, "INBOX", Location{UIDValidity: 1, UID: 1}); done <- err }()
	<-entered
	if _, err := source.Folders(t.Context()); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("concurrent read exceeded connection limit", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := source.Folders(t.Context()); err != nil {
		t.Fatal("cancelled read retained connection", err)
	}
}
