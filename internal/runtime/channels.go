package runtime

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"time"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/delivery/bark"
	"github.com/mingzaily/mailwake/internal/delivery/pushover"
	"github.com/mingzaily/mailwake/internal/delivery/webhook"
	"github.com/mingzaily/mailwake/internal/event"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/settings"
)

func sender(d settings.Delivery) delivery.Sender {
	return senderWithOptions(d, nil)
}

func senderWithOptions(d settings.Delivery, roots *x509.CertPool) delivery.Sender {
	switch d.Channel {
	case "pushover":
		s := pushover.New(pushover.Endpoint, d.Pushover.Token, d.Pushover.User, d.Language)
		s.SetHTTPClient(delivery.NewHTTPClientWithLocalTestRoots(pushover.Endpoint, roots))
		return s
	case "webhook":
		s := webhook.New(d.Webhook.URL, d.Webhook.Secret, d.Language)
		s.SetHTTPClient(delivery.NewHTTPClientWithLocalTestRoots(d.Webhook.URL, roots))
		return s
	default:
		s := bark.New(d.Bark.Endpoint, d.Bark.Key, d.Language)
		s.SetHTTPClient(delivery.NewHTTPClientWithLocalTestRoots(d.Bark.Endpoint, roots))
		return s
	}
}

func (m *Manager) testDelivery(ctx context.Context, config settings.Delivery) error {
	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if config.Channel == "native" {
		return m.native.Test(testCtx, config.NativePairingID)
	}
	if err := m.deliverySender(config).Send(testCtx, event.Notification{ID: "test_" + rand.Text(), Test: true, ReceivedAt: time.Now().UTC()}); err != nil {
		var failure *delivery.Failure
		if errors.As(err, &failure) {
			m.log.Warn("Delivery test failed", "channel", config.Channel, "code", failure.Code, "http_status", failure.HTTPStatus)
			return &fault.Error{Code: failure.Code, Params: failure.Params}
		}
		m.log.Warn("Delivery test failed", "channel", config.Channel, "code", "delivery_test_failed")
		return fault.New("delivery_test_failed")
	}
	return nil
}
