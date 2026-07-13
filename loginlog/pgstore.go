package loginlog

import (
	"context"
	"database/sql"
)

// PGStore is the Postgres reference implementation of Store (stdlib
// database/sql, no ORM). A host with its own login-audit table implements
// Store directly and ignores this.
type PGStore struct{ db *sql.DB }

func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

var _ Store = (*PGStore)(nil)

// Migrate creates the reference login_logs table + a lookup index (idempotent).
func (s *PGStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS login_logs (
	    id         BIGSERIAL PRIMARY KEY,
	    user_id    BIGINT      NOT NULL DEFAULT 0,
	    username   TEXT        NOT NULL DEFAULT '',
	    ip_hash    TEXT        NOT NULL DEFAULT '',
	    success    BOOLEAN     NOT NULL DEFAULT false,
	    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS login_logs_user_time ON login_logs (user_id, created_at DESC)`)
	return err
}

const logCols = `id, user_id, username, ip_hash, success, created_at`

func (s *PGStore) Record(ctx context.Context, e Entry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO login_logs (user_id, username, ip_hash, success) VALUES ($1,$2,$3,$4)`,
		e.UserID, e.Username, e.IPHash, e.Success)
	return err
}

func (s *PGStore) Recent(ctx context.Context, userID int64, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+logCols+` FROM login_logs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows)
}

func (s *PGStore) RecentAll(ctx context.Context, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+logCols+` FROM login_logs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return scanEntries(rows)
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Username, &e.IPHash, &e.Success, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
