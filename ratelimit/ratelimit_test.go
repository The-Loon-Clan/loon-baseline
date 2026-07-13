package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ameNZB/loon-baseline/ratelimit"
	rlmemory "github.com/ameNZB/loon-baseline/ratelimit/memory"
)

func newEngine(cfg ratelimit.Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api", ratelimit.Middleware(cfg), func(c *gin.Context) { c.String(200, "ok") })
	return r
}

func get(r *gin.Engine, key string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api?apikey="+key, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestPerMinuteLimit(t *testing.T) {
	limit := 3
	r := newEngine(ratelimit.Config{
		Counter: rlmemory.New(),
		Key:     func(c *gin.Context) string { return c.Query("apikey") },
		Rules:   []ratelimit.Rule{{Name: "minute", Window: time.Minute, Limit: func(*gin.Context) int { return limit }}},
	})

	// First `limit` requests pass, then the next is rejected.
	for i := 1; i <= limit; i++ {
		if w := get(r, "alice"); w.Code != 200 {
			t.Fatalf("request %d: code=%d want 200", i, w.Code)
		}
	}
	w := get(r, "alice")
	if w.Code != 429 {
		t.Fatalf("over-limit: code=%d want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" || w.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("breach headers: retry-after=%q remaining=%q", w.Header().Get("Retry-After"), w.Header().Get("X-RateLimit-Remaining"))
	}

	// A different key has its own budget.
	if w := get(r, "bob"); w.Code != 200 {
		t.Fatalf("bob should have own budget: code=%d", w.Code)
	}
}

func TestDisabledRulePassesThrough(t *testing.T) {
	r := newEngine(ratelimit.Config{
		Counter: rlmemory.New(),
		Key:     func(c *gin.Context) string { return c.Query("apikey") },
		Rules:   []ratelimit.Rule{{Name: "minute", Window: time.Minute, Limit: func(*gin.Context) int { return 0 }}}, // 0 = disabled
	})
	for i := 0; i < 50; i++ {
		if w := get(r, "alice"); w.Code != 200 {
			t.Fatalf("disabled rule should never limit, code=%d at %d", w.Code, i)
		}
	}
}

func TestTightestRuleWins(t *testing.T) {
	// A generous per-minute rule and a tight per-"day" rule: the day rule bites first.
	r := newEngine(ratelimit.Config{
		Counter: rlmemory.New(),
		Key:     func(c *gin.Context) string { return c.Query("apikey") },
		Rules: []ratelimit.Rule{
			{Name: "minute", Window: time.Minute, Limit: func(*gin.Context) int { return 100 }},
			{Name: "day", Window: time.Hour, Limit: func(*gin.Context) int { return 2 }},
		},
	})
	get(r, "alice")
	get(r, "alice")
	w := get(r, "alice") // 3rd trips the day rule
	if w.Code != 429 {
		t.Fatalf("day rule should bite at 3rd request: code=%d", w.Code)
	}
}

func TestLimitVariesByCaller(t *testing.T) {
	// The limit is evaluated per request, so it can depend on context an earlier
	// middleware set — here a "tier" that a privileged caller has raised, and a
	// staff caller exempted entirely (limit 0 = no cap).
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api", func(c *gin.Context) { c.Set("tier", c.Query("tier")) }, ratelimit.Middleware(ratelimit.Config{
		Counter: rlmemory.New(),
		Key:     func(c *gin.Context) string { return c.Query("apikey") },
		Rules: []ratelimit.Rule{{Name: "minute", Window: time.Minute, Limit: func(c *gin.Context) int {
			switch c.GetString("tier") {
			case "staff":
				return 0 // exempt
			case "vip":
				return 5
			default:
				return 1
			}
		}}},
	}), func(c *gin.Context) { c.String(200, "ok") })

	hit := func(key, tier string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api?apikey="+key+"&tier="+tier, nil))
		return w.Code
	}

	// default tier: limit 1 -> 2nd rejected.
	hit("u1", "")
	if c := hit("u1", ""); c != 429 {
		t.Fatalf("default tier 2nd request: code=%d want 429", c)
	}
	// vip: limit 5 -> 5 ok.
	for i := 1; i <= 5; i++ {
		if c := hit("u2", "vip"); c != 200 {
			t.Fatalf("vip request %d: code=%d want 200", i, c)
		}
	}
	// staff: limit 0 (exempt) -> never limited.
	for i := 0; i < 20; i++ {
		if c := hit("u3", "staff"); c != 200 {
			t.Fatalf("staff should be exempt, code=%d at %d", c, i)
		}
	}
}

func TestOnLimitCustomResponse(t *testing.T) {
	r := newEngine(ratelimit.Config{
		Counter: rlmemory.New(),
		Key:     func(c *gin.Context) string { return c.Query("apikey") },
		Rules:   []ratelimit.Rule{{Name: "minute", Window: time.Minute, Limit: func(*gin.Context) int { return 1 }}},
		OnLimit: func(c *gin.Context, _ time.Duration) {
			c.Data(429, "application/xml", []byte(`<error code="500" description="Request limit reached"/>`))
		},
	})
	get(r, "alice")
	w := get(r, "alice")
	if w.Code != 429 || w.Header().Get("Content-Type") != "application/xml" {
		t.Fatalf("custom OnLimit: code=%d ct=%q", w.Code, w.Header().Get("Content-Type"))
	}
}
