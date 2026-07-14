package jobtrigger

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// Exercises the real Postgres queue (DELETE ... FOR UPDATE SKIP LOCKED RETURNING
// + the dedup index) against a live database. Skipped unless LOON_TEST_DSN is set.
func TestPGStoreAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("LOON_TEST_DSN")
	if dsn == "" {
		t.Skip("LOON_TEST_DSN not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS job_triggers`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	s := NewPGStore(db)
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Dedup: two requests for the same job -> one pending row.
	_ = s.Request(ctx, "Crawl")
	_ = s.Request(ctx, "Crawl")
	_ = s.Request(ctx, "Backfill")

	names, err := s.Claim(ctx, 32)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("claim returned %v, want 2 rows (Crawl deduped)", names)
	}

	// Everything claimed -> next claim empty.
	if again, _ := s.Claim(ctx, 32); len(again) != 0 {
		t.Fatalf("second claim should be empty, got %v", again)
	}

	// A job can be re-requested once its pending row is gone.
	_ = s.Request(ctx, "Crawl")
	if n, _ := s.Claim(ctx, 32); len(n) != 1 || n[0] != "Crawl" {
		t.Fatalf("re-request after claim = %v, want [Crawl]", n)
	}
}
