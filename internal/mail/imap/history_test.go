package imap

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	protocol "github.com/emersion/go-imap/v2"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

func TestHistoryKeepsFixedWindowAndUIDSnapshotWithoutReadingBodies(t *testing.T) {
	source, user := fixtureWithFetch(t, func(options *protocol.FetchOptions) {
		if len(options.BodySection) > 0 || options.BodyStructure != nil {
			t.Error("history requested message content")
		}
	})
	end := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	start := end.Add(-30 * 24 * time.Hour)
	dates := []time.Time{end.Add(-time.Hour), start.Add(-60 * 24 * time.Hour), end, start, start.Add(-time.Second), end.Add(time.Second), start.Add(-30 * time.Hour)}
	var validity uint32
	for i, at := range dates {
		flags := []protocol.Flag{}
		if i == 2 {
			flags = []protocol.Flag{protocol.FlagSeen, protocol.FlagFlagged}
		}
		// The Date header deliberately disagrees with INTERNALDATE.
		raw := fmt.Sprintf("From: =?UTF-8?B?5rWL6K+V?= <sender@example.test>\r\nSubject: message %d\r\nDate: Thu, 01 Jan 1970 00:00:00 +0000\r\n\r\nprivate body", i+1)
		added, err := user.Append("Clients", strings.NewReader(raw), &protocol.AppendOptions{Time: at.In(time.FixedZone("offset", -11*60*60)), Flags: flags})
		if err != nil {
			t.Fatal(err)
		}
		validity = added.UIDValidity
	}
	scan := mail.HistoryScan{Start: start, End: end, Limit: 2}
	var got []uint32
	pages := 0
	for {
		batch, err := source.ReadHistory(t.Context(), "Clients", scan)
		if err != nil {
			t.Fatal(err)
		}
		if batch.MaxUID != 7 || batch.Validity != validity {
			t.Fatalf("snapshot changed: %+v", batch)
		}
		if pages == 0 {
			if len(batch.Messages) != 0 || batch.NextUID == 0 {
				t.Fatalf("expected an empty continuing page: %+v", batch)
			}
			if _, err := user.Append("Clients", strings.NewReader("Subject: next round\r\n\r\nbody"), &protocol.AppendOptions{Time: end}); err != nil {
				t.Fatal(err)
			}
		}
		for _, item := range batch.Messages {
			got = append(got, item.Location.UID)
			if item.SenderName != "测试" || item.Sender != "sender@example.test" || item.IsRead != (item.Location.UID == 3) {
				t.Fatalf("metadata: %+v", item)
			}
		}
		pages++
		if batch.NextUID == 0 {
			break
		}
		if pages > 5 {
			t.Fatal("pagination failed to finish")
		}
		scan.Validity, scan.MaxUID, scan.NextUID = batch.Validity, batch.MaxUID, batch.NextUID
	}
	if !slices.Equal(got, []uint32{4, 3, 1}) {
		t.Fatalf("window/order: %v", got)
	}
	x, err := source.connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	if _, err = x.selectFolder(t.Context(), "Clients", true); err != nil {
		t.Fatal(err)
	}
	items, err := x.fetch(t.Context(), "imap_metadata_failed", protocol.UIDSetNum(1, 3), &protocol.FetchOptions{UID: true, Flags: true}, 0)
	if err != nil || slices.Contains(items[1].Flags, protocol.FlagSeen) || !slices.Contains(items[3].Flags, protocol.FlagFlagged) {
		t.Fatalf("flags changed: %v %v", items, err)
	}
}

func TestHistoryBoundsSparseUIDRangesAndRejectsChangedValidity(t *testing.T) {
	source, _ := fixture(t)
	end := time.Now().UTC()
	scan := mail.HistoryScan{Start: end.Add(-30 * 24 * time.Hour), End: end, Limit: 50}
	first, err := source.ReadHistory(t.Context(), "Clients", scan)
	if err != nil || first.MaxUID != 0 || first.NextUID != 0 || len(first.Messages) != 0 {
		t.Fatalf("empty: %+v %v", first, err)
	}
	scan.Validity, scan.MaxUID, scan.NextUID = first.Validity, ^uint32(0)-1, ^uint32(0)-1
	page, err := source.ReadHistory(t.Context(), "Clients", scan)
	if err != nil || page.NextUID != scan.NextUID-mail.HistoryUIDSpan || len(page.Messages) != 0 {
		t.Fatalf("sparse page: %+v %v", page, err)
	}
	scan.Validity++
	if _, err := source.ReadHistory(t.Context(), "Clients", scan); fault.From(err, "").Code != "sync_cursor_invalid" {
		t.Fatal(err)
	}
	if _, err := source.ReadHistory(t.Context(), "missing", scan); fault.From(err, "").Code != "message_not_found" {
		t.Fatal(err)
	}
}

