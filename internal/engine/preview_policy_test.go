package engine

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/storage"
)

type policySession struct {
	testSession
	change func()
}

func (s policySession) Poll(ctx context.Context, checkpoint string, detect bool) (mail.Batch, error) {
	s.change()
	return (codeSession{}).Poll(ctx, checkpoint, detect)
}

func TestEnqueueUsesPolicyCommittedDuringMailboxRead(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disable"
		if enabled {
			name = "enable"
		}
		t.Run(name, func(t *testing.T) {
			store, err := storage.Open(t.Context(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			previous, next := "subject", "off"
			if enabled {
				previous, next = "off", "subject"
			}
			if err := store.SaveDelivery(t.Context(), []byte("synthetic"), previous, "webhook", false, 0); err != nil {
				t.Fatal(err)
			}
			e := New(nil, store, "mailbox", mail.Subscriptions{}, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
			e.SetDetectCodes(true)
			sess := policySession{change: func() {
				if err := store.SaveDelivery(t.Context(), []byte("synthetic"), next, "webhook", !enabled, 1); err != nil {
					t.Fatal(err)
				}
			}}
			if _, err := e.sync(t.Context(), "INBOX", sess); err != nil {
				t.Fatal(err)
			}
			items, err := store.Recent(t.Context())
			if err != nil || len(items) != 1 || items[0].Message == nil {
				t.Fatalf("history: %+v %v", items, err)
			}
			subject := items[0].Message.Subject
			if enabled {
				if subject == nil || *subject != "Sign in" {
					t.Fatal("enabled title lost")
				}
			} else if subject != nil {
				t.Fatal("disabled title retained")
			}
			task, err := store.Due(t.Context(), time.Now().Add(time.Second))
			if err != nil || task == nil {
				t.Fatal("missing task", err)
			}
			if enabled && task.Event.SenderName != "通知中心" {
				t.Fatal("sender name lost")
			}
			if !enabled && (task.Event.Subject != "" || task.Event.Sender != "" || task.Event.SenderName != "" || task.Event.Code != "") {
				t.Fatal("disabled metadata retained")
			}
		})
	}
}
