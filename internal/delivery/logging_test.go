package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/storage"
	"strings"
	"testing"
	"time"
)

type logSender struct{ err error }

func (s *logSender) Send(context.Context, event.Notification) error { return s.err }
func (s *logSender) Channel() string                                { return "webhook" }
func TestDeliveryLifecycleLogs(t *testing.T) {
	store, err := storage.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var output bytes.Buffer
	log := logging.New(&output)
	sender := &logSender{err: &Failure{Code: "webhook_http_error", HTTPStatus: 503, Params: map[string]string{"status": "503", "private": "sensitive-provider-body"}, Retryable: true}}
	d := New(store, sender, 1, log)
	for _, id := range []string{"evt_retry", "evt_dead"} {
		if err := store.Enqueue(t.Context(), event.Notification{ID: id, MailboxID: "mbx_test", Folder: "INBOX", Sender: "private-sender@example.org", Subject: "private-mail-subject", Account: "private-label"}); err != nil {
			t.Fatal(err)
		}
	}
	// Deliver one task through retry and acceptance, then abandon the other.
	if _, err := d.deliverOne(t.Context(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.deliverOne(t.Context(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	sender.err = nil
	if _, err := d.deliverOne(t.Context(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sender.err = &Failure{Code: "webhook_http_error", HTTPStatus: 403}
	if _, err := d.deliverOne(t.Context(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	p := logging.Read(log, logging.Query{})
	data, _ := json.Marshal(p)
	for _, message := range []string{"Delivery failed; retry scheduled", "Delivery accepted", "Delivery abandoned"} {
		if !strings.Contains(string(data), message) {
			t.Fatalf("missing delivery event %s", message)
		}
	}
	for _, e := range p.Entries {
		if e.Attrs["mailbox_id"] != "mbx_test" || e.Attrs["channel"] != "webhook" || e.Attrs["event_id"] == nil || e.Attrs["attempt"] == nil || e.Attrs["duration_ms"] == nil {
			t.Fatalf("delivery attributes: %+v", e)
		}
	}
	if p.Entries[0].Attrs["next_attempt"] == nil || p.Entries[0].Attrs["http_status"] != int64(503) {
		t.Fatal("retry details missing")
	}
	for _, s := range []string{"private-sender@example.org", "private-mail-subject", "private-label", "sensitive-provider-body"} {
		if strings.Contains(output.String(), s) || strings.Contains(string(data), s) {
			t.Fatal("delivery secret logged")
		}
	}
}
