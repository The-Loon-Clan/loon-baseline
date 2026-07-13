// Package ratelimit is loon-baseline's request rate limiter: a small Counter
// abstraction (fixed-window atomic increment) with two swappable impls
// (ratelimit/memory, ratelimit/redis) and a gin middleware that enforces one or
// more windowed rules — e.g. a per-minute burst cap AND a per-day quota on the
// same request. It mirrors the cache convention (interface + impl + mockable):
// dev/tests run on the in-memory counter with no Redis; several api workers
// share one Redis counter so a client's quota is global, not per-process.
//
// Limits are read through a func() int per rule, so a host can back them with
// admin-editable settings (schedule config vars) that change without a redeploy;
// a rule whose limit is <= 0 is disabled. The limiter is best-effort: if the
// counter backend errors, the request is allowed rather than failed closed — a
// rate limiter should never take the API down.
package ratelimit

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// Counter is a fixed-window atomic counter. Incr adds one to the value at key
// and returns the new total; on the first increment of a window it arms the key
// to expire after ttl, so the window rolls forward on its own. Implementations
// MUST make the increment atomic across processes (Redis INCR / a mutex) or the
// limit leaks under concurrency.
type Counter interface {
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// Rule is one windowed limit. Name namespaces the counter key and appears in
// logs/headers ("minute", "day"). Limit is read per request so an admin change
// takes effect live; <= 0 disables the rule.
type Rule struct {
	Name   string
	Window time.Duration
	Limit  func() int
}

// Config wires the middleware. Key attributes a request to a caller (API key,
// else client IP) — return "" to skip limiting this request. OnLimit renders the
// rejection (a Newznab host writes error XML); when nil, a plain 429 is sent.
type Config struct {
	Counter Counter
	Key     func(*gin.Context) string
	Rules   []Rule
	OnLimit func(c *gin.Context, retryAfter time.Duration)
}

// Middleware enforces every enabled rule against the request's key. It sets
// X-RateLimit-Limit / X-RateLimit-Remaining to the tightest remaining budget,
// and on a breach sends Retry-After (the window length) before aborting.
func Middleware(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := cfg.Key(c)
		if key == "" {
			c.Next()
			return
		}
		bestRemaining, bestLimit := -1, 0
		for _, r := range cfg.Rules {
			limit := r.Limit()
			if limit <= 0 {
				continue // rule disabled
			}
			n, err := cfg.Counter.Incr(c.Request.Context(), "rl:"+r.Name+":"+key, r.Window)
			if err != nil {
				continue // best-effort: never fail closed on a counter error
			}
			if int(n) > limit {
				setHeaders(c, limit, 0)
				c.Header("Retry-After", strconv.Itoa(int(r.Window.Seconds())))
				if cfg.OnLimit != nil {
					cfg.OnLimit(c, r.Window)
				} else {
					c.String(429, "rate limit exceeded")
				}
				c.Abort()
				return
			}
			if remaining := limit - int(n); bestRemaining < 0 || remaining < bestRemaining {
				bestRemaining, bestLimit = remaining, limit
			}
		}
		if bestRemaining >= 0 {
			setHeaders(c, bestLimit, bestRemaining)
		}
		c.Next()
	}
}

func setHeaders(c *gin.Context, limit, remaining int) {
	c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
	c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
}
