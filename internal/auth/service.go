// Package auth owns administrator credentials, setup and session lifecycles.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

type Service struct {
	store    *storage.Store
	log      *slog.Logger
	mu       sync.Mutex
	failures map[string]attempts
}
type Grant struct {
	ID   string `json:"-"`
	CSRF string `json:"csrf_token"`
}

func New(store *storage.Store, log *slog.Logger) *Service {
	return &Service{store: store, log: log, failures: make(map[string]attempts)}
}
func digest(value string) string { h := sha256.Sum256([]byte(value)); return hex.EncodeToString(h[:]) }
func (s *Service) Ready(ctx context.Context) (bool, error) {
	a, err := s.store.Administrator(ctx)
	return a != nil, err
}

// Username returns the configured administrator's display identity.
func (s *Service) Username(ctx context.Context) (string, error) {
	a, err := s.store.Administrator(ctx)
	if err != nil {
		return "", err
	}
	if a == nil {
		return "", fault.New("setup_required")
	}
	return a.Username, nil
}
func (s *Service) Prepare(ctx context.Context) (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	var chars [12]byte
	for i := range chars { // Rejection sampling keeps every character equally likely.
		for {
			var b [1]byte
			if _, err := rand.Read(b[:]); err != nil {
				return "", err
			}
			if int(b[0]) < 256-256%len(alphabet) {
				chars[i] = alphabet[int(b[0])%len(alphabet)]
				break
			}
		}
	}
	code := string(chars[:4]) + "-" + string(chars[4:8]) + "-" + string(chars[8:])
	needed, err := s.store.PrepareSetup(ctx, digest(code))
	if err != nil || !needed {
		return "", err
	}
	return code, nil
}
func newGrant() (Grant, storage.Session) {
	g := Grant{ID: rand.Text(), CSRF: rand.Text()}
	now := time.Now().Unix()
	return g, storage.Session{Hash: digest(g.ID), CSRF: g.CSRF, CreatedAt: now, LastUsedAt: now}
}
func (s *Service) Setup(ctx context.Context, source, code, username, password string) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLimit(source); err != nil {
		return Grant{}, err
	}
	ready, err := s.Ready(ctx)
	if err != nil {
		return Grant{}, err
	}
	if ready {
		return Grant{}, fault.New("setup_complete")
	}
	if len(username) > 128 || strings.TrimSpace(username) == "" || !utf8.ValidString(username) {
		return Grant{}, fault.New("username_invalid")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Grant{}, err
	}
	grant, session := newGrant()
	err = s.store.CompleteSetup(ctx, digest(code), storage.Administrator{Username: username, PasswordHash: hash}, session)
	if err != nil {
		if fault.From(err, "database_unavailable").Code == "setup_code_invalid" {
			s.failed(source)
		}
		return Grant{}, err
	}
	return grant, nil
}
func (s *Service) AuthenticateSession(ctx context.Context, id string) (*storage.Session, error) {
	return s.store.Session(ctx, digest(id), time.Now())
}
