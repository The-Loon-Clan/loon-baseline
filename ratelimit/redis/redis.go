// Package redis is the Redis-backed ratelimit.Counter (go-redis v9). Several api
// workers share one Redis so a caller's window is counted globally, not
// per-process. A host binds it in place of ratelimit/memory; no call site
// changes.
package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ameNZB/loon-baseline/ratelimit"
)

// incrExpire increments a key and, only on the first increment of a window, arms
// its expiry — atomically, so a crash between INCR and EXPIRE can't leave a
// never-expiring key (which would pin a caller over-limit forever). Refreshing
// the TTL on every hit would turn the fixed window into one that never resets
// under sustained load, so the EXPIRE is guarded by n == 1.
var incrExpire = goredis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return n
`)

// Counter wraps a go-redis client as a ratelimit.Counter.
type Counter struct{ rdb *goredis.Client }

func New(rdb *goredis.Client) *Counter { return &Counter{rdb: rdb} }

var _ ratelimit.Counter = (*Counter)(nil)

func (c *Counter) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	return incrExpire.Run(ctx, c.rdb, []string{key}, ttl.Milliseconds()).Int64()
}
