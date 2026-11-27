package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/mailview"
	"github.com/mingzaily/mailwake/internal/settings"
)

type historySource struct {
	multiSource
	read func(context.Context, mail.HistoryScan) (mail.HistoryBatch, error)
}

func (s *historySource) OpenHistory(context.Context, *atomic.Int64) (mail.HistoryConnection, error) {
	return s, nil
}

func (s *historySource) Close() error                               { return nil }
func (s *historySource) Probe(context.Context, *atomic.Int64) error { return nil }
func (s *historySource) Disconnected() bool                         { return false }
func (s *historySource) ReadHistory(ctx context.Context, _ string, scan mail.HistoryScan, _ *atomic.Int64) (mail.HistoryBatch, error) {
	return s.read(ctx, scan)
}

var historyReader = mailview.Reader{ControllerID: "controller", DeviceID: "device"}

func historyRequest(t *testing.T, m *Manager, id string) mailview.MessageRequest {
	t.Helper()
	page, err := m.MailFolders(t.Context(), historyReader, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range page.Items {
		if folder.MailboxID == id {
			return mailview.MessageRequest{MailboxID: id, AccountID: folder.AccountID, Folder: folder.Folder, Limit: 50}
		}
	}
	t.Fatal("subscribed mailbox absent")
	return mailview.MessageRequest{}
}

func TestMailHistoryPreservesWindowReferencesAndMonitoringProgress(t *testing.T) {
	var initial mail.HistoryScan
	source := &historySource{read: func(ctx context.Context, scan mail.HistoryScan) (mail.HistoryBatch, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > mail.HistoryDeadline {
			t.Fatal("unbounded history")
		}
		if scan.Validity == 0 {
			initial = scan
			return mail.HistoryBatch{Validity: 42, MaxUID: 9000, NextUID: 4904, Messages: []mail.HistoryMessage{}}, nil
		}
		if scan.Start != initial.Start || scan.End != initial.End || scan.Validity != 42 || scan.MaxUID != 9000 || scan.NextUID != 4904 {
			t.Fatalf("round changed: %+v", scan)
		}
		return mail.HistoryBatch{Validity: 42, MaxUID: 9000, Messages: []mail.HistoryMessage{{Location: mail.Location{UIDValidity: 42, UID: 7}, Sender: "alerts@example.test", Subject: "Synthetic message", ReceivedAt: scan.End.Add(-time.Hour), IsRead: true}}}, nil
	}}
	m, store, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
	id, _ := addMailbox(t, m, "imap.history.test", 3)
	subscribeMailbox(t, m, id)
	waitFor(t, func() bool { return m.Status()[0].State == "watching" })
	before, _ := store.Checkpoint(t.Context(), id, "INBOX")
	eventsBefore, _ := store.Recent(t.Context())
	input := historyRequest(t, m, id)
	first, err := m.MailMessages(t.Context(), historyReader, input)
	if err != nil || first.Complete || first.NextCursor == "" || len(first.Items) != 0 || first.WindowEnd.Sub(first.WindowStart) != 30*24*time.Hour {
		t.Fatalf("first: %+v %v", first, err)
	}
	input.Cursor = first.NextCursor
	second, err := m.MailMessages(t.Context(), historyReader, input)
	if err != nil || !second.Complete || second.NextCursor != "" || len(second.Items) != 1 {
		t.Fatalf("second: %+v %v", second, err)
	}
	ref := second.Items[0].Reference
	if err := m.identity.VerifyMessageReference(t.Context(), *ref); err != nil {
		t.Fatal("invalid content reference", err)
	}
	if ref.MailboxID != id || ref.AccountID != input.AccountID || ref.Folder != "INBOX" || ref.UID != 7 || ref.UIDValidity != 42 || !second.Items[0].IsRead {
		t.Fatalf("reference/metadata: %+v", second.Items[0])
	}
	after, _ := store.Checkpoint(t.Context(), id, "INBOX")
	eventsAfter, _ := store.Recent(t.Context())
	if before != after || len(eventsBefore) != len(eventsAfter) {
		t.Fatal("history changed monitoring progress or notifications")
	}
}

func TestMailHistoryRejectsObsoleteResourcesAndCursorOwners(t *testing.T) {
	for _, change := range []string{"tampered", "controller", "device", "folder", "account", "revision", "unsubscribed", "deleted", "expired", "kind"} {
		t.Run(change, func(t *testing.T) {
			source := &historySource{read: func(context.Context, mail.HistoryScan) (mail.HistoryBatch, error) {
				return mail.HistoryBatch{Validity: 42, MaxUID: 9000, NextUID: 4904}, nil
			}}
			m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
			id, _ := addMailbox(t, m, "imap.cursor.test", 3)
			subscribeMailbox(t, m, id)
			input := historyRequest(t, m, id)
			page, err := m.MailMessages(t.Context(), historyReader, input)
			if err != nil {
				t.Fatal(err)
			}
			input.Cursor = page.NextCursor
			reader := historyReader
			want := "sync_cursor_invalid"
			switch change {
			case "tampered":
				input.Cursor = "x" + input.Cursor
			case "controller":
				reader.ControllerID = "another"
			case "device":
				reader.DeviceID = "another"
			case "folder":
				input.Folder = "Archive"
			case "account":
				input.AccountID = "replacement"
			case "deleted":
				if err := m.DeleteMailbox(t.Context(), id); err != nil {
					t.Fatal(err)
				}
				want = "message_not_found"
			case "revision", "unsubscribed":
				subs, _ := m.Subscriptions(id)
				if change == "unsubscribed" {
					subs.Folders = nil
					want = "message_not_found"
				}
				if _, err := m.UpdateSubscriptions(t.Context(), id, subs); err != nil {
					t.Fatal(err)
				}
			case "expired", "kind":
				identity, _ := m.identity.Load(t.Context())
				cursor, err := readMailCursor(identity, input.Cursor, "messages", reader, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if change == "expired" {
					cursor.Expires = time.Now().Add(-time.Second)
				} else {
					cursor.Kind = "folders"
				}
				input.Cursor, err = cursor.sign(identity)
				if err != nil {
					t.Fatal(err)
				}
			}
			result, err := m.MailMessages(t.Context(), reader, input)
			if fault.From(err, "").Code != want || len(result.Items) != 0 || result.NextCursor != "" {
				t.Fatalf("obsolete request accepted: %+v %v", result, err)
			}
		})
	}
}

func TestMailHistoryDiscardsResultAfterConcurrentResourceChange(t *testing.T) {
	for _, change := range []string{"deleted", "unsubscribed", "account", "revision"} {
		t.Run(change, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			source := &historySource{read: func(context.Context, mail.HistoryScan) (mail.HistoryBatch, error) {
				close(entered)
				<-release
				return mail.HistoryBatch{Validity: 42, MaxUID: 7, Messages: []mail.HistoryMessage{{Location: mail.Location{UIDValidity: 42, UID: 7}, Subject: "old account"}}}, nil
			}}
			m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
			id, update := addMailbox(t, m, "imap.replace.test", 3)
			subscribeMailbox(t, m, id)
			input := historyRequest(t, m, id)
			done := make(chan error, 1)
			go func() { _, err := m.MailMessages(t.Context(), historyReader, input); done <- err }()
			<-entered
			want := "sync_cursor_invalid"
			switch change {
			case "deleted":
				if err := m.DeleteMailbox(t.Context(), id); err != nil {
					t.Fatal(err)
				}
				want = "message_not_found"
			case "account":
				update.Username = "replacement@example.test"
				if err := m.UpdateMailbox(t.Context(), id, update); err != nil {
					t.Fatal(err)
				}
			case "unsubscribed", "revision":
				subs, _ := m.Subscriptions(id)
				if change == "unsubscribed" {
					subs.Folders = nil
					want = "message_not_found"
				}
				if _, err := m.UpdateSubscriptions(t.Context(), id, subs); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			if err := <-done; fault.From(err, "").Code != want {
				t.Fatal(err)
			}
		})
	}
}

func TestMailHistorySizeBudgetResumesAtOmittedMessage(t *testing.T) {
	m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return &multiSource{} })
	identity, err := m.identity.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cursor := mailCursor{Version: 1, Kind: "messages", Reader: historyReader, Expires: now.Add(time.Hour), Scan: mail.HistoryScan{Validity: 42, MaxUID: 100}}
	page := mailview.Messages{Page: mailview.Page[mailview.Message]{CoreID: identity.ID, Items: []mailview.Message{}}}
	for uid := uint32(100); uid > 0; uid-- {
		page.Items = append(page.Items, mailview.Message{Subject: strings.Repeat("<", 1024)})
		ref := signedContentReference(t, m, "box", "account")
		ref.UID = uid
		page.Items[len(page.Items)-1].Reference = &ref
	}
	if err := fitHistoryPage(&page, cursor, identity); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(page)
	if len(raw) > mailview.MaxResponseBytes || len(page.Items) == 100 || len(page.Items) == 0 || page.Complete {
		t.Fatalf("page was not bounded: %d %d", len(raw), len(page.Items))
	}
	next, err := readMailCursor(identity, page.NextCursor, "messages", historyReader, time.Now())
	if err != nil || next.Scan.NextUID != 100-uint32(len(page.Items)) {
		t.Fatalf("omitted UID lost: %+v %v", next, err)
	}
}

