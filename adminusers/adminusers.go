// Package adminusers is a batteries-included user-management admin surface —
// list users, change role, reset password — provided as loon core.Views that a
// host registers and the view system mounts (the host wraps the fragments in
// its own chrome). It operates over users.Store, so it works for any host,
// including one with its own users table.
//
// loon-baseline isn't a plugin (can't self-register), so the host calls Views()
// after Boot and RegisterView()s each — see the demo's main.go.
package adminusers

import (
	"bytes"
	"context"
	"embed"
	"html/template"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"

	"github.com/the-loon-clan/loon-baseline/password"
	"github.com/the-loon-clan/loon-baseline/users"
)

//go:embed templates/*.html
var viewFS embed.FS

const pageURL = "/admin/p/users"

type handler struct {
	store  users.Store
	hasher password.Hasher
	tmpl   *template.Template
}

// Views returns the user-management admin views. Register each on the Core
// (c.RegisterView) after Boot; a view-system host mounts them at
// /admin/p/users (+ POST set-role / reset-password actions) and lists them in
// the admin nav.
func Views(store users.Store, hasher password.Hasher) ([]core.View, error) {
	t, err := template.ParseFS(viewFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	h := &handler{store: store, hasher: hasher, tmpl: t}
	return []core.View{{
		Slug: "users", Title: "Users", Slot: core.SlotAdminPage,
		Render: func(c *gin.Context) (template.HTML, error) {
			return h.render(c.Request.Context(), c.Query("msg"), c.Query("err"))
		},
		Actions: map[string]func(*gin.Context) (template.HTML, error){
			"set-role":       h.setRole,
			"reset-password": h.resetPassword,
		},
	}}, nil
}

var roleOpts = []struct {
	Value int
	Label string
}{
	{int(core.RoleBanned), "Banned"},
	{int(core.RoleUser), "User"},
	{int(core.RoleContributor), "Contributor"},
	{int(core.RoleMod), "Mod"},
	{int(core.RoleAdmin), "Admin"},
}

type userRow struct {
	ID       int64
	Username string
	Email    string
	Role     int
	Joined   string
}

func (h *handler) render(ctx context.Context, msg, errMsg string) (template.HTML, error) {
	list, total, err := h.store.List(ctx, 0, 200)
	if err != nil {
		return "", err
	}
	rows := make([]userRow, len(list))
	for i, u := range list {
		rows[i] = userRow{ID: u.ID, Username: u.Username, Email: u.Email, Role: int(u.Role), Joined: u.CreatedAt.Format("2006-01-02")}
	}
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "users.html", map[string]any{
		"Users": rows, "Roles": roleOpts, "Total": total, "Msg": msg, "Err": errMsg,
	}); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func (h *handler) setRole(c *gin.Context) (template.HTML, error) {
	id, _ := strconv.ParseInt(c.PostForm("user_id"), 10, 64)
	role, err := strconv.Atoi(c.PostForm("role"))
	if id <= 0 || err != nil {
		return redirect(c, "err", "bad request")
	}
	if err := h.store.SetRole(c.Request.Context(), id, core.Role(role)); err != nil {
		return redirect(c, "err", err.Error())
	}
	return redirect(c, "msg", "role updated")
}

func (h *handler) resetPassword(c *gin.Context) (template.HTML, error) {
	id, _ := strconv.ParseInt(c.PostForm("user_id"), 10, 64)
	pw := c.PostForm("password")
	if id <= 0 || len(pw) < 8 {
		return redirect(c, "err", "password must be at least 8 characters")
	}
	hash, err := h.hasher.Hash(pw)
	if err != nil {
		return redirect(c, "err", err.Error())
	}
	if err := h.store.UpdatePasswordHash(c.Request.Context(), id, hash); err != nil {
		return redirect(c, "err", err.Error())
	}
	return redirect(c, "msg", "password reset")
}

func redirect(c *gin.Context, key, val string) (template.HTML, error) {
	c.Redirect(http.StatusSeeOther, pageURL+"?"+key+"="+url.QueryEscape(val))
	return "", nil
}
