package runtime

import (
	"context"
	"time"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
)

// ReadContent accepts an owner-authorized request and owns only its bounded read connection.
func (m *Manager) ReadContent(ctx context.Context, reference coreidentity.MessageReference) (mail.Body, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	source, err := m.messageSource(ctx, reference)
	if err != nil {
		return mail.Body{}, err
	}
	body, err := source.ReadContent(ctx, reference.Folder, reference.Location)
	return body, messageError(ctx, err)
}

// MarkRead adds the mailbox read flag after the App opens the body, including cached bodies.
func (m *Manager) MarkRead(ctx context.Context, reference coreidentity.MessageReference) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	source, err := m.messageSource(ctx, reference)
	if err != nil {
		return err
	}
	return messageError(ctx, source.MarkRead(ctx, reference.Folder, reference.Location))
}

func (m *Manager) messageSource(ctx context.Context, reference coreidentity.MessageReference) (mail.Source, error) {
	if err := m.identity.VerifyMessageReference(ctx, reference); err != nil {
		return nil, err
	}
	b, err := m.mailbox(reference.MailboxID)
	if err != nil {
		return nil, fault.New("message_not_found")
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
		return nil, fault.New("message_not_found")
	}
	if source == nil {
		return nil, fault.New("message_unavailable")
	}
	return source, nil
}

func messageError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil || fault.From(err, "message_unavailable").Code == "imap_command_timeout" {
		return fault.New("message_timeout")
	}
	switch fault.From(err, "message_unavailable").Code {
	case "message_not_found", "message_too_large", "message_content_invalid":
		return err
	default:
		return fault.New("message_unavailable")
	}
}
