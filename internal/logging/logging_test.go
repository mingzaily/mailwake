package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestRingCapacityConcurrencyAndRestart(t *testing.T) {
	log := New(io.Discard)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 300 {
				log.Info("event")
			}
		})
	}
	wg.Wait()
	log.Debug("debug")
	all := []Entry{}
	after := uint64(0)
	for {
		p := Read(log, Query{After: after, Limit: 500})
		all = append(all, p.Entries...)
		after = p.Next
		if len(p.Entries) == 0 {
			break
		}
	}
	if len(all) != Capacity || all[0].Seq != 401 || after != 2400 {
		t.Fatalf("ring capacity/order: len=%d first=%d tail=%d", len(all), all[0].Seq, after)
	}
	for i, e := range all {
		if e.Seq != uint64(401+i) {
			t.Fatal("sequence gap")
		}
	}
	p := Read(New(io.Discard), Query{After: after})
	if p.Next != 0 || len(p.Entries) != 0 {
		t.Fatal("restart retained entries")
	}
}
func TestLogFiltersAndSafeAttributes(t *testing.T) {
	var output bytes.Buffer
	log := New(&output)
	secret := "synthetic-sensitive-value"
	log.Debug("debug")
	log.With("mailbox_id", "mbx_a", "password", secret).Info("first", "folder", "INBOX", "token", secret, "subject", secret, "sender", secret, "body", secret, "endpoint", secret, "url", secret, "session", secret, "setup_code", secret, "key", secret)
	log.Warn("second", "mailbox_id", "mbx_b", "code", "imap_auth_failed")
	log.Error("third", "mailbox_id", "mbx_a", "code", "checkpoint_storage_failed")
	log.WithGroup("unknown").With("mailbox_id", secret).Info("fourth", "folder", secret)
	p := Read(log, Query{Level: slog.LevelWarn, MailboxID: "mbx_a", Limit: 200})
	if len(p.Entries) != 1 || p.Entries[0].Seq != 3 || p.Next != 4 {
		t.Fatalf("level/mailbox filters: %+v", p)
	}
	p = Read(log, Query{After: 1, Limit: 1})
	if len(p.Entries) != 1 || p.Entries[0].Seq != 2 || p.Next != 2 {
		t.Fatalf("after/pagination: %+v", p)
	}
	p = Read(log, Query{After: 4})
	if len(p.Entries) != 0 || p.Next != 4 {
		t.Fatal("tail cursor")
	}
	p = Read(log, Query{})
	p.Entries[0].Attrs["folder"] = "mutated"
	if Read(log, Query{}).Entries[0].Attrs["folder"] != "INBOX" {
		t.Fatal("reader mutated ring")
	}
	data, _ := json.Marshal(Read(log, Query{}))
	if strings.Contains(output.String(), secret) || strings.Contains(string(data), secret) {
		t.Fatal("sensitive attributes leaked")
	}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["mailbox_id"] == nil {
			t.Fatal("invalid JSON log")
		}
	}
}

type blockingOutput struct{ entered, release chan struct{} }

func (w blockingOutput) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(p), nil
}
func TestRingReadsWhileConsoleIsBlocked(t *testing.T) {
	writer := blockingOutput{make(chan struct{}), make(chan struct{})}
	log := New(writer)
	done := make(chan struct{})
	go func() { defer close(done); log.Info("stored before console") }()
	<-writer.entered
	page := Read(log, Query{})
	close(writer.release)
	<-done
	if len(page.Entries) != 1 {
		t.Fatal("ring waited for console output")
	}
}
