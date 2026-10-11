package httpapi

import (
	"context"
	"github.com/mingzaily/mailwake/internal/engine"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/mail"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/settings"
)

type Monitor interface {
	Status() []engine.FolderStatus
	Subscriptions() mail.Subscriptions
	UpdateSubscriptions(context.Context, mail.Subscriptions) (mail.Subscriptions, error)
}
type FolderSource interface {
	Folders(context.Context) (*mail.FolderDiscovery, error)
}

// Focused transport tests provide their own monitor and discovery behavior.
type testRuntime struct {
	monitor Monitor
	source  FolderSource
	channel string
}

func newTestRuntime(channel string, monitor Monitor, source FolderSource) Runtime {
	return testRuntime{monitor, source, channel}
}
func (r testRuntime) Status() []engine.FolderStatus {
	if r.monitor == nil {
		return nil
	}
	return r.monitor.Status()
}
func (r testRuntime) Subscriptions(string) (mail.Subscriptions, error) {
	return r.monitor.Subscriptions(), nil
}
func (r testRuntime) UpdateSubscriptions(ctx context.Context, _ string, state mail.Subscriptions) (mail.Subscriptions, error) {
	return r.monitor.UpdateSubscriptions(ctx, state)
}
func (r testRuntime) Folders(ctx context.Context, _ string) (*mail.FolderDiscovery, error) {
	return r.source.Folders(ctx)
}
func (r testRuntime) Channel() string                { return r.channel }
func (testRuntime) Notices() map[string]*fault.Error { return nil }
func (r testRuntime) Mailboxes() []map[string]any {
	result := []map[string]any{}
	seen := map[string]bool{}
	for _, s := range r.Status() {
		if !seen[s.MailboxID] {
			result = append(result, map[string]any{"id": s.MailboxID})
			seen[s.MailboxID] = true
		}
	}
	return result
}
func (testRuntime) Mailbox(id string) (map[string]any, error) { return map[string]any{"id": id}, nil }
func (testRuntime) CreateMailbox(context.Context, settings.MailboxUpdate) (map[string]any, error) {
	return nil, fault.New("mailbox_required")
}
func (testRuntime) DeleteMailbox(context.Context, string) error {
	return fault.New("mailbox_not_found")
}
func (r testRuntime) DeliveryView() map[string]any {
	return map[string]any{"channel": r.channel, "revision": int64(0)}
}
func (testRuntime) UpdateMailbox(context.Context, string, settings.MailboxUpdate) error {
	return fault.New("mailbox_required")
}
func (testRuntime) TestMailbox(context.Context, string, settings.MailboxUpdate) (*mail.FolderDiscovery, error) {
	return nil, fault.New("mailbox_required")
}
func (testRuntime) TestSavedMailbox(context.Context, string) error {
	return fault.New("mailbox_required")
}
func (testRuntime) UpdateDelivery(context.Context, settings.DeliveryUpdate) error {
	return fault.New("delivery_test_failed")
}
func (testRuntime) DeleteDelivery(context.Context, int64) error {
	return fault.New("delivery_test_failed")
}
func (testRuntime) TestDelivery(context.Context, settings.DeliveryUpdate) error {
	return fault.New("delivery_test_failed")
}
func (testRuntime) TestSavedDelivery(context.Context) error {
	return fault.New("delivery_test_failed")
}

func (r testRuntime) ReadContent(context.Context, native.MessageReference) (mail.Body, error) {
	return mail.Body{Text: "test body"}, nil
}
