package imap

import (
	"context"
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
	remaining int64
	exceeded  atomic.Bool
}

func (c *readLimitConn) Read(p []byte) (int, error) {
	if c.remaining == 0 {
		c.exceeded.Store(true)
		return 0, fault.New("sync_page_too_large")
	}
	n, err := c.Conn.Read(p[:min(int64(len(p)), c.remaining)])
	c.remaining -= int64(n)
	return n, err
}

func (s *Source) ReadHistory(ctx context.Context, folder string, scan mail.HistoryScan) (mail.HistoryBatch, error) {
	x, err := s.connectWithReadLimit(ctx, mail.HistoryReadBytes)
	if err != nil {
		return mail.HistoryBatch{}, err
	}
	defer x.Close()
	selected, err := x.selectFolder(ctx, folder, true)
	if err != nil {
		return mail.HistoryBatch{}, messageFolderError(err)
	}
	if scan.Validity != 0 && selected.validity != scan.Validity {
		return mail.HistoryBatch{}, fault.New("sync_cursor_invalid")
	}
	if scan.Validity == 0 {
		// A fixed snapshot requires UIDNEXT from EXAMINE.
		if selected.uidNext == 0 {
			return mail.HistoryBatch{}, fault.New("message_unavailable")
		}
		scan.MaxUID, scan.NextUID = selected.uidNext-1, selected.uidNext-1
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
	items, err := x.fetch(ctx, "imap_metadata_failed", set, &protocol.FetchOptions{UID: true, InternalDate: true, Envelope: true, Flags: true}, 0)
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
			item.SenderName = historyText(v.Envelope.From[0].Name, 512)
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
