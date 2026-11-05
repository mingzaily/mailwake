package engine

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

func TestHealthTimestampFollowsLifecycleAndRemovedWorkersStayAbsent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := New(nil, testCheckpoints{}, "account", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"INBOX"})}, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
		initial := e.Status()[0]
		time.Sleep(time.Second)
		e.update("INBOX", func(status *FolderStatus) {
			status.State = "reconnecting"
			status.LastError = fault.New("imap_command_timeout")
		})
		failed := e.Status()[0]
		if !failed.UpdatedAt.After(initial.UpdatedAt) || e.Status()[0].UpdatedAt != failed.UpdatedAt {
			t.Fatal("timestamp did not follow status update")
		}
		next, err := e.UpdateSubscriptions(t.Context(), mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Archive"})})
		if err != nil {
			t.Fatal(err)
		}
		e.update("INBOX", func(status *FolderStatus) { status.State = "stopped" })
		revision, statuses := e.StatusSnapshot()
		if revision != next.Revision || len(statuses) != 1 || statuses[0].Folder != "Archive" || statuses[0].UpdatedAt.IsZero() {
			t.Fatalf("subscription snapshot: %d %+v", revision, statuses)
		}
		if _, exists := e.status["INBOX"]; exists {
			t.Fatal("removed worker republished old health")
		}
	})
}

func TestScheduledHealthRemainsHealthyBetweenChecks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newScheduledSource()
		e, _ := scheduledEngine(t, source, []mail.Folder{{Name: "Archive", Check: "5m"}})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() { defer close(done); e.Run(ctx) }()
		<-source.polls
		synctest.Wait()
		initial := e.Status()[0]
		time.Sleep(time.Minute)
		waiting := e.Status()[0]
		if waiting.State != "watching" || waiting.LastError != nil || waiting.LastCheck == nil || initial.UpdatedAt != waiting.UpdatedAt || waiting.NextCheck == nil {
			t.Fatalf("scheduled wait reported unhealthy: %+v", waiting)
		}
		cancel()
		<-done
	})
}
