// Package account is the self-service account surface — a profile summary plus
// a change-password form — provided as a login-gated loon core.View the host
// registers and the view system mounts at /p/account. It closes the loop on
// authflow: authflow.ChangePassword existed but nothing gave the user a way to
// call it. Like adminusers, loon-baseline hands the host []core.View rather
// than self-registering (it isn't a plugin).
package account

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/ameNZB/loon/core"

	"github.com/ameNZB/loon-baseline/authflow"
)

//go:embed templates/*.html
var viewFS embed.FS

const pageURL = "/p/account"

// CurrentFunc resolves the logged-in user for a request — the host's
// current-user middleware (e.g. webauth.Auth.Current).
type CurrentFunc func(*gin.Context) (*core.User, bool)

type handler struct {
	flow    authflow.Flow
	current CurrentFunc
	tmpl    *template.Template
}

// Views returns the self-service account view (profile + change password) as a
// site page gated to any logged-in (non-banned) user. Register on the Core
// after Boot; a view-system host mounts it at /p/account (+ the
// change-password POST action) and lists it in the site nav for signed-in
// viewers.
func Views(flow authflow.Flow, current CurrentFunc) ([]core.View, error) {
	t, err := template.ParseFS(viewFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	h := &handler{flow: flow, current: current, tmpl: t}
	return []core.View{{
		Slug: "account", Title: "Account", Slot: core.SlotSitePage,
		MinRole: core.RoleUser, // zero-value role: any logged-in account
		Render:  h.render,
		Actions: map[string]func(*gin.Context) (template.HTML, error){
			"change-password": h.changePassword,
		},
	}}, nil
}

func (h *handler) render(c *gin.Context) (template.HTML, error) {
	u, ok := h.current(c)
	if !ok {
		return "", nil // the site gate prevents this; render nothing if reached
	}
	return h.view(u, c.Query("msg"), c.Query("err"))
}

func (h *handler) view(u *core.User, msg, errMsg string) (template.HTML, error) {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "account.html", map[string]any{
		"User":     u,
		"RoleName": roleName(u.Role),
		"Joined":   u.CreatedAt.Format("2006-01-02"),
		"Msg":      msg,
		"Err":      errMsg,
	}); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func (h *handler) changePassword(c *gin.Context) (template.HTML, error) {
	u, ok := h.current(c)
	if !ok {
		return redirect(c, "err", "not signed in")
	}
	cur := c.PostForm("current_password")
	nw := c.PostForm("new_password")
	if nw != c.PostForm("confirm_password") {
		return redirect(c, "err", "new passwords do not match")
	}
	if err := h.flow.ChangePassword(c.Request.Context(), u.ID, cur, nw); err != nil {
		return redirect(c, "err", changeErr(err))
	}
	return redirect(c, "msg", "password changed")
}

func changeErr(err error) string {
	switch {
	case errors.Is(err, authflow.ErrBadCredentials):
		return "current password is incorrect"
	case errors.Is(err, authflow.ErrWeakPassword):
		return "new password must be at least 8 characters"
	default:
		return "could not change password"
	}
}

func roleName(r core.Role) string {
	switch r {
	case core.RoleBanned:
		return "Banned"
	case core.RoleDisabled:
		return "Disabled"
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

func redirect(c *gin.Context, key, val string) (template.HTML, error) {
	c.Redirect(http.StatusSeeOther, pageURL+"?"+key+"="+url.QueryEscape(val))
	return "", nil
}
