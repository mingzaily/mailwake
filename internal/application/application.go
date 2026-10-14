// Package application assembles Core services for the process entry point.
package application

import (
	"context"
	"crypto/x509"
	"log/slog"

	"github.com/mingzaily/mailwake/internal/appmanagement"
	"github.com/mingzaily/mailwake/internal/coreidentity"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/runtime"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

type Application struct {
	*runtime.Manager
	Native     *native.Service
	Management *appmanagement.Service
}
type Options struct{ IMAPRootCAs, RelayRootCAs, DeliveryRootCAs *x509.CertPool }

func New(ctx context.Context, store *storage.Store, vault *settings.Vault, log *slog.Logger, relayURL string, options Options) (*Application, error) {
	identity := coreidentity.New(store, vault)
	push, err := native.NewWithOptions(store, vault, identity, relayURL, log, native.ClientOptions{RootCAs: options.RelayRootCAs})
	if err != nil {
		return nil, err
	}
	management := &appmanagement.Service{Store: store, Identity: identity, Native: push}
	manager, err := runtime.New(ctx, store, vault, log, runtime.Dependencies{Identity: identity, Native: push}, runtime.Options{IMAPRootCAs: options.IMAPRootCAs, DeliveryRootCAs: options.DeliveryRootCAs})
	if err != nil {
		return nil, err
	}
	return &Application{Manager: manager, Native: push, Management: management}, nil
}
