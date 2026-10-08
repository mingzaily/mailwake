package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mingzaily/mailwake/internal/fault"
)

type Token struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
}

func (s *Store) CreateToken(ctx context.Context, token Token, hash string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO api_tokens(id,token_hash,name,created_at) VALUES(?,?,?,?)", token.ID, hash, token.Name, token.CreatedAt)
	return err
}
func (s *Store) Tokens(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,created_at,last_used_at FROM api_tokens ORDER BY created_at DESC,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tokens := make([]Token, 0)
	for rows.Next() {
		var token Token
		if err := rows.Scan(&token.ID, &token.Name, &token.CreatedAt, &token.LastUsedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}
func (s *Store) AuthenticateToken(ctx context.Context, hash string) error {
	var id string
	err := s.db.QueryRowContext(ctx, "UPDATE api_tokens SET last_used_at=? WHERE token_hash=? RETURNING id", time.Now().Unix(), hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return fault.New("unauthorized")
	}
	return err
}
func (s *Store) RevokeToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM api_tokens WHERE id=?", id)
	return err
}
