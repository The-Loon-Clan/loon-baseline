package notify

import (
	"context"
	"database/sql"

	"github.com/ameNZB/loon/core"
)

// PGStore is the Postgres InboxStore (stdlib database/sql). A host with its own
// notifications table implements InboxStore directly.
type PGStore struct{ db *sql.DB }

func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

var _ InboxStore = (*PGStore)(nil)

// Migrate creates the notifications table + a per-user lookup index (idempotent).
func (s *PGStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS notifications (
	    id         BIGSERIAL PRIMARY KEY,
	    user_id    BIGINT      NOT NULL,
	    kind       TEXT        NOT NULL DEFAULT '',
	    title      TEXT        NOT NULL,
	    body       TEXT        NOT NULL DEFAULT '',
	    link       TEXT        NOT NULL DEFAULT '',
	    actor_name TEXT        NOT NULL DEFAULT '',
	    read_at    TIMESTAMPTZ,
	    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS notifications_user_time ON notifications (user_id, created_at DESC)`)
	return err
}

func (s *PGStore) Add(ctx context.Context, userID int64, n core.Notification) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, kind, title, body, link, actor_name)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		userID, n.Kind, n.Title, n.Body, n.Link, n.ActorName)
	return err
}

func (s *PGStore) List(ctx context.Context, userID int64, limit int) ([]Item, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, title, body, link, actor_name, read_at, created_at
		   FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var readAt sql.NullTime
		if err := rows.Scan(&it.ID, &it.Kind, &it.Title, &it.Body, &it.Link, &it.ActorName, &readAt, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.Read = readAt.Valid
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *PGStore) UnreadCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (s *PGStore) MarkAllRead(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}
