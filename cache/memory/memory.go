// Package memory is the in-memory cache.Cache impl: a mutex-guarded map with
// lazy TTL expiry. It's the default for dev/tests/the demo (no Redis needed).
// Keys are only reclaimed on access or Delete, so it suits bounded key spaces
// (page caches, per-request memoization) rather than unbounded growth.
package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ameNZB/loon-baseline/cache"
)

type entry struct {
	val []byte
	exp time.Time // zero = no expiry
}

// Cache is the in-memory implementation.
type Cache struct {
	mu sync.Mutex
	m  map[string]entry
}

// New builds an empty in-memory cache.
func New() *Cache { return &Cache{m: map[string]entry{}} }

var (
	_ cache.Cache         = (*Cache)(nil)
	_ cache.PrefixDeleter = (*Cache)(nil)
)

func (c *Cache) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return nil, false, nil
	}
	if !e.exp.IsZero() && time.Now().After(e.exp) {
		delete(c.m, key)
		return nil, false, nil
	}
	// return a copy so callers can't mutate the cached bytes
	out := make([]byte, len(e.val))
	copy(out, e.val)
	return out, true, nil
}

func (c *Cache) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	e := entry{val: make([]byte, len(val))}
	copy(e.val, val)
	if ttl > 0 {
		e.exp = time.Now().Add(ttl)
	}
	c.mu.Lock()
	c.m[key] = e
	c.mu.Unlock()
	return nil
}

func (c *Cache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	delete(c.m, key)
	c.mu.Unlock()
	return nil
}

// DeletePrefix removes every key beginning with prefix.
func (c *Cache) DeletePrefix(_ context.Context, prefix string) error {
	c.mu.Lock()
	for k := range c.m {
		if strings.HasPrefix(k, prefix) {
			delete(c.m, k)
		}
	}
	c.mu.Unlock()
	return nil
}
