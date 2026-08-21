package redis

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Exercises the real SCAN+DEL against a live Redis. Covers the batching path
// with more than one SCAN batch of keys.
//
// REDIS_TEST_ADDR, NOT REDIS_ADDR, and the difference is not cosmetic. This
// test calls FlushDB — it wipes the entire database — and REDIS_ADDR is the
// OPERATOR'S switch, set in every compose file in this project. Gated on that,
// `go test ./...` on a developer machine with the app's environment loaded
// would flush whatever Redis the app was using: the running site's cache, its
// sessions, its rate-limit counters.
//
// The sibling test in session/ already read REDIS_TEST_ADDR for exactly this
// reason, and loon-demo-site's Makefile spells the reason out. This one was the
// odd case out.
func TestDeletePrefixAgainstRedis(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set; skipping Redis integration test")
	}
	ctx := context.Background()
	rdb := goredis.NewClient(&goredis.Options{Addr: addr})
	defer rdb.Close()
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	c := New(rdb)

	// 600 namespaced keys (> the 256 batch) + one unrelated key.
	for i := 0; i < 600; i++ {
		if err := c.Set(ctx, fmt.Sprintf("newznab:v1:%d", i), []byte("x"), time.Minute); err != nil {
			t.Fatalf("set: %v", err)
		}
	}
	_ = c.Set(ctx, "keep:zzz", []byte("y"), time.Minute)

	if err := c.DeletePrefix(ctx, "newznab:v1:"); err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}

	if n, _ := rdb.Keys(ctx, "newznab:v1:*").Result(); len(n) != 0 {
		t.Fatalf("namespace not fully cleared, %d keys remain", len(n))
	}
	if _, ok, _ := c.Get(ctx, "keep:zzz"); !ok {
		t.Fatal("unrelated key should survive a scoped flush")
	}
}
