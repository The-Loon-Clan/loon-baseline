package jobtrigger

import (
	"context"
	"database/sql"
)

// PGStore is the Postgres reference queue. Claimed requests are deleted (they're
// transient), and a UNIQUE index on job_name enforces at-most-one-pending per
// job so repeated clicks collapse to a single run.
type PGStore struct{ db *sql.DB }

func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

var _ Store = (*PGStore)(nil)

// Migrate creates the queue table + the dedup index (idempotent).
func (s *PGStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS job_triggers (
	    id           BIGSERIAL   PRIMARY KEY,
	    job_name     TEXT        NOT NULL,
	    requested_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`CREATE UNIQUE INDEX IF NOT EXISTS job_triggers_name ON job_triggers (job_name)`)
	return err
}

func (s *PGStore) Request(ctx context.Context, jobName string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO job_triggers (job_name) VALUES ($1) ON CONFLICT (job_name) DO NOTHING`,
		jobName)
	return err
}

func (s *PGStore) Claim(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 32
	}
	// Delete-and-return the oldest pending rows; SKIP LOCKED lets several workers
	// claim disjoint sets without blocking each other.
	rows, err := s.db.QueryContext(ctx,
		`DELETE FROM job_triggers
		 WHERE id IN (
		   SELECT id FROM job_triggers ORDER BY requested_at LIMIT $1 FOR UPDATE SKIP LOCKED
		 )
		 RETURNING job_name`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
