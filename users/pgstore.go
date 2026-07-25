package users

import (
	"context"
	"database/sql"

	"github.com/the-loon-clan/loon/core"
)

// PGStore is the Postgres reference implementation of Store — a plain `users`
// table for new hosts. Uses stdlib database/sql (no ORM dep). A host with its
// own users schema (e.g. prod) implements Store directly and ignores this.
type PGStore struct{ db *sql.DB }

func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

var _ Store = (*PGStore)(nil)

// Migrate creates the reference users table (idempotent). New hosts call this
// at boot; a host with its own users schema skips it.
func (s *PGStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (
	    id             BIGSERIAL PRIMARY KEY,
	    username       TEXT        NOT NULL UNIQUE,
	    email          TEXT        NOT NULL DEFAULT '',
	    password_hash  TEXT        NOT NULL DEFAULT '',
	    role           INT         NOT NULL DEFAULT 0,
	    email_verified BOOLEAN     NOT NULL DEFAULT false,
	    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	// Backfill the column for a users table created before email verification.
	if _, err := s.db.ExecContext(ctx,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT false`); err != nil {
		return err
	}
	// user_display is the plugin-facing identity DISPLAY contract (loon's
	// IDENTITY-FACETS design, phase A): five documented columns plugin SQL
	// JOINs instead of the users table, so each host maps its own schema
	// behind them. Here the INT role enum maps to the display names (the
	// core.Role constants); avatar is empty and reputation zero until the
	// corresponding facet packages land — at which point only this view
	// changes, no plugin.
	_, err := s.db.ExecContext(ctx, `CREATE OR REPLACE VIEW user_display AS
		SELECT id,
		       username,
		       CASE role
		           WHEN -2 THEN 'banned'
		           WHEN -1 THEN 'disabled'
		           WHEN  1 THEN 'contributor'
		           WHEN  2 THEN 'mod'
		           WHEN  3 THEN 'admin'
		           ELSE 'user'
		       END AS role,
		       ''::text AS avatar_path,
		       0::smallint AS reputation_tier
		FROM users`)
	return err
}

const userCols = `id, username, email, password_hash, role, email_verified, created_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var role int
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &role, &u.EmailVerified, &u.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.Role = core.Role(role)
	return &u, nil
}

func (s *PGStore) Create(ctx context.Context, u *User) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, role) VALUES ($1,$2,$3,$4) RETURNING id`,
		u.Username, u.Email, u.PasswordHash, int(u.Role)).Scan(&id)
	return id, err
}

func (s *PGStore) ByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

func (s *PGStore) ByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE lower(username) = lower($1)`, username))
}

func (s *PGStore) ByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email <> '' AND lower(email) = lower($1)`, email))
}

func (s *PGStore) IDByName(ctx context.Context, username string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE lower(username) = lower($1)`, username).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return id, err
}

func (s *PGStore) UpdatePasswordHash(ctx context.Context, id int64, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, id, hash)
	return err
}

func (s *PGStore) SetRole(ctx context.Context, id int64, role core.Role) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role = $2 WHERE id = $1`, id, int(role))
	return err
}

func (s *PGStore) SetEmailVerified(ctx context.Context, id int64, verified bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET email_verified = $2 WHERE id = $1`, id, verified)
	return err
}

func (s *PGStore) List(ctx context.Context, offset, limit int) ([]*User, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userCols+` FROM users ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}
