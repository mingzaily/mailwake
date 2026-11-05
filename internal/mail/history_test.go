package mail

import (
	"context"
	"errors"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
)

type historyBudgetSource struct {
	budgetTestSource
	readHistory func(context.Context) (HistoryBatch, error)
}

func (s historyBudgetSource) ReadHistory(ctx context.Context, _ string, _ HistoryScan) (HistoryBatch, error) {
	return s.readHistory(ctx)
}

func TestHistoryLeavesWatcherCapacityAndReleasesCancelledLease(t *testing.T) {
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
	go func() { _, err := source.(HistorySource).ReadHistory(ctx, "INBOX", HistoryScan{}); done <- err }()
	<-entered
	if _, err := source.(HistorySource).ReadHistory(t.Context(), "INBOX", HistoryScan{}); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("concurrent history admitted", err)
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
