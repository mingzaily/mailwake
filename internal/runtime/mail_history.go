package runtime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/mailview"
)

func (m *Manager) MailMessages(ctx context.Context, reader mailview.Reader, input mailview.MessageRequest) (mailview.Messages, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	identity, err := m.identity.Load(ctx)
	if err != nil {
		return mailview.Messages{}, err
	}
	now := time.Now().UTC()
	cursor := mailCursor{
		Version: 1, Kind: "messages", Reader: reader, Expires: now.Add(24 * time.Hour),
		MailboxID: input.MailboxID, AccountID: input.AccountID, Folder: input.Folder,
		Scan: mail.HistoryScan{Start: now.Add(-30 * 24 * time.Hour), End: now},
	}
	if input.Cursor != "" {
		cursor, err = readMailCursor(identity, input.Cursor, "messages", reader, now)
		if err != nil {
			return mailview.Messages{}, err
		}
		if cursor.MailboxID != input.MailboxID || cursor.AccountID != input.AccountID || cursor.Folder != input.Folder {
			return mailview.Messages{}, fault.New("sync_cursor_invalid")
		}
	}
	source, revision, err := m.historyResource(input)
	if err != nil {
		return mailview.Messages{}, err
	}
	if input.Cursor != "" && cursor.Revision != revision {
		return mailview.Messages{}, fault.New("sync_cursor_invalid")
	}
	cursor.Revision = revision
	cursor.Scan.Limit = input.Limit
	batch, err := source.ReadHistory(ctx, input.Folder, cursor.Scan)
	if err != nil {
		switch fault.From(err, "message_unavailable").Code {
		case "sync_cursor_invalid", "sync_page_too_large":
			return mailview.Messages{}, err
		default:
			return mailview.Messages{}, messageError(ctx, err)
		}
	}
	page := mailview.Messages{
		Page:      mailview.Page[mailview.Message]{CoreID: identity.ID, Items: make([]mailview.Message, 0, len(batch.Messages))},
		MailboxID: input.MailboxID, AccountID: input.AccountID, Folder: input.Folder,
		SubscriptionRevision: revision, WindowStart: cursor.Scan.Start, WindowEnd: cursor.Scan.End,
		UIDValidity: batch.Validity, SnapshotMaxUID: batch.MaxUID,
	}
	for _, item := range batch.Messages {
		reference, err := identity.MessageReference(event.Notification{Location: &item.Location, MailboxID: input.MailboxID, AccountID: input.AccountID, Folder: input.Folder})
		if err != nil {
			return mailview.Messages{}, err
		}
		page.Items = append(page.Items, mailview.Message{
			Reference: reference, Sender: item.Sender, SenderName: item.SenderName,
			Subject: item.Subject, ReceivedAt: item.ReceivedAt, IsRead: item.IsRead,
		})
	}
	cursor.Scan.Validity, cursor.Scan.MaxUID, cursor.Scan.NextUID = batch.Validity, batch.MaxUID, batch.NextUID
	if err := fitHistoryPage(&page, cursor, identity); err != nil {
		return mailview.Messages{}, err
	}
	_, currentRevision, err := m.historyResource(input)
	if err != nil {
		return mailview.Messages{}, err
	}
	if currentRevision != revision {
		return mailview.Messages{}, fault.New("sync_cursor_invalid")
	}
	if ctx.Err() != nil {
		return mailview.Messages{}, messageError(ctx, ctx.Err())
	}
	return page, nil
}

func (m *Manager) historyResource(input mailview.MessageRequest) (mail.HistorySource, int64, error) {
	box, err := m.mailbox(input.MailboxID)
	if err != nil {
		return nil, 0, fault.New("message_not_found")
	}
	box.mu.RLock()
	defer box.mu.RUnlock()
	if box.deleted {
		return nil, 0, fault.New("message_not_found")
	}
	if box.config.Identity() != input.AccountID {
		return nil, 0, fault.New("sync_cursor_invalid")
	}
	subs := box.monitor.Subscriptions()
	for _, folder := range subs.Folders {
		if folder.Name == input.Folder {
			source, ok := box.source.(mail.HistorySource)
			if !ok {
				return nil, 0, fault.New("message_unavailable")
			}
			return source, subs.Revision, nil
		}
	}
	return nil, 0, fault.New("message_not_found")
}

// Long folder paths and JSON escaping also consume response space. Shorten the
// page at a retained UID so the next request can read every omitted message.
func fitHistoryPage(page *mailview.Messages, cursor mailCursor, identity *coreidentity.Identity) error {
	for {
		page.Complete = cursor.Scan.NextUID == 0
		page.NextCursor = ""
		if !page.Complete {
			var err error
			page.NextCursor, err = cursor.sign(identity)
			if err != nil {
				return err
			}
		}
		raw, err := json.Marshal(page)
		if err != nil {
			return err
		}
		if len(raw) <= mailview.MaxResponseBytes {
			return nil
		}
		if len(page.Items) <= 1 {
			return fault.New("sync_page_too_large")
		}
		keep := max(1, len(page.Items)*mailview.MaxResponseBytes/len(raw)-1)
		cursor.Scan.NextUID = page.Items[keep].Reference.UID
		page.Items = page.Items[:keep]
	}
}
