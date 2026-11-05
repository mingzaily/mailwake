package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/mailview"
	"github.com/mingzaily/mailwake/internal/settings"
)

func TestMailCatalogPaginatesCurrentSubscriptionsAndBindsCursors(t *testing.T) {
	m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return &multiSource{} })
	for _, host := range []string{"imap.first.test", "imap.second.test", "imap.unsubscribed.test"} {
		id, _ := addMailbox(t, m, host, 3)
		if host != "imap.unsubscribed.test" {
			subscribeMailbox(t, m, id)
		}
	}
	page, err := m.MailFolders(t.Context(), historyReader, 1, "")
	if err != nil || page.Complete || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("first: %+v %v", page, err)
	}
	first := page.Items[0]
	if first.AccountID == "" || first.SubscriptionRevision != 2 || first.Folder != "INBOX" {
		t.Fatalf("identity: %+v", first)
	}
	last, err := m.MailFolders(t.Context(), historyReader, 1, page.NextCursor)
	if err != nil || !last.Complete || len(last.Items) != 1 || last.NextCursor != "" || first.MailboxID >= last.Items[0].MailboxID {
		t.Fatalf("last: %+v %v", last, err)
	}
	if _, err := m.MailHealth(t.Context(), historyReader, 1, page.NextCursor); fault.From(err, "").Code != "sync_cursor_invalid" {
		t.Fatal("cross-endpoint cursor", err)
	}
	if _, err := m.MailFolders(t.Context(), mailview.Reader{ControllerID: "other", DeviceID: historyReader.DeviceID}, 1, page.NextCursor); fault.From(err, "").Code != "sync_cursor_invalid" {
		t.Fatal("cross-controller cursor", err)
	}
	if err := m.DeleteMailbox(t.Context(), first.MailboxID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.MailFolders(t.Context(), historyReader, 1, page.NextCursor); fault.From(err, "").Code != "sync_cursor_invalid" {
		t.Fatal("removed resource cursor", err)
	}
	fresh, err := m.MailFolders(t.Context(), historyReader, 100, "")
	if err != nil || !fresh.Complete || len(fresh.Items) != 1 || fresh.Items[0].MailboxID == first.MailboxID {
		t.Fatalf("deleted resource survived: %+v %v", fresh, err)
	}
	data, _ := json.Marshal(fresh)
	for _, private := range []string{"username", "password", "outlook", "connection_limit", "user@example.test"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("catalog exposed configuration: %s", private)
		}
	}
}

func TestMailCatalogAndHealthBoundLongFolderPathsWithoutLosingResources(t *testing.T) {
	m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return &multiSource{} })
	for _, host := range []string{"imap.long-one.test", "imap.long-two.test"} {
		id, _ := addMailbox(t, m, host, 3)
		var folders []mail.Folder
		for i := range 8 {
			folders = append(folders, mail.Folder{Name: strings.Repeat("<", 4090) + string(rune('A'+i)), Check: "5m"})
		}
		if _, err := m.UpdateSubscriptions(t.Context(), id, mail.Subscriptions{Revision: 1, Folders: folders}); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"folders", "health"} {
		t.Run(kind, func(t *testing.T) {
			seen := map[string]bool{}
			token, pages := "", 0
			for {
				page, err := m.catalogPage(t.Context(), historyReader, 100, token, kind)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(page)
				if len(raw) > mailview.MaxResponseBytes {
					t.Fatalf("response is %d bytes", len(raw))
				}
				for _, item := range page.Items {
					id := item.MailboxID + "\x00" + item.Folder.Folder
					if seen[id] {
						t.Fatal("repeated catalog item")
					}
					seen[id] = true
				}
				pages++
				if page.Complete {
					break
				}
				if pages > 4 {
					t.Fatal("catalog made no progress")
				}
				token = page.NextCursor
			}
			if len(seen) != 16 || pages < 2 {
				t.Fatalf("catalog lost resources: %d items, %d pages", len(seen), pages)
			}
		})
	}
}

type healthSource struct {
	multiSource
	failure atomic.Pointer[fault.Error]
}

