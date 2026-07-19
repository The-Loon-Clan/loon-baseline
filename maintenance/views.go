package maintenance

import (
	"bytes"
	"html/template"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"
)

// Views returns the admin maintenance toggle as a core.View (SlotAdminPage).
// Register on the Core after Boot; a view-system host mounts it at
// /admin/p/maintenance plus the begin/end POST actions. Note: when maintenance
// is ON, the host's Middleware Allow predicate MUST let /admin through, or the
// admin can't reach this page to turn it off.
func (c *Controller) Views() ([]core.View, error) {
	return []core.View{{
		Slug: "maintenance", Title: "Maintenance", Slot: core.SlotAdminPage,
		Render: c.renderAdmin,
		Actions: map[string]func(*gin.Context) (template.HTML, error){
			"begin": c.beginAction,
			"end":   c.endAction,
		},
	}}, nil
}

func (c *Controller) renderAdmin(g *gin.Context) (template.HTML, error) {
	s := c.Snapshot()
	var buf bytes.Buffer
	if err := adminTmpl.Execute(&buf, map[string]any{
		"Active":  s.Active,
		"Reason":  s.Reason,
		"ETAMins": s.ETASecs / 60,
	}); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func (c *Controller) beginAction(g *gin.Context) (template.HTML, error) {
	etaMins, _ := strconv.Atoi(g.PostForm("eta_mins"))
	if err := c.Begin(g.Request.Context(), g.PostForm("reason"), etaMins*60); err != nil {
		return "", err
	}
	g.Redirect(http.StatusSeeOther, "/admin/p/maintenance")
	return "", nil
}

func (c *Controller) endAction(g *gin.Context) (template.HTML, error) {
	if err := c.End(g.Request.Context()); err != nil {
		return "", err
	}
	g.Redirect(http.StatusSeeOther, "/admin/p/maintenance")
	return "", nil
}

var adminTmpl = template.Must(template.New("madmin").Parse(`
<div class="card"><div class="card-body">
  <h5 class="card-title mb-3">Maintenance mode</h5>
  {{if .Active}}
    <p><span class="badge bg-warning text-dark">ON</span> The site is showing the maintenance page to visitors.</p>
    {{if .Reason}}<p class="text-muted small mb-3">Reason: {{.Reason}}</p>{{end}}
    <form method="post" action="/admin/p/maintenance/end">
      <button class="btn btn-success btn-sm">End maintenance</button>
    </form>
  {{else}}
    <p><span class="badge bg-secondary">OFF</span> The site is serving normally.</p>
    <form method="post" action="/admin/p/maintenance/begin">
      <div class="mb-2">
        <label class="form-label small mb-1">Reason (shown on the page)</label>
        <input type="text" name="reason" class="form-control form-control-sm" placeholder="Upgrading the database">
      </div>
      <div class="mb-3">
        <label class="form-label small mb-1">ETA in minutes (0 = unknown)</label>
        <input type="number" name="eta_mins" class="form-control form-control-sm" value="0" min="0">
      </div>
      <button class="btn btn-warning btn-sm">Begin maintenance</button>
    </form>
  {{end}}
</div></div>`))
