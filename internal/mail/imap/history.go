package imap

import (
	"context"
	"crypto/tls"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// readLimitConn bounds all provider input, including envelope literals and
// unsolicited responses, as the IMAP decoder consumes their contents.
type readLimitConn struct {
	net.Conn
	remaining atomic.Pointer[atomic.Int64]
	exceeded  atomic.Pointer[atomic.Int64]
}

func (c *readLimitConn) Read(p []byte) (int, error) {
	budget := c.remaining.Load()
	left := budget.Load()
	if left <= 0 {
		c.exceeded.Store(budget)
		return 0, fault.New("sync_page_too_large")
	}
	n, err := c.Conn.Read(p[:min(int64(len(p)), left)])
	// A decoder read can remain blocked between pages. Charge the input to
	// the page that owns the connection when those bytes arrive.
	c.remaining.Load().Add(-int64(n))
	return n, err
}

// History releases its transport before another operation can take the lease.
func (c *readLimitConn) Close() error {
	if secure, ok := c.Conn.(*tls.Conn); ok {
		return secure.NetConn().Close()
	}
	return c.Conn.Close()
}

type historyConnection struct {
	conn      *connection
	transport *readLimitConn
}

func (s *Source) OpenHistory(ctx context.Context, remaining *atomic.Int64) (mail.HistoryConnection, error) {
	transport := &readLimitConn{}
	transport.remaining.Store(remaining)
	x, err := s.connectWithReadLimit(ctx, transport)
	if err != nil {
		return nil, err
	}
	// Each history command observes its page context. After setup, the
	// authenticated connection outlives that first request.
	x.stop()
	if err := ctx.Err(); err != nil {
		x.Close()
		return nil, err
	}
	return &historyConnection{conn: x, transport: transport}, nil
}

func (x *historyConnection) Close() error {
	return x.conn.Close()
}

func (x *historyConnection) Disconnected() bool {
	return x.conn.client.State() == protocol.ConnStateLogout
}

func (x *historyConnection) Probe(ctx context.Context, remaining *atomic.Int64) error {
	x.transport.remaining.Store(remaining)
	return x.conn.command(ctx, "imap_connection_closed", func(context.Context) error { return x.conn.client.Noop().Wait() })
}

func (x *historyConnection) ReadHistory(ctx context.Context, folder string, scan mail.HistoryScan, remaining *atomic.Int64) (mail.HistoryBatch, error) {
	x.transport.remaining.Store(remaining)
	selected, err := x.conn.selectFolder(ctx, folder, true)
	if err != nil {
		return mail.HistoryBatch{}, messageFolderError(err)
	}
	if scan.Validity != 0 && selected.validity != scan.Validity {
		return mail.HistoryBatch{}, fault.New("sync_cursor_invalid")
	}
	if scan.Validity == 0 {
		// A fixed snapshot starts below the next UID. Servers that omit UIDNEXT
		// from EXAMINE, such as NetEase, use the same SEARCH baseline as monitoring.
		next, err := selected.baseline(ctx)
		if err != nil {
			return mail.HistoryBatch{}, err
		}
		scan.MaxUID, scan.NextUID = next-1, next-1
	}
	result := mail.HistoryBatch{Validity: selected.validity, MaxUID: scan.MaxUID, Messages: []mail.HistoryMessage{}}
	if scan.NextUID == 0 {
		return result, nil
	}
	lower := uint32(1)
	if scan.NextUID >= mail.HistoryUIDSpan {
		lower = scan.NextUID - mail.HistoryUIDSpan + 1
	}
	uids, err := selected.historyUIDs(ctx, lower, scan.NextUID, scan.Start, scan.End)
	if err != nil {
		return mail.HistoryBatch{}, err
	}
	result.NextUID = lower - 1
	if len(uids) > scan.Limit {
		uids = uids[:scan.Limit]
		result.NextUID = uint32(uids[len(uids)-1]) - 1
	}
	if len(uids) == 0 {
		return result, nil
	}
	set := protocol.UIDSetNum(uids...)
	items, err := x.conn.fetch(ctx, "imap_metadata_failed", set, &protocol.FetchOptions{UID: true, InternalDate: true, Envelope: true, Flags: true}, 0)
	if err != nil {
		return mail.HistoryBatch{}, err
	}
	// EXPUNGE may remove a candidate between SEARCH and FETCH. Missing required
	// metadata on a still-existing candidate is a failed page, never a silent gap.
	var missing protocol.UIDSet
	for _, uid := range uids {
		v := items[uid]
		if v == nil || v.Envelope == nil || v.InternalDate.IsZero() || v.Flags == nil {
			missing.AddNum(uid)
		}
	}
	if len(missing) > 0 {
		remaining, err := selected.historySearch(ctx, &protocol.SearchCriteria{UID: []protocol.UIDSet{missing}}, lower, scan.NextUID)
		if err != nil {
			return mail.HistoryBatch{}, err
		}
		if len(remaining) > 0 {
			return mail.HistoryBatch{}, fault.New("imap_metadata_failed")
		}
	}
	for _, uid := range uids {
		if missing.Contains(uid) {
			continue
		}
		v := items[uid]
		if v.InternalDate.Before(scan.Start) || v.InternalDate.After(scan.End) {
			continue
		}
		item := mail.HistoryMessage{
			Location: mail.Location{UIDValidity: selected.validity, UID: uint32(uid)},
			Subject:  historyText(v.Envelope.Subject, 1024), ReceivedAt: v.InternalDate.UTC(),
			IsRead: slices.ContainsFunc(v.Flags, func(flag protocol.Flag) bool {
				return strings.EqualFold(string(flag), string(protocol.FlagSeen))
			}),
		}
		if len(v.Envelope.From) > 0 {
			item.Sender = historyText(v.Envelope.From[0].Addr(), 512)
			item.SenderName = historyText(mail.DisplayName(v.Envelope.From[0].Name), 512)
		}
		result.Messages = append(result.Messages, item)
	}
	return result, nil
}

func (s *session) historyUIDs(ctx context.Context, lower, upper uint32, start, end time.Time) ([]protocol.UID, error) {
	return s.historySearch(ctx, &protocol.SearchCriteria{
		UID:   []protocol.UIDSet{{{Start: protocol.UID(lower), Stop: protocol.UID(upper)}}},
		Since: start.UTC().Add(-24 * time.Hour), Before: end.UTC().Add(48 * time.Hour),
	}, lower, upper)
}

func (s *session) historySearch(ctx context.Context, criteria *protocol.SearchCriteria, lower, upper uint32) ([]protocol.UID, error) {
	var uids []protocol.UID
	err := s.command(ctx, "imap_status_failed", func(context.Context) error {
		data, err := s.client.UIDSearch(criteria, nil).Wait()
		if err != nil {
			return err
		}
		set, ok := data.All.(protocol.UIDSet)
		if !ok {
			return fault.New("imap_metadata_failed")
		}
		// UIDSearch normalizes and deduplicates ranges. Bound their endpoints
		// before expanding them into at most one requested UID span.
		for _, r := range set {
			if uint32(r.Start) < lower || uint32(r.Stop) > upper {
				return fault.New("imap_metadata_failed")
			}
		}
		uids, _ = set.Nums()
		return nil
	})
	slices.Reverse(uids)
	return uids, err
}

func historyText(value string, limit int) string {
	var out strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			continue
		}
		if out.Len()+utf8.RuneLen(r) > limit {
			break
		}
		out.WriteRune(r)
	}
	return out.String()
}
