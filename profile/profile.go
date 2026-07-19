// Package profile is a batteries-included public user-profile summary: a
// SlotUserWidget card (role, joined) rendered for the profile's SUBJECT
// (core.ViewSubject). It's the first thing to exercise loon's user.* view
// slots — the host mounts the /u/<name> page and sets the subject; this fills
// in the summary card, and any plugin can add its own SlotUserWidget/SlotUserTab
// to the same page (e.g. dailyreward's streak).
//
// loon-baseline isn't a plugin, so it hands the host []core.View to register.
package profile

import (
	"bytes"
	"context"
	"embed"
	"html/template"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"
)

//go:embed templates/*.html
var viewFS embed.FS

// Resolver looks up the profile subject by id (the host's user store).
type Resolver func(ctx context.Context, id int64) (*core.User, bool)

type handler struct {
	resolve Resolver
	tmpl    *template.Template
}

// Views returns the profile summary view (a public SlotUserWidget). Register on
// the Core after Boot; the host's /u/<name> page renders it for the subject.
func Views(resolve Resolver) ([]core.View, error) {
	t, err := template.ParseFS(viewFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	h := &handler{resolve: resolve, tmpl: t}
	return []core.View{{
		Slug: "summary", Title: "Profile", Slot: core.SlotUserWidget,
		Public: true, // public profiles
		Render: h.render,
	}}, nil
}

func (h *handler) render(c *gin.Context) (template.HTML, error) {
	id, ok := core.ViewSubject(c)
	if !ok {
		return "", nil
	}
	u, ok := h.resolve(c.Request.Context(), id)
	if !ok || u == nil {
		return "", nil
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "summary.html", map[string]any{
		"Role":   roleName(u.Role),
		"Joined": u.CreatedAt.Format("2006-01-02"),
	}); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func roleName(r core.Role) string {
	switch r {
	case core.RoleBanned:
		return "Banned"
	case core.RoleContributor:
		return "Contributor"
	case core.RoleMod:
		return "Mod"
	case core.RoleAdmin:
		return "Admin"
	default:
		return "User"
	}
}