func TestHistoryCredentialUpdateRetiresTheOldSource(t *testing.T) {
	source := func(subject string) *historySource {
		return &historySource{read: func(context.Context, mail.HistoryScan) (mail.HistoryBatch, error) {
			return mail.HistoryBatch{Validity: 42, MaxUID: 1, Messages: []mail.HistoryMessage{
				{Location: mail.Location{UIDValidity: 42, UID: 1}, Subject: subject},
			}}, nil
		}}
	}
	replacement := "replacement-synthetic-password"
	m, _, _ := managerFixture(t, func(config settings.Mailbox) mail.Source {
		if config.Password == replacement {
			return source("replacement connection")
		}
		return source("original connection")
	})
	id, update := addMailbox(t, m, "imap.replace.test", 3)
	subscribeMailbox(t, m, id)
	input := historyRequest(t, m, id)
	old, err := m.historyResource(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.MailMessages(t.Context(), historyReader, input); err != nil {
		t.Fatal(err)
	}
	update.Password = &replacement
	if err := m.UpdateMailbox(t.Context(), id, update); err != nil {
		t.Fatal(err)
	}
	if _, err := old.source.ReadHistory(t.Context(), "INBOX", mail.HistoryScan{}); fault.From(err, "").Code != "sync_cursor_invalid" {
		t.Fatalf("old source admitted a history page: %v", err)
	}
	page, err := m.MailMessages(t.Context(), historyReader, input)
	if err != nil || len(page.Items) != 1 || page.Items[0].Subject != "replacement connection" {
		t.Fatalf("replacement history: %+v %v", page, err)
	}
}

type monitoredHistorySource struct {
	*historySource
	authCode     string
	historyOpens atomic.Int32
}

func (s *monitoredHistorySource) Open(ctx context.Context, folder string) (mail.Session, error) {
	if s.authCode != "" {
		return nil, fault.New(s.authCode)
	}
	return s.historySource.Open(ctx, folder)
}

func (s *monitoredHistorySource) OpenHistory(ctx context.Context, remaining *atomic.Int64) (mail.HistoryConnection, error) {
	s.historyOpens.Add(1)
	return s.historySource.OpenHistory(ctx, remaining)
}

func TestMailHistoryUsesMonitorsCredentialFailureBeforeOpeningConnection(t *testing.T) {
	for _, code := range []string{mail.CodeAuthFailed, mail.OutlookReauthorize} {
		t.Run(code, func(t *testing.T) {
			read := func(context.Context, mail.HistoryScan) (mail.HistoryBatch, error) {
				return mail.HistoryBatch{Validity: 42}, nil
			}
			refusing := &monitoredHistorySource{historySource: &historySource{read: read}, authCode: code}
			replacement := &monitoredHistorySource{historySource: &historySource{read: read}}
			m, _, _ := managerFixture(t, func(config settings.Mailbox) mail.Source {
				if config.Password == "replacement-synthetic-password" {
					return replacement
				}
				return refusing
			})
			id, update := addMailbox(t, m, "imap.auth.test", 3)
			subscribeMailbox(t, m, id)
			waitFor(t, func() bool { return m.Status()[0].State == "auth_required" })
			input := historyRequest(t, m, id)
			_, err := m.MailMessages(t.Context(), historyReader, input)
			failure := fault.From(err, "")
			if failure.Code != "message_unavailable" || failure.Params["cause_code"] != code || refusing.historyOpens.Load() != 0 {
				t.Fatalf("monitor credential failure did not block history: %v", failure)
			}
			update.Password = ptr("replacement-synthetic-password")
			if err := m.UpdateMailbox(t.Context(), id, update); err != nil {
				t.Fatal(err)
			}
			if _, err := m.MailMessages(t.Context(), historyReader, input); err != nil || replacement.historyOpens.Load() != 1 {
				t.Fatalf("replacement source did not read history: %v", err)
			}
		})
	}
}

func TestMailboxChangesCancelActiveHistoryAndReleaseTheLease(t *testing.T) {
	for _, change := range []string{"replace", "delete"} {
		t.Run(change, func(t *testing.T) {
			entered, cancelled := make(chan struct{}), make(chan struct{})
			source := &historySource{read: func(ctx context.Context, _ mail.HistoryScan) (mail.HistoryBatch, error) {
				close(entered)
				<-ctx.Done()
				close(cancelled)
				return mail.HistoryBatch{}, ctx.Err()
			}}
			m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
			id, update := addMailbox(t, m, "imap.retire.test", 3)
			subscribeMailbox(t, m, id)
			box, err := m.mailbox(id)
			if err != nil {
				t.Fatal(err)
			}
			input := historyRequest(t, m, id)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := m.MailMessages(ctx, historyReader, input); done <- err }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("history did not start")
			}
			other := mailview.Reader{ControllerID: "other-controller", DeviceID: "other-device"}
			_, err = m.MailMessages(ctx, other, input)
			failure := fault.From(err, "")
			if failure.Code != "message_unavailable" || failure.Params["cause_code"] != mail.HistoryBusy || failure.Params["retry_after"] != "1" {
				t.Fatalf("concurrent controller did not receive busy response: %+v", failure)
			}
			want, leases := "sync_cursor_invalid", 1
			switch change {
			case "replace":
				update.Password = ptr("replacement-synthetic-password")
				if err := m.UpdateMailbox(t.Context(), id, update); err != nil {
					t.Fatal(err)
				}
				waitFor(t, func() bool { return m.Status()[0].State == "watching" })
			case "delete":
				if err := m.DeleteMailbox(t.Context(), id); err != nil {
					t.Fatal(err)
				}
				want, leases = "message_not_found", 0
			}
			select {
			case <-cancelled:
				if ctx.Err() != nil {
					t.Fatal("caller deadline cancelled the page before mailbox retirement")
				}
			case <-ctx.Done():
				t.Fatal("mailbox retirement did not cancel history")
			}
			if err := <-done; fault.From(err, "").Code != want || box.budget.InUse() != leases {
				t.Fatalf("history result %v, leases %d", err, box.budget.InUse())
			}
		})
	}
}