func (s *healthSource) Open(ctx context.Context, folder string) (mail.Session, error) {
	if folder == "INBOX" {
		if err := s.failure.Load(); err != nil {
			return nil, err
		}
	}
	return s.multiSource.Open(ctx, folder)
}

func TestMailHealthReportsPartialFailureAndRecoveryFromActualMonitor(t *testing.T) {
	source := &healthSource{}
	source.failure.Store(fault.New("imap_command_timeout"))
	m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
	id, _ := addMailbox(t, m, "imap.health.test", 3)
	if _, err := m.UpdateSubscriptions(t.Context(), id, mail.Subscriptions{Revision: 1, Folders: mail.RealtimeFolders([]string{"INBOX", "Archive"})}); err != nil {
		t.Fatal(err)
	}
	var before mailview.Page[mailview.Health]
	waitFor(t, func() bool {
		before, _ = m.MailHealth(t.Context(), historyReader, 100, "")
		return len(before.Items) == 2 && before.Items[0].State == "healthy" && before.Items[1].State == "retrying"
	})
	if before.Items[1].ReasonCode != "imap_command_timeout" || before.Items[1].LastCheckedAt != nil || before.Items[0].LastCheckedAt == nil || before.Items[1].UpdatedAt.IsZero() || before.ObservedAt == nil {
		t.Fatalf("health semantics: %+v", before)
	}
	unchanged, _ := m.MailHealth(t.Context(), historyReader, 100, "")
	if unchanged.Items[1].UpdatedAt != before.Items[1].UpdatedAt {
		t.Fatal("reading health changed its update time")
	}
	source.failure.Store(nil)
	var after mailview.Page[mailview.Health]
	waitFor(t, func() bool {
		after, _ = m.MailHealth(t.Context(), historyReader, 100, "")
		return after.Items[1].State == "healthy"
	})
	if after.Items[1].ReasonCode != "" || after.Items[1].LastCheckedAt == nil || !after.Items[1].UpdatedAt.After(before.Items[1].UpdatedAt) {
		t.Fatalf("recovery: %+v", after.Items[1])
	}
	subs, _ := m.Subscriptions(id)
	subs.Folders = mail.RealtimeFolders([]string{"Archive"})
	if _, err := m.UpdateSubscriptions(t.Context(), id, subs); err != nil {
		t.Fatal(err)
	}
	removed, _ := m.MailHealth(t.Context(), historyReader, 100, "")
	if len(removed.Items) != 1 || removed.Items[0].Folder.Folder != "Archive" {
		t.Fatalf("unsubscribed health: %+v", removed)
	}
}

func TestMailHealthAuthenticationAndPausedConfiguration(t *testing.T) {
	for _, code := range []string{"mail_auth_failed", "outlook_reauthorize"} {
		t.Run(code, func(t *testing.T) {
			source := &healthSource{}
			source.failure.Store(&fault.Error{Code: code, Params: map[string]string{"private": "secret-provider-detail"}})
			m, _, _ := managerFixture(t, func(settings.Mailbox) mail.Source { return source })
			id, _ := addMailbox(t, m, "imap.auth.test", 3)
			subscribeMailbox(t, m, id)
			var page mailview.Page[mailview.Health]
			waitFor(t, func() bool {
				page, _ = m.MailHealth(t.Context(), historyReader, 100, "")
				return page.Items[0].State == "action_required"
			})
			if page.Items[0].ReasonCode != code {
				t.Fatalf("reason: %+v", page.Items[0])
			}
			raw, _ := json.Marshal(page)
			if strings.Contains(string(raw), "secret-provider-detail") {
				t.Fatal("health exposed error parameters")
			}
			box, _ := m.mailbox(id)
			box.mu.Lock()
			box.stop()
			box.invalid = true
			box.source = nil
			m.startMailbox(box, box.monitor.Subscriptions())
			box.mu.Unlock()
			paused, _ := m.MailHealth(t.Context(), historyReader, 100, "")
			if paused.Items[0].State != "paused" || paused.Items[0].ReasonCode != "configuration_invalid" {
				t.Fatalf("paused: %+v", paused.Items[0])
			}
		})
	}
}
