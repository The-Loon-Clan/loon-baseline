package heartbeat

import (
	"bytes"
	"fmt"
	"html/template"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"
)

// onlineWindow is how recently an instance must have beaten to count as "online"
// — a few beat intervals of slack.
const onlineWindow = 60 * time.Second

// Views returns the admin "Services online" page (SlotAdminPage). Register on the
// Core after Boot; a view-system host mounts it at /admin/p/services.
func Views(store Store) ([]core.View, error) {
	return []core.View{{
		Slug: "services", Title: "Services", Slot: core.SlotAdminPage,
		Render: renderServices(store),
	}}, nil
}

func renderServices(store Store) func(*gin.Context) (template.HTML, error) {
	return func(c *gin.Context) (template.HTML, error) {
		hbs, err := store.Active(c.Request.Context(), onlineWindow)
		if err != nil {
			return "", err
		}
		now := time.Now()
		type row struct{ Kind, ServiceID, Uptime, LastSeen, Meta string }
		rows := make([]row, len(hbs))
		byKind := map[string]int{}
		for i, h := range hbs {
			byKind[h.Kind]++
			rows[i] = row{
				Kind: h.Kind, ServiceID: h.ServiceID, Meta: h.Meta,
				Uptime:   shortDur(now.Sub(h.StartedAt)),
				LastSeen: shortDur(now.Sub(h.LastSeen)) + " ago",
			}
		}
		var buf bytes.Buffer
		if err := servicesTmpl.Execute(&buf, map[string]any{
			"Rows": rows, "Total": len(rows), "ByKind": byKind,
			"WindowSecs": int(onlineWindow.Seconds()),
		}); err != nil {
			return "", err
		}
		return template.HTML(buf.String()), nil
	}
}

func shortDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

var servicesTmpl = template.Must(template.New("services").Parse(`
<div class="card"><div class="card-body">
  <h5 class="card-title mb-1">Services online</h5>
  <p class="text-muted small mb-3">Process instances seen in the last {{.WindowSecs}}s
    ({{.Total}} online{{range $k, $n := .ByKind}} &middot; {{$n}} {{$k}}{{end}}).</p>
  {{if .Rows}}
  <div class="table-responsive"><table class="table table-dark table-sm align-middle">
    <thead><tr><th>Kind</th><th>Instance</th><th class="text-end">Uptime</th><th class="text-end">Last seen</th><th>Meta</th></tr></thead>
    <tbody>
    {{range .Rows}}
      <tr>
        <td><span class="badge bg-info text-dark">{{.Kind}}</span></td>
        <td><code>{{.ServiceID}}</code></td>
        <td class="text-end small">{{.Uptime}}</td>
        <td class="text-end small">{{.LastSeen}}</td>
        <td class="small text-muted">{{.Meta}}</td>
      </tr>
    {{end}}
    </tbody>
  </table></div>
  {{else}}
  <div class="alert alert-secondary mb-0">No services have checked in recently.</div>
  {{end}}
</div></div>`))
