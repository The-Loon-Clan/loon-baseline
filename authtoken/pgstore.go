package authtoken

import (
	"context"
	"database/sql"
	"time"
)

// PGStore is the Postgres reference implementation of Store (stdlib
// database/sql). A host with its own token table implements Store directly.
type PGStore struct{ db *sql.DB }

func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

var _ Store = (*PGStore)(nil)

// Migrate creates the auth_tokens table + a lookup index (idempotent).
func (s *PGStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS auth_tokens (
	    id         BIGSERIAL PRIMARY KEY,
	    user_id    BIGINT      NOT NULL,
	    purpose    TEXT        NOT NULL,
	    token_hash TEXT        NOT NULL,
	    expires_at TIMESTAMPTZ NOT NULL,
	    used_at    TIMESTAMPTZ,
	    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS auth_tokens_hash ON auth_tokens (token_hash, purpose)`)
	return err
}

func (s *PGStore) Create(ctx context.Context, userID int64, purpose Purpose, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at) VALUES ($1,$2,$3,$4)`,
		userID, string(purpose), tokenHash, expiresAt)
	return err
}

// Consume marks a matching unexpired unused token used and returns its user id,
// atomically (the UPDATE ... RETURNING can't race two redemptions).
func (s *PGStore) Consume(ctx context.Context, tokenHash string, purpose Purpose) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx,
		`UPDATE auth_tokens SET used_at = now()
		   WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > now()
		 RETURNING user_id`, tokenHash, string(purpose)).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, ErrInvalidToken
	}
	return userID, err
}
