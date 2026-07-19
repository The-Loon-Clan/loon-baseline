package webauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"

	"github.com/the-loon-clan/loon-baseline/session"
)

// harness builds a gin engine with the session middleware, a /login stamping
// route, and gated + soft routes, mirroring how a host wires the baseline.
func harness(t *testing.T, a *Auth, pwStamp int64, ipHash string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(a.Session.Middleware())
	e.GET("/login-as/:id", func(c *gin.Context) {
		id := int64(0)
		for _, r := range c.Param("id") {
			id = id*10 + int64(r-'0')
		}
		if err := session.Issue(c, id, ipHash, pwStamp); err != nil {
			c.String(500, "issue: %v", err)
			return
		}
		c.String(200, "ok")
	})
	e.GET("/admin", append(a.Require(core.RoleAdmin), func(c *gin.Context) {
		u, _ := a.Current(c)
		c.String(200, "hello %s", u.Username)
	})...)
	e.GET("/public", a.Soft(), func(c *gin.Context) {
		if u, ok := a.Current(c); ok {
			c.String(200, "user:%s", u.Username)
			return
		}
		c.String(200, "anon")
	})
	return e
}

func newTestAuth(users map[int64]*core.User, metas map[int64]Meta) *Auth {
	return &Auth{
		Session: session.Config{Secret: []byte("0123456789abcdef0123456789abcdef")},
		Resolve: func(_ context.Context, id int64) (*core.User, Meta, bool) {
			u, ok := users[id]
			return u, metas[id], ok
		},
	}
}

// get replays cookies from a prior response, like a browser.
func get(e *gin.Engine, path string, from *httptest.ResponseRecorder, html bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if html {
		req.Header.Set("Accept", "text/html")
	}
	if from != nil {
		for _, ck := range from.Result().Cookies() {
			req.AddCookie(ck)
		}
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

func TestRequireAndSoft(t *testing.T) {
	users := map[int64]*core.User{
		1: {ID: 1, Username: "alice", Role: core.RoleAdmin},
		2: {ID: 2, Username: "bob", Role: core.RoleUser},
	}
	a := newTestAuth(users, map[int64]Meta{})
	e := harness(t, a, 0, "")

	// anonymous: soft passes, gate redirects (html) / 401 (api)
	if w := get(e, "/public", nil, false); w.Body.String() != "anon" {
		t.Fatalf("anon public = %q", w.Body.String())
	}
	if w := get(e, "/admin", nil, true); w.Code != http.StatusSeeOther {
		t.Fatalf("anon admin html = %d, want 303", w.Code)
	}
	if w := get(e, "/admin", nil, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("anon admin api = %d, want 401", w.Code)
	}

	// alice (admin): full round trip through the mysession cookie
	login := get(e, "/login-as/1", nil, false)
	if w := get(e, "/admin", login, false); w.Code != 200 || w.Body.String() != "hello alice" {
		t.Fatalf("alice admin = %d %q", w.Code, w.Body.String())
	}
	if w := get(e, "/public", login, false); w.Body.String() != "user:alice" {
		t.Fatalf("alice public = %q", w.Body.String())
	}

	// bob (user): below RoleAdmin → 403
	login = get(e, "/login-as/2", nil, false)
	if w := get(e, "/admin", login, false); w.Code != http.StatusForbidden {
		t.Fatalf("bob admin = %d, want 403", w.Code)
	}
}

func TestServerSideExpiry(t *testing.T) {
	users := map[int64]*core.User{1: {ID: 1, Username: "alice", Role: core.RoleAdmin}}
	a := newTestAuth(users, map[int64]Meta{})
	e := harness(t, a, 0, "")

	login := get(e, "/login-as/1", nil, false)
	if w := get(e, "/admin", login, false); w.Code != 200 {
		t.Fatalf("fresh session = %d", w.Code)
	}
	// travel past MaxAge: the cookie is still valid client-side, login_at is not
	old := nowUnix
	nowUnix = func() int64 { return time.Now().Add(8 * 24 * time.Hour).Unix() }
	defer func() { nowUnix = old }()
	if w := get(e, "/admin", login, false); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session = %d, want 401", w.Code)
	}
}

func TestPasswordChangeInvalidation(t *testing.T) {
	users := map[int64]*core.User{1: {ID: 1, Username: "alice", Role: core.RoleAdmin}}
	metas := map[int64]Meta{1: {PasswordChangedAt: 1000}}
	a := newTestAuth(users, metas)
	e := harness(t, a, 1000, "") // session stamped with the current value

	login := get(e, "/login-as/1", nil, false)
	if w := get(e, "/admin", login, false); w.Code != 200 {
		t.Fatalf("matching stamp = %d", w.Code)
	}
	// password changes: DB stamp advances past the session's
	metas[1] = Meta{PasswordChangedAt: 2000}
	w := get(e, "/admin", login, true)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("stale stamp = %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login?reason=password_changed" {
		t.Fatalf("redirect = %q", loc)
	}
}

func TestAdminIPPinning(t *testing.T) {
	users := map[int64]*core.User{1: {ID: 1, Username: "alice", Role: core.RoleAdmin}}
	a := newTestAuth(users, map[int64]Meta{})
	currentIP := "hash-A"
	a.IPHash = func(_ *gin.Context) string { return currentIP }
	e := harness(t, a, 0, "hash-A") // login stamped from hash-A

	login := get(e, "/login-as/1", nil, false)
	if w := get(e, "/admin", login, false); w.Code != 200 {
		t.Fatalf("same IP = %d", w.Code)
	}
	currentIP = "hash-B" // request now arrives from a different IP
	w := get(e, "/admin", login, true)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login?reason=ip_changed" {
		t.Fatalf("pinned session = %d %q, want 303 → ip_changed", w.Code, w.Header().Get("Location"))
	}
}
