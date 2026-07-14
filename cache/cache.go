// Package cache is loon-baseline's small key/value cache abstraction: a Cache
// interface with two swappable impls (cache/memory, cache/redis), mirroring the
// storage convention (interface + impl + mockable). A host or plugin caches
// against the interface, so tests and the demo run on the in-memory impl with
// no Redis, and prod can bind the redis impl — without any call site changing.
package cache

import (
	"context"
	"encoding/json"
	"time"
)

// Cache is a byte-blob key/value store with per-key TTL. Values are opaque
// bytes; use GetJSON/SetJSON for structs. A miss is (ok=false), never an error.
type Cache interface {
	Get(ctx context.Context, key string) (val []byte, ok bool, err error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// PrefixDeleter is an optional capability a Cache may implement: drop every key
// under a namespace prefix in one call. Used for coarse invalidation — e.g. a
// worker clears the "newznab:v1:" search-cache namespace after an ingest so the
// API tier repopulates from fresh data. Callers type-assert for it:
//
//	if pd, ok := c.(cache.PrefixDeleter); ok { _ = pd.DeletePrefix(ctx, "newznab:v1:") }
//
// Both bundled impls (cache/memory, cache/redis) provide it.
type PrefixDeleter interface {
	DeletePrefix(ctx context.Context, prefix string) error
}

// GetJSON reads key and JSON-unmarshals it into dst. ok=false on a miss (dst
// untouched).
func GetJSON(ctx context.Context, c Cache, key string, dst any) (bool, error) {
	b, ok, err := c.Get(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	return true, json.Unmarshal(b, dst)
}

// SetJSON JSON-marshals v and stores it under key for ttl.
func SetJSON(ctx context.Context, c Cache, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, b, ttl)
}
