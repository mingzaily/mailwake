package httpapi

import (
	"context"

	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/settings"
)

// Runtime combines the capabilities used to assemble all HTTP routes.
// Individual route groups depend on their own capability set.
type Runtime interface {
	MailboxRuntime
	DeliveryRuntime
	DiagnosticsRuntime
	ReadContent(context.Context, coreidentity.MessageReference) (mail.Body, error)
}
type MailboxRuntime interface {
	Mailboxes() []settings.MailboxView
	Mailbox(string) (settings.MailboxView, error)
	CreateMailbox(context.Context, settings.MailboxUpdate) (settings.MailboxView, error)
	UpdateMailbox(context.Context, string, settings.MailboxUpdate) error
	DeleteMailbox(context.Context, string) error
	TestMailbox(context.Context, string, settings.MailboxUpdate) (*mail.FolderDiscovery, error)
	TestSavedMailbox(context.Context, string) error
	Folders(context.Context, string) (*mail.FolderDiscovery, error)
	Subscriptions(string) (mail.Subscriptions, error)
	UpdateSubscriptions(context.Context, string, mail.Subscriptions) (mail.Subscriptions, error)
}
type DeliveryRuntime interface {
	DeliveryView() settings.DeliveryView
	UpdateDelivery(context.Context, settings.DeliveryUpdate) error
	DeleteDelivery(context.Context, int64) error
	TestDelivery(context.Context, settings.DeliveryUpdate) error
	TestSavedDelivery(context.Context) error
}
type DiagnosticsRuntime interface {
	Status() []engine.FolderStatus
	Notices() map[string]*fault.Error
	Channel() string
	DeliveryView() settings.DeliveryView
	Mailboxes() []settings.MailboxView
}
