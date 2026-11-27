package mail

import (
	"context"
	"sync/atomic"
	"time"
)

const HistoryUIDSpan = 4096
const HistoryReadBytes = 2 << 20

// HistoryScan describes a fixed read window and its next inclusive UID bound.
type HistoryScan struct {
	Start, End                time.Time
	Validity, MaxUID, NextUID uint32
	Limit                     int
}

type HistoryMessage struct {
	Location                    Location
	Sender, SenderName, Subject string
	ReceivedAt                  time.Time
	IsRead                      bool
}

type HistoryBatch struct {
	Validity, MaxUID, NextUID uint32
	Messages                  []HistoryMessage
}

// HistoryProvider opens a read-only connection. The atomic counter belongs to
// one page and follows it across connection setup and any reconnect.
type HistoryProvider interface {
	OpenHistory(context.Context, *atomic.Int64) (HistoryConnection, error)
}

// HistoryConnection serves one page at a time. Disconnected reports transport
// loss so the caller can restart an uncommitted page on a fresh connection.
type HistoryConnection interface {
	ReadHistory(context.Context, string, HistoryScan, *atomic.Int64) (HistoryBatch, error)
	Disconnected() bool
	Close() error
}
