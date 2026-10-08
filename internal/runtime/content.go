package runtime

import (
	"context"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/native"
)

// ReadContent accepts an owner-authorized request and owns only its bounded read connection.
func (m *Manager) ReadContent(ctx context.Context, reference native.MessageReference) (mail.Body, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := m.native.VerifyMessageReference(ctx, reference); err != nil {
		return mail.Body{}, err
	}
	b, err := m.mailbox(reference.MailboxID)
	if err != nil {
		return mail.Body{}, fault.New("message_not_found")
	}
	b.mu.RLock()
	source, deleted := b.source, b.deleted
	accountID := b.config.Identity()
	subscribed := false
	for _, folder := range b.monitor.Subscriptions().Folders {
		if folder.Name == reference.Folder {
			subscribed = true
			break
		}
	}
	b.mu.RUnlock()
	if deleted || !subscribed || reference.AccountID != accountID {
		return mail.Body{}, fault.New("message_not_found")
	}
	if source == nil {
		return mail.Body{}, fault.New("message_unavailable")
	}
	body, err := source.ReadContent(ctx, reference.Folder, reference.Location)
	if err == nil {
		return body, nil
	}
	if ctx.Err() != nil || fault.From(err, "message_unavailable").Code == "imap_command_timeout" {
		return mail.Body{}, fault.New("message_timeout")
	}
	switch fault.From(err, "message_unavailable").Code {
	case "message_not_found", "message_too_large", "message_content_invalid":
		return mail.Body{}, err
	default:
		return mail.Body{}, fault.New("message_unavailable")
	}
}
