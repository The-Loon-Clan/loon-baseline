// Package memory is the in-process ratelimit.Counter — a mutex-guarded map with
// lazy fixed-window expiry. Suitable for a single-process host, dev, and tests;
// several processes each get their own counts, so a shared deployment binds
// ratelimit/redis instead (no call site changes).
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/the-loon-clan/loon-baseline/ratelimit"
)

type entry struct {
	n   int64
	exp time.Time
}

// Counter is an in-memory fixed-window counter.
type Counter struct {
	mu sync.Mutex
	m  map[string]*entry
}

func New() *Counter { return &Counter{m: map[string]*entry{}} }

var _ ratelimit.Counter = (*Counter)(nil)

func (c *Counter) Incr(_ context.Context, key string, ttl time.Duration) (int64, error) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	// Bound growth: distinct IPs/keys accumulate over time, so sweep expired
	// entries when the map gets large (cheap amortised — rate-limit maps are
	// small in practice).
	if len(c.m) > 10000 {
		for k, e := range c.m {
			if now.After(e.exp) {
				delete(c.m, k)
			}
		}
	}
	e := c.m[key]
	if e == nil || now.After(e.exp) {
		e = &entry{exp: now.Add(ttl)}
		c.m[key] = e
	}
	e.n++
	return e.n, nil
}
