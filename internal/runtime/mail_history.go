package runtime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/logging"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/mailview"
)

func (m *Manager) MailMessages(ctx context.Context, reader mailview.Reader, input mailview.MessageRequest) (mailview.Messages, error) {
	ctx, cancel := context.WithTimeout(ctx, mail.HistoryDeadline)
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
	resource, err := m.historyResource(input)
	if err != nil {
		return mailview.Messages{}, err
	}
	if input.Cursor != "" && cursor.Revision != resource.revision {
		return mailview.Messages{}, fault.New("sync_cursor_invalid")
	}
	cursor.Revision = resource.revision
	cursor.Scan.Limit = input.Limit
	var batch mail.HistoryBatch
	if code := resource.monitor.AuthenticationFailure(); code != "" {
		err = fault.New(code)
	} else {
		batch, err = resource.source.ReadHistory(ctx, input.Folder, cursor.Scan)
	}
	if err != nil {
		if resourceErr := m.checkHistoryResource(input, resource); resourceErr != nil {
			return mailview.Messages{}, resourceErr
		}
		return mailview.Messages{}, historyError(ctx, err)
	}
	page := mailview.Messages{
		Page:      mailview.Page[mailview.Message]{CoreID: identity.ID, Items: make([]mailview.Message, 0, len(batch.Messages))},
		MailboxID: input.MailboxID, AccountID: input.AccountID, Folder: input.Folder,
		SubscriptionRevision: resource.revision, WindowStart: cursor.Scan.Start, WindowEnd: cursor.Scan.End,
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
	if err := m.checkHistoryResource(input, resource); err != nil {
		return mailview.Messages{}, err
	}
	if ctx.Err() != nil {
		return mailview.Messages{}, messageError(ctx, ctx.Err())
	}
	return page, nil
}

type historyResource struct {
	source   *mail.BudgetedSource
	monitor  *engine.Engine
	revision int64
}

func (m *Manager) checkHistoryResource(input mailview.MessageRequest, resource historyResource) error {
	current, err := m.historyResource(input)
	if err != nil {
		return err
	}
	if current.source != resource.source || current.revision != resource.revision {
		return fault.New("sync_cursor_invalid")
	}
	return nil
}

func historyError(ctx context.Context, err error) error {
	failure := fault.From(err, "message_unavailable")
	switch failure.Code {
	case "sync_cursor_invalid", "sync_page_too_large":
		return err
	case mail.HistoryUpstreamUnavailable, mail.HistoryBusy, mail.CodeAuthFailed, mail.OutlookReauthorize:
		logging.RequestCause(ctx, failure.Code)
		params := map[string]string{"cause_code": failure.Code}
		if seconds := failure.Params["retry_after"]; seconds != "" {
			params["retry_after"] = seconds
		}
		return &fault.Error{Code: "message_unavailable", Params: params}
	default:
		return messageError(ctx, err)
	}
}

func (m *Manager) historyResource(input mailview.MessageRequest) (historyResource, error) {
	box, err := m.mailbox(input.MailboxID)
	if err != nil {
		return historyResource{}, fault.New("message_not_found")
	}
	box.mu.RLock()
	defer box.mu.RUnlock()
	if box.deleted {
		return historyResource{}, fault.New("message_not_found")
	}
	if box.config.Identity() != input.AccountID {
		return historyResource{}, fault.New("sync_cursor_invalid")
	}
	subs := box.monitor.Subscriptions()
	for _, folder := range subs.Folders {
		if folder.Name == input.Folder {
			if box.source == nil {
				return historyResource{}, fault.New("message_unavailable")
			}
			return historyResource{source: box.source, monitor: box.monitor, revision: subs.Revision}, nil
		}
	}
	return historyResource{}, fault.New("message_not_found")
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
