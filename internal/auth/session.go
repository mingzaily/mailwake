package auth

import (
	"context"
	"github.com/mingzaily/mailwake/internal/fault"
)

func (s *Service) Login(ctx context.Context, source, username, password string) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLimit(source); err != nil {
		return Grant{}, err
	}
	a, err := s.store.Administrator(ctx)
	if err != nil {
		return Grant{}, err
	}
	if a == nil {
		return Grant{}, fault.New("setup_required")
	}
	valid := VerifyPassword(a.PasswordHash, password)
	if !valid || a.Username != username {
		s.failed(source)
		return Grant{}, fault.New("unauthorized")
	}
	grant, session := newGrant()
	if err := s.store.CreateSession(ctx, a.PasswordHash, session); err != nil {
		return Grant{}, err
	}
	s.log.Info("Administrator login succeeded", "mailbox_id", "")
	return grant, nil
}
func (s *Service) Logout(ctx context.Context, hash string) error {
	return s.store.DeleteSession(ctx, hash)
}
func (s *Service) ChangePassword(ctx context.Context, source, sessionHash, current, next string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLimit(source); err != nil {
		return err
	}
	a, err := s.store.Administrator(ctx)
	if err != nil {
		return err
	}
	if a == nil {
		return fault.New("setup_required")
	}
	if !VerifyPassword(a.PasswordHash, current) {
		s.failed(source)
		return fault.New("current_password_invalid")
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.store.ChangePassword(ctx, a.PasswordHash, hash, sessionHash); err != nil {
		return err
	}
	s.log.Info("Administrator password changed", "mailbox_id", "")
	return nil
}
