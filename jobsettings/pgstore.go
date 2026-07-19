package jobsettings

import (
	"context"
	"database/sql"

	"github.com/the-loon-clan/loon/schedule"
)

// PGStore is the Postgres reference implementation of Store (stdlib
// database/sql, no ORM). A host with its own settings table implements Store
// directly and ignores this.
type PGStore struct{ db *sql.DB }

func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

var (
	_ Store                   = (*PGStore)(nil)
	_ schedule.JobConfigStore = (*PGStore)(nil)
)

// Migrate creates the reference job_settings table (idempotent). One row per
// (job_name, key) override; the declared default applies when no row exists.
func (s *PGStore) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS job_settings (
	    job_name   TEXT        NOT NULL,
	    key        TEXT        NOT NULL,
	    value      TEXT        NOT NULL DEFAULT '',
	    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	    PRIMARY KEY (job_name, key)
	)`)
	return err
}

func (s *PGStore) GetJobSettings(ctx context.Context, jobName string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, value FROM job_settings WHERE job_name = $1`, jobName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *PGStore) SetJobSetting(ctx context.Context, jobName, key, value string) error {
	// Empty value means "revert to default" — delete the override rather than
	// storing a blank, so GetConfig* falls through to the declared default.
	if value == "" {
		return s.DeleteJobSetting(ctx, jobName, key)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO job_settings (job_name, key, value, updated_at) VALUES ($1,$2,$3, now())
		 ON CONFLICT (job_name, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		jobName, key, value)
	return err
}

func (s *PGStore) DeleteJobSetting(ctx context.Context, jobName, key string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM job_settings WHERE job_name = $1 AND key = $2`, jobName, key)
	return err
}
