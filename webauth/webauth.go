// Package webauth turns the session layer + a host user-resolver into the
// current-user middleware and role gates a loon host needs, and wires them into
// loon's core.AuthService seam.
//
// The auth semantics are a faithful extraction of the production site's
// AuthRequired (web/handlers/handlers.go): server-side expiry via login_at,
// password-change invalidation via the password_changed_at stamp, optional
// admin IP pinning, and clear-session-on-failure. Product-specific enrichment
// (inbox badges, nav pins, preference caching) stays in the host via the
// Enrich hook.
package webauth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"

	"github.com/the-loon-clan/loon-baseline/session"
)

// nowUnix is a var so expiry tests can travel in time.
var nowUnix = func() int64 { return time.Now().Unix() }

// Meta carries the per-user invalidation state the resolver supplies alongside
// the user.
type Meta struct {
	// PasswordChangedAt is the user's password_changed_at unix stamp. Sessions
	// stamped with an older value are invalidated (prod: sess < db ⇒ dead; a
	// pre-migration cookie has no key → 0 → forces one fresh login, correct).
	// Return 0 when the host has no such column — the check is skipped.
	PasswordChangedAt int64
}

// Resolver loads the host's user for a session user id. ok is false when the
// user no longer exists or is disabled (role below RoleUser — banned users'
// sessions die here, exactly like prod).
type Resolver func(ctx context.Context, id int64) (u *core.User, meta Meta, ok bool)

// Auth is the middleware bundle. Set Session + Resolve; the rest is optional.
type Auth struct {
	Session   session.Config
	Resolve   Resolver
	LoginPath string // default "/login"

	// IPHash, when set, enables prod's admin IP pinning: sessions of RoleAdmin
	// users whose current hashed IP differs from the login_ip stamp are
	// invalidated (→ /login?reason=ip_changed, which forces 2FA again on a
	// host that has it). Supply the same salted hash used at Issue time.
	IPHash func(c *gin.Context) string

	// Enrich, when set, runs after a successful auth with the resolved user —
	// the seam for host extras prod keeps in AuthRequired (unread counts,
	// admin pins, preference caching). Failures inside must be best-effort.
	Enrich func(c *gin.Context, u *core.User)
}

const ctxUser = "loon.user"

func (a Auth) login() string {
	if a.LoginPath == "" {
		return "/login"
	}
	return a.LoginPath
}

// authFail is why a session was rejected; it maps to prod's redirect reasons.
type authFail int

const (
	failNone authFail = iota
	failNoSession
	failExpired
	failNoUser
	failPasswordChanged
	failIPChanged
)

func (f authFail) loginQuery() string {
	switch f {
	case failPasswordChanged:
		return "?reason=password_changed"
	case failIPChanged:
		return "?reason=ip_changed"
	}
	return ""
}

// resolve runs the full prod check chain. On any failure the session is cleared
// (stale cookies shouldn't re-fail every request) and the reason returned.
func (a Auth) resolve(c *gin.Context) (*core.User, authFail) {
	claims, ok := session.Read(c)
	if !ok {
		return nil, failNoSession
	}
	// Server-side expiry: login_at is authoritative regardless of cookie MaxAge.
	if claims.LoginAt == 0 || nowUnix()-claims.LoginAt > int64(a.Session.MaxAgeD().Seconds()) {
		_ = session.Clear(c)
		return nil, failExpired
	}
	u, meta, ok := a.Resolve(c.Request.Context(), claims.UserID)
	if !ok || u == nil {
		_ = session.Clear(c)
		return nil, failNoUser
	}
	// Password-change invalidation: every session stamps password_changed_at at
	// login; if the DB stamp advanced, every older cookie is dead.
	if meta.PasswordChangedAt != 0 && claims.PasswordChangedAt < meta.PasswordChangedAt {
		_ = session.Clear(c)
		return nil, failPasswordChanged
	}
	// Admin IP pinning (prod: exactly RoleAdmin, not mods).
	if a.IPHash != nil && u.Role == core.RoleAdmin {
		if claims.LoginIP != "" && claims.LoginIP != a.IPHash(c) {
			_ = session.Clear(c)
			return nil, failIPChanged
		}
	}
	return u, failNone
}

// Soft loads the session user into the context when the full check chain
// passes, but never blocks — for public pages that render differently when
// logged in (prod's SoftAuth).
func (a Auth) Soft() gin.HandlerFunc {
	return func(c *gin.Context) {
		if u, fail := a.resolve(c); fail == failNone {
			c.Set(ctxUser, u)
			c.Set("userID", u.ID)
			if a.Enrich != nil {
				a.Enrich(c, u)
			}
		}
		c.Next()
	}
}

// Require aborts the request when there is no valid session OR the user is
// below minRole. Browser requests redirect to LoginPath (carrying prod's
// ?reason= for password/IP invalidation); API requests get JSON.
func (a Auth) Require(minRole core.Role) gin.HandlersChain {
	return gin.HandlersChain{func(c *gin.Context) {
		u, fail := a.resolve(c)
		if fail != failNone {
			if wantsHTML(c) {
				c.Redirect(http.StatusSeeOther, a.login()+fail.loginQuery())
				c.Abort()
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "log in first"})
			return
		}
		if u.Role < minRole {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"ok": false, "error": "insufficient role"})
			return
		}
		c.Set(ctxUser, u)
		c.Set("userID", u.ID)
		if a.Enrich != nil {
			a.Enrich(c, u)
		}
		c.Next()
	}}
}

// RequireExact aborts unless the user has exactly the given role (the
// core.AuthAdapter.RequireRoleFn shape). Prefer Require for AtLeast semantics.
func (a Auth) RequireExact(role core.Role) gin.HandlersChain {
	return gin.HandlersChain{func(c *gin.Context) {
		u, fail := a.resolve(c)
		if fail != failNone || u.Role != role {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"ok": false, "error": "wrong role"})
			return
		}
		c.Set(ctxUser, u)
		c.Next()
	}}
}

// Current returns the request's user: the one a middleware already resolved
// into the context, or a fresh resolve (so it works on routes with no auth
// middleware, e.g. public pages rendering the navbar).
func (a Auth) Current(c *gin.Context) (*core.User, bool) {
	if v, ok := c.Get(ctxUser); ok {
		if u, ok := v.(*core.User); ok {
			return u, true
		}
	}
	u, fail := a.resolve(c)
	return u, fail == failNone
}

func wantsHTML(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "text/html")
}
