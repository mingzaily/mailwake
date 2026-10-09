package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/i18n"
	"github.com/mingzaily/mailwake/internal/native"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
	"golang.org/x/term"
)

type adminInteraction struct {
	password func() (string, error)
	confirm  func() (bool, error)
	output   io.Writer
}

func runAdmin(ctx context.Context, dir, command string, interaction adminInteraction) error {
	if command != "reset-password" && command != "reset-setup" && command != "reset-native-push" {
		return fault.New("cli_usage")
	}
	store, err := storage.Open(ctx, dir)
	if err != nil {
		return err
	}
	defer store.Close()
	if command == "reset-native-push" {
		return resetNativePush(ctx, dir, store, interaction)
	}
	service := auth.New(store, slog.Default())
	if command == "reset-setup" {
		return service.ResetSetup(ctx)
	}
	password, err := interaction.password()
	if err != nil {
		return err
	}
	return service.ResetPassword(ctx, password)
}
func terminalPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fault.New("terminal_required")
	}
	fmt.Fprint(os.Stderr, "New administrator password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fault.New("terminal_required")
	}
	return string(password), nil
}

func resetNativePush(ctx context.Context, dir string, store *storage.Store, interaction adminInteraction) error {
	if interaction.confirm == nil {
		return fault.New("terminal_required")
	}
	confirmed, err := interaction.confirm()
	if err != nil {
		return err
	}
	if !confirmed {
		return fault.New("native_reset_cancelled")
	}
	configured, err := store.HasConfiguration(ctx)
	if err != nil {
		return err
	}
	vault, err := settings.OpenVault(dir, configured)
	if err != nil {
		return err
	}
	report, err := native.ResetPush(ctx, store, vault)
	if err != nil {
		return err
	}
	out := interaction.output
	if out == nil {
		out = io.Discard
	}
	if report.RelayUnavailable {
		fmt.Fprintln(out, i18n.Message("en", "cli.native_reset_offline", nil))
	}
	if report.Remaining > 0 {
		fmt.Fprintln(out, i18n.Message("en", "cli.native_reset_pending", map[string]string{"unlinked": strconv.Itoa(report.Unlinked), "remaining": strconv.Itoa(report.Remaining)}))
		return nil
	}
	fmt.Fprintln(out, i18n.Message("en", "cli.native_reset_done", map[string]string{"unlinked": strconv.Itoa(report.Unlinked), "remaining": strconv.Itoa(report.Remaining)}))
	return nil
}

func terminalNativeReset() (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fault.New("terminal_required")
	}
	fmt.Fprintln(os.Stderr, i18n.Message("en", "cli.native_reset_confirm", nil))
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, fault.New("native_reset_cancelled")
	}
	return strings.TrimSpace(answer) == "reset-native-push", nil
}
