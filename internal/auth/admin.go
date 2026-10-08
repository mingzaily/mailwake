package auth

import (
	"context"
	"github.com/mingzaily/mailwake/internal/fault"
)

// ResetPassword is used only by the offline CLI while holding the directory lock.
func (s *Service) ResetPassword(ctx context.Context, password string) error {
	a, err := s.store.Administrator(ctx)
	if err != nil {
		return err
	}
	if a == nil {
		return fault.New("setup_required")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.store.ChangePassword(ctx, a.PasswordHash, hash, "")
}
func (s *Service) ResetSetup(ctx context.Context) error { return s.store.ResetSetup(ctx) }
