package main

import (
	"context"
	"fmt"
	"github.com/mingzaily/mailwake/internal/logging"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/buildinfo"
	"github.com/mingzaily/mailwake/internal/config"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/httpapi"
	"github.com/mingzaily/mailwake/internal/i18n"
	"github.com/mingzaily/mailwake/internal/runtime"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		var err error
		if len(os.Args) != 3 {
			err = fault.New("cli_usage")
		} else {
			dir := os.Getenv("MAILWAKE_DATA_DIR")
			if dir == "" {
				dir = "data"
			}
			err = runAdmin(context.Background(), dir, os.Args[2], adminInteraction{password: terminalPassword, confirm: terminalNativeReset, output: os.Stdout})
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.ErrorMessage("en", err))
			os.Exit(1)
		}
		return
	}

	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, i18n.Message("en", "cli_usage", nil))
		os.Exit(1)
	}
	log := logging.New(os.Stderr)
	if err := run(log); err != nil {
		logStartupFailure(log, err)
		os.Exit(1)
	}
}

// Startup diagnostics use catalog messages to keep raw paths and secrets out of logs.
func logStartupFailure(log *slog.Logger, err error) {
	coded := fault.From(err, "core_startup_failed")
	log.Error(i18n.Message("en", "log.core_failed", nil)+" "+i18n.Message("en", coded.Code, nil), "code", coded.Code)
}

func run(log *slog.Logger) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := storage.Open(ctx, c.DataDir)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.Message("en", "cli.open_storage", nil), err)
	}
	defer store.Close()
	hasConfiguration, err := store.HasConfiguration(ctx)
	if err != nil {
		return err
	}
	vault, err := settings.OpenVault(c.DataDir, hasConfiguration)
	if err != nil {
		return err
	}
	authService := auth.New(store, log)
	code, err := authService.Prepare(ctx)
	if err != nil {
		return err
	}
	if code != "" {
		writeSetupCode(os.Stderr, code)
	}
	configuration, err := runtime.NewWithOptions(ctx, store, vault, log, c.RelayURL, runtime.Options{IMAPRootCAs: c.LocalTestTLS.IMAP, RelayRootCAs: c.LocalTestTLS.Relay, DeliveryRootCAs: c.LocalTestTLS.Delivery})
	if err != nil {
		return err
	}
	defer configuration.Close()
	configuration.AppManagement().Trust = c.AppManagementTrust
	gin.SetMode(gin.ReleaseMode)
	server := &http.Server{Handler: httpapi.New(authService, configuration, store, log), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return fault.New("http_listen_failed")
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	version, _ := buildinfo.Current()
	log.Info(i18n.Message("en", "log.core_started", nil), "version", version, "listen", listener.Addr().String())
	select {
	case <-ctx.Done():
	case err = <-serveErr:
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		server.Close()
	}

	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Setup codes are console-only and never enter the management log buffer.
func writeSetupCode(output io.Writer, code string) {
	slog.New(slog.NewJSONHandler(output, nil)).Warn("Mailwake Core is not set up. Open the web page and enter the setup code: " + code)
}
