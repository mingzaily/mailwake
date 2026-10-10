package runtime

import (
	"context"
	"log/slog"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

// Model aliases keep the relocated behavioral fixtures unchanged.
type Mailbox = settings.Mailbox
type MailboxUpdate = settings.MailboxUpdate
type Delivery = settings.Delivery
type DeliveryUpdate = settings.DeliveryUpdate

var OpenVault = settings.OpenVault

// singleMailboxService keeps the focused pre-C12 regression scenarios explicit:
// they start with one recoverable mailbox and always edit that mailbox's latest revision.
type singleMailboxService struct{ *Manager }

func newService(ctx context.Context, store *storage.Store, vault *settings.Vault, log *slog.Logger, sourceFactory func(settings.Mailbox) mail.Source, senderFactory func(settings.Delivery) delivery.Sender) (*singleMailboxService, error) {
	// Start with one recoverable (not yet configured) mailbox, mbx_primary.
	if record, err := store.ConfigurationRecord(ctx, storage.MailboxConfigurationName("mbx_primary")); err != nil {
		return nil, err
	} else if record.Revision == 0 {
		encrypted, err := vault.Seal(storage.MailboxConfigurationName("mbx_primary"), []byte(`{"id":"mbx_primary"}`))
		if err != nil {
			return nil, err
		}
		if err := store.SaveMailboxRecord(ctx, "mbx_primary", 0, encrypted, false); err != nil {
			return nil, err
		}
	}
	manager, err := newManager(ctx, store, vault, log, "", sourceFactory, senderFactory)
	if err != nil {
		return nil, err
	}
	return &singleMailboxService{manager}, nil
}
func (s *singleMailboxService) MailboxView() map[string]any {
	view, _ := s.Mailbox("mbx_primary")
	return view
}
func (s *singleMailboxService) UpdateMailbox(ctx context.Context, input settings.MailboxUpdate) error {
	input.Revision = s.MailboxView()["revision"].(int64)
	return s.Manager.UpdateMailbox(ctx, "mbx_primary", input)
}
func (s *singleMailboxService) TestMailbox(ctx context.Context, input settings.MailboxUpdate) (*mail.FolderDiscovery, error) {
	return s.Manager.TestMailbox(ctx, "mbx_primary", input)
}
func (s *singleMailboxService) Folders(ctx context.Context) (*mail.FolderDiscovery, error) {
	return s.Manager.Folders(ctx, "mbx_primary")
}
func (s *singleMailboxService) UpdateSubscriptions(ctx context.Context, input mail.Subscriptions) (mail.Subscriptions, error) {
	return s.Manager.UpdateSubscriptions(ctx, "mbx_primary", input)
}
func (s *singleMailboxService) UpdateDelivery(ctx context.Context, input settings.DeliveryUpdate) error {
	revision := s.DeliveryView()["revision"].(int64)
	input.Revision = &revision
	return s.Manager.UpdateDelivery(ctx, input)
}

func (s *singleMailboxService) ReadContent(context.Context, string, mail.Location) (mail.Body, error) {
	return mail.Body{}, nil
}
