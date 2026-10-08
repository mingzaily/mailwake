package native

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/settings"
	"github.com/mingzaily/mailwake/internal/storage"
)

type ResetReport struct {
	Unlinked         int
	Remaining        int
	RelayUnavailable bool
}

// ResetPush is an offline administrator operation. The caller must own the
// exclusive data-directory lock and must have obtained user confirmation.
func ResetPush(ctx context.Context, store *storage.Store, vault *settings.Vault) (ResetReport, error) {
	var report ResetReport
	binding, err := loadRelayBinding(ctx, store, vault)
	if err != nil {
		return report, err
	}
	ids, err := store.NativeResetTargets(ctx)
	if err != nil {
		return report, err
	}
	report.Remaining = len(ids)
	if len(ids) > 0 {
		if binding == nil {
			return report, fault.New("configuration_corrupt")
		}
		client, err := NewClient(binding.URL)
		if err != nil {
			return report, fault.New("configuration_corrupt")
		}
		remoteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		report, err = unlinkOldRelay(remoteCtx, store, vault, client, binding.Info, ids)
		cancel()
		if err != nil {
			return report, err
		}
	}
	if report.Remaining > 0 {
		return report, nil
	}
	if err = store.ResetNativePush(ctx); err != nil {
		return report, &fault.Error{Code: "native_reset_failed", Params: map[string]string{"unlinked": strconv.Itoa(report.Unlinked)}}
	}
	return report, nil
}

// unlinkOldRelay deletes each pairing at the bound Relay. 404 and 410 mean it is already gone.
func unlinkOldRelay(ctx context.Context, store *storage.Store, vault *settings.Vault, client *Client, expected Info, ids []string) (ResetReport, error) {
	report := ResetReport{Remaining: len(ids)}
	service := &Service{store: store, vault: vault, identity: identityStore{store: store, vault: vault}, client: client, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	info, err := service.refreshInfo(checkCtx, false)
	cancel()
	if err != nil || !info.sameIdentity(expected) {
		report.RelayUnavailable = true
		return report, nil
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = service.deletePairing(callCtx, id)
		cancel()
		if err == nil || hasStatus(err, 404, 410) {
			report.Unlinked++
			report.Remaining--
		}
	}
	return report, nil
}
