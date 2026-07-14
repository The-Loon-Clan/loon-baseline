package session

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func roundTrip(t *testing.T, cfg Config) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(cfg.Middleware())
	r.GET("/set", func(c *gin.Context) {
		if err := Issue(c, 42, "iphash", 1700000000); err != nil {
			c.String(500, "issue: %v", err)
			return
		}
		c.String(200, "ok")
	})
	r.GET("/get", func(c *gin.Context) {
		claims, ok := Read(c)
		if !ok {
			c.String(401, "no session")
			return
		}
		c.String(200, "uid=%d pca=%d", claims.UserID, claims.PasswordChangedAt)
	})

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/set", nil))
	if w1.Code != 200 {
		t.Fatalf("set: code=%d body=%q", w1.Code, w1.Body.String())
	}
	ck := strings.SplitN(w1.Header().Get("Set-Cookie"), ";", 2)[0]
	if !strings.HasPrefix(ck, "mysession=") {
		t.Fatalf("no mysession cookie: %q", ck)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/get", nil)
	req2.Header.Set("Cookie", ck)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Body.String() != "uid=42 pca=1700000000" {
		t.Fatalf("round-trip: %q", w2.Body.String())
	}
}

func TestCookieStoreRoundTrip(t *testing.T) {
	roundTrip(t, Config{Secret: []byte("test-secret-at-least-32-characters!!")})
}

// Redis-backed store: same contract, cookie is an opaque id. Skipped unless
// REDIS_TEST_ADDR is set (docker run --rm -p 6399:6379 redis:7-alpine).
func TestRedisStoreRoundTrip(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	roundTrip(t, Config{
		Secret:    []byte("test-secret-at-least-32-characters!!"),
		RedisAddr: addr,
	})
}