func TestHistoryProviderBudgetAndCancellation(t *testing.T) {
	t.Run("oversized envelope", func(t *testing.T) {
		source, user := fixture(t)
		now := time.Now().UTC().Truncate(time.Second)
		if _, err := user.Append("Clients", strings.NewReader("Subject: "+strings.Repeat("x", mail.HistoryReadBytes+1024)+"\r\n\r\nbody"), &protocol.AppendOptions{Time: now}); err != nil {
			t.Fatal(err)
		}
		_, err := source.ReadHistory(t.Context(), "Clients", mail.HistoryScan{Start: now.Add(-time.Hour), End: now, Limit: 50})
		if fault.From(err, "").Code != "sync_page_too_large" {
			t.Fatalf("budget error: %v", err)
		}
	})
	t.Run("cancelled fetch", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		source, user := fixtureWithFetch(t, func(*protocol.FetchOptions) { cancel() })
		appendMail(t, user, "metadata")
		page, err := source.ReadHistory(ctx, "Clients", mail.HistoryScan{Start: time.Now().Add(-time.Hour), End: time.Now(), Limit: 50})
		if err == nil || len(page.Messages) != 0 {
			t.Fatalf("cancelled page: %+v %v", page, err)
		}
	})
}

func TestHistoryMetadataIsReadableBoundedUTF8(t *testing.T) {
	got := historyText("hello\r\n\x00\u202e"+strings.Repeat("界", 400), 1024)
	if !utf8.ValidString(got) || len(got) > 1024 || strings.ContainsAny(got, "\r\n\x00\u202e") || !strings.HasPrefix(got, "hello界") {
		t.Fatal("invalid display metadata")
	}
}

func TestHistoryRejectsUnboundedOrMisdirectedSearchResponses(t *testing.T) {
	for _, reply := range []string{`* ESEARCH (TAG "{tag}") UID ALL 1:4294967295`, `* ESEARCH (TAG "{tag}") ALL 1:3`, `* SEARCH 0 1`, `* SEARCH 9001`} {
		t.Run(reply, func(t *testing.T) {
			source, _, _, _ := scriptedServer(t, serverOptions{caps: "IMAP4rev1", searchReply: reply, uidNext: 9001})
			now := time.Now()
			page, err := source.ReadHistory(t.Context(), "Clients", mail.HistoryScan{Start: now.Add(-30 * 24 * time.Hour), End: now, Limit: 50})
			if fault.From(err, "").Code != "imap_metadata_failed" || len(page.Messages) != 0 {
				t.Fatalf("invalid SEARCH accepted: %+v %v", page, err)
			}
		})
	}
}

func TestHistoryOrdersOverlappingAndUnorderedSearchResults(t *testing.T) {
	for _, reply := range []string{
		`* SEARCH 9000 8998 8999 8998 8997`,
		`* ESEARCH (TAG "{tag}") UID ALL 8997:9000,8998:8999,9000`,
	} {
		t.Run(reply, func(t *testing.T) {
			source, _, _, _ := scriptedServer(t, serverOptions{caps: "IMAP4rev1", searchReply: reply, uidNext: 9001})
			x, err := source.connect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer x.Close()
			selected, err := x.selectFolder(t.Context(), "Clients", true)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			uids, err := selected.historyUIDs(t.Context(), 8997, 9000, now.Add(-time.Hour), now)
			if err != nil || !slices.Equal(uids, []protocol.UID{9000, 8999, 8998, 8997}) {
				t.Fatalf("history UID order: %v %v", uids, err)
			}
		})
	}
}

func TestHistoryTreatsMissingFetchMetadataAsFailedPage(t *testing.T) {
	source, _, _, _ := scriptedServer(t, serverOptions{caps: "IMAP4rev1", uidNext: 2, readScenario: "missing_flags"})
	now := time.Now()
	page, err := source.ReadHistory(t.Context(), "Clients", mail.HistoryScan{Start: now.Add(-time.Hour), End: now, Limit: 50})
	if fault.From(err, "").Code != "imap_metadata_failed" || len(page.Messages) != 0 {
		t.Fatalf("incomplete FETCH accepted: %+v %v", page, err)
	}
}

func TestHistorySkipsCandidateExpungedDuringRead(t *testing.T) {
	source, _, _, _ := scriptedServer(t, serverOptions{caps: "IMAP4rev1", uidNext: 2, verifySearchReply: "* SEARCH"})
	now := time.Now()
	page, err := source.ReadHistory(t.Context(), "Clients", mail.HistoryScan{Start: now.Add(-time.Hour), End: now, Limit: 50})
	if err != nil || len(page.Messages) != 0 || page.NextUID != 0 {
		t.Fatalf("expunged candidate: %+v %v", page, err)
	}
}

func TestHistoryUsesExistingOutlookTokenProvider(t *testing.T) {
	source, _, _, _ := scriptedServer(t, serverOptions{caps: "IMAP4rev1 AUTH=XOAUTH2"})
	source.accessToken = func(context.Context) (string, error) { return "fixture-access", nil }
	now := time.Now()
	page, err := source.ReadHistory(t.Context(), "Clients", mail.HistoryScan{Start: now.Add(-time.Hour), End: now, Limit: 50})
	if err != nil || len(page.Messages) != 0 || page.NextUID != 0 {
		t.Fatalf("OAuth history: %+v %v", page, err)
	}
}
