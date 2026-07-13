package apikey

import (
	"context"
	"database/sql"
	"time"
)

// PGStore is the Postgres reference implementation of Store (stdlib
// database/sql). One row per user; the key is UNIQUE so Resolve can index it.
type PGStore struct{ db *sql.DB }

func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

var _ Store = (*PGStore)(nil)

// Migrate creates the reference api_keys table (idempotent). The UNIQUE index on
// api_key backs the per-request Resolve lookup.
func (s *PGStore) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS api_keys (
	    user_id    BIGINT      PRIMARY KEY,
	    api_key    TEXT        NOT NULL UNIQUE,
	    rotated_at TIMESTAMPTZ,
	    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	return err
}

func (s *PGStore) Resolve(ctx context.Context, key string) (int64, bool, error) {
	if key == "" {
		return 0, false, nil
	}
	var uid int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM api_keys WHERE api_key = $1`, key).Scan(&uid)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return uid, true, nil
}

func (s *PGStore) Ensure(ctx context.Context, userID int64) (Key, error) {
	gen, err := Generate()
	if err != nil {
		return Key{}, err
	}
	// Insert a fresh key only if the user has none; either way return the row
	// that ends up there (race-safe: a concurrent Ensure that lost keeps the
	// winner's key).
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (user_id, api_key) VALUES ($1,$2) ON CONFLICT (user_id) DO NOTHING`,
		userID, gen); err != nil {
		return Key{}, err
	}
	return s.get(ctx, userID)
}

func (s *PGStore) Rotate(ctx context.Context, userID int64) (Key, error) {
	gen, err := Generate()
	if err != nil {
		return Key{}, err
	}
	var k Key
	k.UserID = userID
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO api_keys (user_id, api_key) VALUES ($1,$2)
		 ON CONFLICT (user_id) DO UPDATE SET api_key = EXCLUDED.api_key, rotated_at = now()
		 RETURNING api_key, rotated_at, created_at`,
		userID, gen).Scan(&k.Key, &nullTime{&k.RotatedAt}, &k.CreatedAt)
	return k, err
}

func (s *PGStore) get(ctx context.Context, userID int64) (Key, error) {
	k := Key{UserID: userID}
	err := s.db.QueryRowContext(ctx,
		`SELECT api_key, rotated_at, created_at FROM api_keys WHERE user_id = $1`, userID).
		Scan(&k.Key, &nullTime{&k.RotatedAt}, &k.CreatedAt)
	return k, err
}

// nullTime scans a nullable TIMESTAMPTZ into a time.Time, leaving it zero on NULL.
type nullTime struct{ t *time.Time }

func (n *nullTime) Scan(v any) error {
	if v == nil {
		*n.t = time.Time{}
		return nil
	}
	if tv, ok := v.(time.Time); ok {
		*n.t = tv
	}
	return nil
}
