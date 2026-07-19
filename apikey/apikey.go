// Package apikey is the Newznab-style API key: one long-lived key per user,
// carried in the ?apikey= query string that Sonarr/Prowlarr/Radarr send. It
// gives the read tier (loon-api) a way to authenticate + attribute a request to
// a user, and the user a self-service page to view and regenerate ("refresh")
// their key. Modeled on prod: a 256-bit hex key, plaintext at rest (it travels
// in URLs, so it's a URL credential rather than a password), rotatable with a
// rotated_at stamp.
//
// Handed to the host as []core.View — it isn't a plugin. The store is
// deliberately read-mostly: Resolve is a pure SELECT so the API read tier can
// run against a Postgres read replica (see LOON-DISTRIBUTED); only Ensure/Rotate
// write, and those happen on the web tier.
package apikey

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"
)

//go:embed templates/*.html
var viewFS embed.FS

const pageURL = "/p/api-key"

// Key is one user's API key.
type Key struct {
	UserID    int64
	Key       string
	RotatedAt time.Time // zero until first rotated
	CreatedAt time.Time
}

// Store persists one key per user.
type Store interface {
	// Resolve maps a raw key to its user id. ok=false on no match (not an
	// error). Pure read — safe against a replica.
	Resolve(ctx context.Context, key string) (userID int64, ok bool, err error)
	// Ensure returns the user's key, generating + persisting one on first call
	// so every user always has a key to show.
	Ensure(ctx context.Context, userID int64) (Key, error)
	// Rotate replaces the user's key with a fresh one (the "refresh"),
	// stamping RotatedAt. The previous key stops resolving immediately.
	Rotate(ctx context.Context, userID int64) (Key, error)
}

// Generate returns a 256-bit random key as hex. Keys surface in URL query
// strings and proxy logs (Newznab convention), so the entropy margin is
// deliberately generous.
func Generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Resolver is the narrow read side loon-api needs: key -> user id. A store-based
// host passes store.Resolve directly.
type Resolver func(ctx context.Context, key string) (userID int64, ok bool, err error)

// CurrentFunc resolves the logged-in user for a request (host middleware).
type CurrentFunc func(*gin.Context) (*core.User, bool)

type handler struct {
	store   Store
	current CurrentFunc
	tmpl    *template.Template
}

// Views returns the self-service API-key page (view + regenerate action) as a
// login-gated site page. Register on the Core after Boot; a view-system host
// mounts it at /p/api-key (+ the regenerate POST) and lists it in the site nav
// for signed-in viewers.
func Views(store Store, current CurrentFunc) ([]core.View, error) {
	t, err := template.ParseFS(viewFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	h := &handler{store: store, current: current, tmpl: t}
	return []core.View{{
		Slug: "api-key", Title: "API key", Slot: core.SlotSitePage,
		MinRole: core.RoleUser, // any logged-in account, their own key
		Render:  h.render,
		Actions: map[string]func(*gin.Context) (template.HTML, error){
			"regenerate": h.regenerate,
		},
	}}, nil
}

func (h *handler) render(c *gin.Context) (template.HTML, error) {
	u, ok := h.current(c)
	if !ok {
		return "", nil // the site gate prevents this; render nothing if reached
	}
	k, err := h.store.Ensure(c.Request.Context(), u.ID)
	if err != nil {
		return "", err
	}
	return h.view(k, c.Query("msg"))
}

func (h *handler) view(k Key, msg string) (template.HTML, error) {
	rotated := ""
	if !k.RotatedAt.IsZero() {
		rotated = k.RotatedAt.Format("2006-01-02 15:04")
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "apikey.html", map[string]any{
		"Key":     k.Key,
		"Rotated": rotated,
		"Msg":     msg,
	}); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func (h *handler) regenerate(c *gin.Context) (template.HTML, error) {
	u, ok := h.current(c)
	if !ok {
		return "", nil
	}
	if _, err := h.store.Rotate(c.Request.Context(), u.ID); err != nil {
		return "", err
	}
	c.Redirect(http.StatusSeeOther, pageURL+"?msg="+url.QueryEscape("API key regenerated — update the new key in your connected apps"))
	return "", nil
}
