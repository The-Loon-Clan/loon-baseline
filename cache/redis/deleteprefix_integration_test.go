package redis

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Exercises the real SCAN+DEL against a live Redis (skipped unless REDIS_ADDR is
// set). Covers the batching path with > one SCAN batch of keys.
func TestDeletePrefixAgainstRedis(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping Redis integration test")
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
