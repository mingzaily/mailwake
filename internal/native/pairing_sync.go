package native

import (
	"context"
	"errors"
	"time"

	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

// PollActivePairings checks one established pairing per worker tick. The store
// admits at most 16 live pairings; each is checked at most once per minute.
func (s *Service) PollActivePairings(ctx context.Context) error {
	return s.pollActivePairings(ctx, time.Now)
}

func (s *Service) pollActivePairings(ctx context.Context, clock func() time.Time) error {
	now := clock()
	if !s.Available() || !s.activePolling.CompareAndSwap(false, true) {
		return nil
	}
	defer s.activePolling.Store(false)
	if now.Before(s.activeRetryAt) {
		return nil
	}
	pairs, err := s.store.NativePairings(ctx, "active")
	if err != nil {
		return err
	}
	checks := make(map[string]time.Time, len(pairs))
	var selected *storage.NativePairing
	for i := range pairs {
		p := &pairs[i]
		due := s.activeChecks[p.ID]
		checks[p.ID] = due
		if !now.Before(due) && (selected == nil || due.Before(checks[selected.ID])) {
			selected = p
		}
	}
	s.activeChecks = checks
	if selected == nil {
		return nil
	}
	err = s.checkActivePairing(ctx, *selected)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	completedAt := clock()
	s.activeChecks[selected.ID] = completedAt.Add(time.Minute)
	if err == nil {
		s.activeFailures = 0
		s.activeRetryAt = time.Time{}
		return nil
	}
	delay := min(30*time.Second<<min(s.activeFailures, 5), 15*time.Minute)
	s.activeFailures++
	var failure *delivery.Failure
	if errors.As(err, &failure) {
		delay = max(delay, min(failure.RetryAfter, 24*time.Hour))
	}
	s.activeRetryAt = completedAt.Add(delay)
	return err
}

func (s *Service) checkActivePairing(ctx context.Context, p storage.NativePairing) error {
	info, err := s.ensure(ctx)
	if err != nil {
		return err
	}
	identity, err := s.identity.Load(ctx)
	if err != nil {
		return err
	}
	var result relayPairing
	err = s.client.call(ctx, identity, info.Audience, "GET", "/v1/pairings/"+p.ID, nil, &result, 200)
	if err != nil {
		var failure *delivery.Failure
		if errors.As(err, &failure) && pairingGone(failure) {
			return s.revoke(ctx, p.ID)
		}
		return err
	}
	if result.ID != p.ID || result.DeviceID != p.DeviceID || result.EncryptionKey != p.EncryptionKey {
		return fault.New("native_protocol_invalid")
	}
	switch result.Status {
	case "active":
		return nil
	case "revoked":
		return s.revoke(ctx, p.ID)
	default:
		return fault.New("native_protocol_invalid")
	}
}
