package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

type testCheckpoints struct{}

func (testCheckpoints) SaveSubscriptions(context.Context, string, mail.Subscriptions) error {
	return nil
}

func (testCheckpoints) Checkpoint(context.Context, string, string) (string, error) { return "cp", nil }
func (testCheckpoints) Commit(context.Context, string, string, string, string, []event.Notification) error {
	return nil
}

type testSession struct {
	wait func() error
	poll func() error
}

func (s testSession) Poll(context.Context, string, bool) (mail.Batch, error) {
	var err error
	if s.poll != nil {
		err = s.poll()
	}
	return mail.Batch{Checkpoint: "cp"}, err
}
func (s testSession) Wait(context.Context, time.Duration) error { return s.wait() }
func (testSession) Close() error                                { return nil }
func (testSession) Mode() string                                { return "idle" }

type flappingSource struct{ opened chan time.Time }

func (flappingSource) Folders(context.Context) (*mail.FolderDiscovery, error) { return nil, nil }
func (s flappingSource) Open(context.Context, string) (mail.Session, error) {
	s.opened <- time.Now()
	return testSession{wait: func() error { return errors.New("IDLE failed") }}, nil
}

func TestFailedIdleKeepsExponentialBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		opened := make(chan time.Time, 1)
		e := New(flappingSource{opened}, testCheckpoints{}, "a", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Clients"})}, time.Minute, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
		done := make(chan struct{})
		go func() { defer close(done); e.Run(ctx) }()
		first, second, third := <-opened, <-opened, <-opened
		cancel()
		<-done
		if gap := second.Sub(first); gap < time.Second || gap > 1250*time.Millisecond {
			t.Fatal("first retry delay", gap)
		}
		if gap := third.Sub(second); gap < 2*time.Second || gap > 2500*time.Millisecond {
			t.Fatal("repeated failure reset retry delay", gap)
		}
	})
}

type rejectingSource struct{ opened chan time.Time }

func (rejectingSource) Folders(context.Context) (*mail.FolderDiscovery, error) { return nil, nil }
func (s rejectingSource) Open(context.Context, string) (mail.Session, error) {
	s.opened <- time.Now()
	return nil, fault.New(mail.CodeAuthFailed)
}

func TestRejectedCredentialsPauseLoginAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		opened := make(chan time.Time, 1)
		e := New(rejectingSource{opened}, testCheckpoints{}, "a", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Clients"})}, time.Minute, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
		done := make(chan struct{})
		go func() { defer close(done); e.Run(ctx) }()
		first := <-opened
		synctest.Wait()
		if status := e.Status(); status[0].State != "auth_required" || status[0].LastError.Code != mail.CodeAuthFailed {
			t.Fatalf("status %+v", status)
		}
		second, third := <-opened, <-opened
		cancel()
		<-done
		if gap := second.Sub(first); gap != authRetryDelay {
			t.Fatal("rejected login retried after", gap)
		}
		if gap := third.Sub(second); gap != authRetryDelay {
			t.Fatal("rejected login retry grew or shrank to", gap)
		}
	})
}

type steadySource struct{ opened chan time.Time }

func (steadySource) Folders(context.Context) (*mail.FolderDiscovery, error) { return nil, nil }
func (s steadySource) Open(context.Context, string) (mail.Session, error) {
	s.opened <- time.Now()
	return testSession{wait: func() error { time.Sleep(time.Minute); return nil }}, nil
}

func TestHealthySessionIsReopenedAfterLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		opened := make(chan time.Time, 1)
		e := New(steadySource{opened}, testCheckpoints{}, "a", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Clients"})}, time.Minute, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
		done := make(chan struct{})
		go func() { defer close(done); e.Run(ctx) }()
		first := <-opened
		second := <-opened
		synctest.Wait()
		state := e.Status()[0]
		cancel()
		<-done
		if gap := second.Sub(first); gap != sessionLifetime {
			t.Fatal("session reopened after", gap)
		}
		if state.State != "watching" || state.LastError != nil {
			t.Fatalf("scheduled reopen must not report a failure: %+v", state)
		}
	})
}

func TestBackoffResetsOnlyAfterStableWaitAndReconcile(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		duration             time.Duration
		waitError, pollError bool
		wantReset            bool
	}{
		{"short successful cycle", time.Second, false, false, false},
		{"stable successful cycle", time.Minute, false, false, true},
		{"long failed wait", time.Minute, true, false, false},
		{"failed reconcile", time.Minute, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := New(nil, testCheckpoints{}, "a", mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"Clients"})}, time.Minute, false, slog.Default())
				waits, polls, resets := 0, 0, 0
				s := testSession{wait: func() error {
					waits++
					if waits > 1 {
						return errors.New("stop")
					}
					time.Sleep(tc.duration)
					if tc.waitError {
						return errors.New("wait failed")
					}
					return nil
				}, poll: func() error {
					polls++
					if polls > 1 && tc.pollError {
						return errors.New("poll failed")
					}
					return nil
				}}
				if err := e.consume(t.Context(), "Clients", s, func() { resets++ }); err == nil {
					t.Fatal("expected session to end")
				}
				if (resets > 0) != tc.wantReset {
					t.Fatalf("resets=%d", resets)
				}
			})
		})
	}
}

type messageSession struct{ testSession }

func (messageSession) Poll(context.Context, string, bool) (mail.Batch, error) {
	return mail.Batch{Checkpoint: "new", Messages: []mail.Message{{Key: "1:42", ReceivedAt: time.Now()}}}, nil
}
func TestMailboxIdentitySeparatesEventIDs(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i, identity := range []string{"first-mailbox", "second-mailbox"} {
		if err := store.SaveMailboxRecord(t.Context(), "mbx_test", int64(i), []byte("encrypted"), true); err != nil {
			t.Fatal(err)
		}
		e := New(nil, store, "mbx_test", mail.Subscriptions{Revision: 1}, time.Minute, false, slog.Default())
		e.SetEventNamespace(identity)
		if _, err := e.sync(t.Context(), "INBOX", messageSession{}); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := store.Summary(t.Context())
	if err != nil || summary.Pending != 2 {
		t.Fatal("UID collision", summary, err)
	}
}

type codeSession struct{ testSession }

func (codeSession) Poll(_ context.Context, _ string, detectCodes bool) (mail.Batch, error) {
	msg := mail.Message{Key: "code-message", Sender: "sender@example.org", Subject: "Sign in", ReceivedAt: time.Now()}
	if detectCodes {
		msg.Code = "482913"
	}
	return mail.Batch{Checkpoint: "next", Messages: []mail.Message{msg}}, nil
}

func TestEnqueueVerificationCodeFollowsDetectionAndPreview(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		detect, preview       bool
		wantCode, wantSubject string
	}{
		{"native", true, true, "482913", "Sign in"},
		{"subject only", false, true, "", "Sign in"},
		{"preview off", true, false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := storage.Open(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			e := New(nil, store, "mailbox", mail.Subscriptions{}, time.Minute, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
			e.SetDetectCodes(tc.detect)
			e.SetPreview(tc.preview)
			if _, err := e.sync(t.Context(), "INBOX", codeSession{}); err != nil {
				t.Fatal(err)
			}
			task, err := store.Due(t.Context(), time.Now().Add(time.Second))
			if err != nil || task == nil {
				t.Fatalf("task=%v err=%v", task, err)
			}
			if task.Event.Code != tc.wantCode || task.Event.Subject != tc.wantSubject {
				t.Fatalf("unexpected event: %+v", task.Event)
			}
		})
	}
}

func (flappingSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}

func (rejectingSource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}

func (steadySource) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}
