// Package logging records safe operational events in JSON and a bounded memory ring.
package logging

import (
	"context"
	"io"
	"log/slog"
	"maps"
	"sync"
	"time"
)

const Capacity = 2000

type Entry struct {
	Seq     uint64         `json:"seq"`
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs"`
}
type Query struct {
	After     uint64
	Level     slog.Level
	MailboxID string
	Limit     int
}
type Page struct {
	Entries []Entry `json:"entries"`
	Next    uint64  `json:"next"`
}

type ring struct {
	mu      sync.Mutex
	entries [Capacity]Entry
	seq     uint64
}
type handler struct {
	output  slog.Handler
	ring    *ring
	attrs   []slog.Attr
	grouped bool
}

// New keeps the ring write independent of stderr I/O. Both outputs share the
// same attribute whitelist; callers supply fixed messages and safe field values.
func New(output io.Writer) *slog.Logger {
	return slog.New(&handler{output: slog.NewJSONHandler(output, nil), ring: &ring{}})
}
func (h *handler) Enabled(_ context.Context, level slog.Level) bool { return level >= slog.LevelInfo }
func allowed(key string) bool {
	switch key {
	case "pairing_id", "device_id", "mailbox_id", "folder", "mode", "check", "code", "event_id", "channel", "attempt", "duration_ms", "http_status", "next_attempt", "backoff_seconds", "count", "revision", "token_id", "version", "listen":
		return true
	}
	return false
}
func safe(a slog.Attr) bool {
	if !allowed(a.Key) {
		return false
	}
	switch a.Value.Kind() {
	case slog.KindString, slog.KindBool, slog.KindInt64, slog.KindUint64, slog.KindFloat64, slog.KindDuration, slog.KindTime:
		return true
	}
	return false
}
func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	attrs := map[string]any{"mailbox_id": ""}
	add := func(a slog.Attr) {
		if safe(a) {
			attrs[a.Key] = a.Value.Any()
		}
	}
	for _, a := range h.attrs {
		add(a)
	}
	if !h.grouped {
		record.Attrs(func(a slog.Attr) bool { add(a); return true })
	}
	level := "info"
	if record.Level >= slog.LevelError {
		level = "error"
	} else if record.Level >= slog.LevelWarn {
		level = "warn"
	}
	h.ring.mu.Lock()
	h.ring.seq++
	seq := h.ring.seq
	h.ring.entries[(seq-1)%Capacity] = Entry{seq, record.Time.UTC(), level, record.Message, attrs}
	h.ring.mu.Unlock()
	clean := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	for k, v := range attrs {
		clean.AddAttrs(slog.Any(k, v))
	}
	return h.output.Handle(ctx, clean)
}
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	copy := *h
	copy.attrs = append([]slog.Attr{}, h.attrs...)
	if !h.grouped {
		for _, a := range attrs {
			if safe(a) {
				copy.attrs = append(copy.attrs, a)
			}
		}
	}
	return &copy
}
func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	copy := *h
	copy.grouped = true // Grouped fields are outside the flat API whitelist.
	return &copy
}
func (h *handler) read(q Query) Page {
	h.ring.mu.Lock()
	tail := h.ring.seq
	start := uint64(1)
	if tail > Capacity {
		start = tail - Capacity + 1
	}
	snapshot := make([]Entry, 0, min(tail, Capacity))
	for seq := start; seq <= tail; seq++ {
		snapshot = append(snapshot, h.ring.entries[(seq-1)%Capacity])
	}
	h.ring.mu.Unlock()
	page := Page{Entries: []Entry{}, Next: tail}
	for _, entry := range snapshot {
		level := slog.LevelInfo
		if entry.Level == "warn" {
			level = slog.LevelWarn
		} else if entry.Level == "error" {
			level = slog.LevelError
		}
		if entry.Seq <= q.After || level < q.Level || (q.MailboxID != "" && entry.Attrs["mailbox_id"] != q.MailboxID) {
			continue
		}
		entry.Attrs = maps.Clone(entry.Attrs)
		page.Entries = append(page.Entries, entry)
		if len(page.Entries) >= q.Limit {
			page.Next = entry.Seq
			break
		}
	}
	return page
}
func Read(log *slog.Logger, q Query) Page {
	if q.Limit <= 0 {
		q.Limit = 200
	}
	q.Limit = min(q.Limit, 500)
	if h, ok := log.Handler().(*handler); ok {
		return h.read(q)
	}
	return Page{Entries: []Entry{}}
}
