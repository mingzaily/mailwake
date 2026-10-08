package auth

import (
	"context"
	"crypto/rand"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

type IssuedToken struct {
	storage.Token
	Value string `json:"token"`
}

func (s *Service) CreateToken(ctx context.Context, name string) (IssuedToken, error) {
	if strings.TrimSpace(name) == "" || len(name) > 128 || !utf8.ValidString(name) {
		return IssuedToken{}, fault.New("token_name_invalid")
	}
	value := "mwk_" + rand.Text()
	t := storage.Token{ID: rand.Text(), Name: name, CreatedAt: time.Now().Unix()}
	if err := s.store.CreateToken(ctx, t, digest(value)); err != nil {
		return IssuedToken{}, err
	}
	s.log.Info("API token created", "mailbox_id", "", "token_id", t.ID)
	return IssuedToken{Token: t, Value: value}, nil
}
func (s *Service) Tokens(ctx context.Context) ([]storage.Token, error) { return s.store.Tokens(ctx) }
func (s *Service) AuthenticateToken(ctx context.Context, value string) error {
	if !strings.HasPrefix(value, "mwk_") {
		return fault.New("unauthorized")
	}
	return s.store.AuthenticateToken(ctx, digest(value))
}
func (s *Service) RevokeToken(ctx context.Context, id string) error {
	if err := s.store.RevokeToken(ctx, id); err != nil {
		return err
	}
	s.log.Info("API token revoked", "mailbox_id", "")
	return nil
}
