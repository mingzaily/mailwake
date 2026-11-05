package mail

import (
	"context"
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

type HistorySource interface {
	ReadHistory(context.Context, string, HistoryScan) (HistoryBatch, error)
}
