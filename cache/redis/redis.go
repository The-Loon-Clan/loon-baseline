// Package redis is the Redis-backed cache.Cache impl (go-redis v9). A host binds
// it in place of cache/memory when a shared cache is needed across processes;
// no call site changes.
package redis

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ameNZB/loon-baseline/cache"
)

// Cache wraps a go-redis client as a cache.Cache.
type Cache struct{ rdb *goredis.Client }

// New builds a Redis-backed cache over an existing client.
func New(rdb *goredis.Client) *Cache { return &Cache{rdb: rdb} }

var (
	_ cache.Cache         = (*Cache)(nil)
	_ cache.PrefixDeleter = (*Cache)(nil)
)

func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	b, err := c.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (c *Cache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, val, ttl).Err()
}

func (c *Cache) Delete(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, key).Err()
}

// DeletePrefix removes every key under prefix using SCAN (cursor-based, never
// the blocking KEYS), deleting in batches as it goes. Best-effort across the
// scan: it returns the first error but keeps whatever it already deleted.
func (c *Cache) DeletePrefix(ctx context.Context, prefix string) error {
	iter := c.rdb.Scan(ctx, 0, prefix+"*", 256).Iterator()
	batch := make([]string, 0, 256)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := c.rdb.Del(ctx, batch...).Err()
		batch = batch[:0]
		return err
	}
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
		if len(batch) >= 256 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}
	return flush()
}