func TestManagerCloseJoinsHistoryLeaseFromRetiredSource(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "current source"
		if replace {
			name = "retired source"
		}
		t.Run(name, func(t *testing.T) {
			entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			releaseRead := sync.OnceFunc(func() { close(release) })
			defer releaseRead()
			source := &historySource{read: func(ctx context.Context, _ mail.HistoryScan) (mail.HistoryBatch, error) {
				close(entered)
				<-ctx.Done()
				close(cancelled)
				<-release
				return mail.HistoryBatch{}, ctx.Err()
			}}
			m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
			id, update := addMailbox(t, m, "imap.shutdown.test", 3)
			subscribeMailbox(t, m, id)
			input := historyRequest(t, m, id)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			readDone := make(chan error, 1)
			go func() { _, err := m.MailMessages(ctx, historyReader, input); readDone <- err }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("history did not start")
			}
			if replace {
				update.Password = ptr("replacement-synthetic-password")
				if err := m.UpdateMailbox(t.Context(), id, update); err != nil {
					t.Fatal(err)
				}
			}
			closed := make(chan struct{})
			go func() { m.Close(); close(closed) }()
			select {
			case <-cancelled:
				if ctx.Err() != nil {
					t.Fatal("caller deadline cancelled the page before shutdown")
				}
			case <-ctx.Done():
				t.Fatal("shutdown did not cancel history")
			}
			select {
			case <-closed:
				t.Fatal("shutdown returned while history still held the lease")
			case <-time.After(50 * time.Millisecond):
			}
			// Release the test reader and join both entry points.
			releaseRead()
			select {
			case <-closed:
			case <-ctx.Done():
				t.Fatal("shutdown did not finish after releasing history")
			}
			if err := <-readDone; err == nil {
				t.Fatal("shutdown accepted a cancelled page")
			}
		})
	}
}
