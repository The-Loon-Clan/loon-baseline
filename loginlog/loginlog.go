// Package loginlog records login attempts (with a salted IP hash, never the
// raw IP) and renders them as loon core.Views: an admin "all attempts" page
// and a per-user "recent sign-ins" page. The host records each attempt at its
// login handler (where the request IP lives) via Store.Record; loon-baseline
// owns the storage + the views. Handed to the host as []core.View — it isn't a
// plugin.
//
// Scope note: this logs attempts. Server-side SESSION management ("sign out my
// other devices") needs a server-side session store; a cookie-session host
// (like the demo) can't enumerate active sessions, so that isn't offered here.
package loginlog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ameNZB/loon/core"
)

//go:embed templates/*.html
var viewFS embed.FS

// Entry is one login attempt.
type Entry struct {
	ID        int64
	UserID    int64  // 0 when the attempted username didn't resolve to a user
	Username  string // as typed at the login form
	IPHash    string // salted hash of the client IP ("" if unknown)
	Success   bool
	CreatedAt time.Time
}

// Store persists and reads login attempts.
type Store interface {
	Record(ctx context.Context, e Entry) error
	Recent(ctx context.Context, userID int64, limit int) ([]Entry, error) // one user's own
	RecentAll(ctx context.Context, limit int) ([]Entry, error)            // admin: everyone
}

// HashIP returns a salted SHA-256 hex digest of ip, or "" if ip is empty.
// Hashing keeps raw IPs out of the DB while still letting the same address be
// recognised across attempts. Pass a per-deployment secret salt.
func HashIP(salt, ip string) string {
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(salt + "|" + ip))
	return hex.EncodeToString(sum[:])
}

// CurrentFunc resolves the logged-in user for a request (host middleware).
type CurrentFunc func(*gin.Context) (*core.User, bool)

type handler struct {
	store   Store
	current CurrentFunc
	tmpl    *template.Template
}

// Views returns two login-history views to register on the Core after Boot:
//
//	/admin/p/login-log  — all recent attempts (admin-gated by the host)
//	/p/sign-ins         — the signed-in user's own recent sign-ins
func Views(store Store, current CurrentFunc) ([]core.View, error) {
	t, err := template.ParseFS(viewFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	h := &handler{store: store, current: current, tmpl: t}
	return []core.View{
		{
			Slug: "login-log", Title: "Login log", Slot: core.SlotAdminPage,
			Render: h.renderAdmin,
		},
		{
			Slug: "sign-ins", Title: "Sign-ins", Slot: core.SlotSitePage,
			MinRole: core.RoleUser, // any logged-in account, their own history
			Render:  h.renderUser,
		},
	}, nil
}

type row struct {
	When    string
	User    string
	UserID  int64
	IPShort string
	Success bool
}

func toRows(es []Entry) []row {
	out := make([]row, len(es))
	for i, e := range es {
		ip := e.IPHash
		if len(ip) > 12 {
			ip = ip[:12]
		}
		out[i] = row{
			When:    e.CreatedAt.Format("2006-01-02 15:04:05"),
			User:    e.Username,
			UserID:  e.UserID,
			IPShort: ip,
			Success: e.Success,
		}
	}
	return out
}

func (h *handler) renderAdmin(c *gin.Context) (template.HTML, error) {
	es, err := h.store.RecentAll(c.Request.Context(), 200)
	if err != nil {
		return "", err
	}
	return h.exec("admin.html", map[string]any{"Rows": toRows(es)})
}

func (h *handler) renderUser(c *gin.Context) (template.HTML, error) {
	u, ok := h.current(c)
	if !ok {
		return "", nil // site gate prevents this; render nothing if reached
	}
	es, err := h.store.Recent(c.Request.Context(), u.ID, 50)
	if err != nil {
		return "", err
	}
	return h.exec("user.html", map[string]any{"Rows": toRows(es)})
}

func (h *handler) exec(name string, data any) (template.HTML, error) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}
