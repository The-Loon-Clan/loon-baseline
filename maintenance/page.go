package maintenance

import (
	"bytes"
	"html/template"
	"log"
)

// The 503 page is fully self-contained (inline CSS, no external assets) so it
// renders correctly even when static assets are also gated or the app is
// otherwise minimal. Dark theme to match the loon look.
const pageSrc = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Under maintenance</title>
<style>
  :root { color-scheme: dark; }
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
    background:#0d1117; color:#e6edf3; font:16px/1.5 system-ui,-apple-system,Segoe UI,Roboto,sans-serif; }
  .card { max-width:32rem; padding:2.5rem; text-align:center; }
  h1 { font-size:1.6rem; margin:0 0 .75rem; }
  p { color:#9aa7b4; margin:.4rem 0; }
  .reason { margin-top:1rem; padding:.75rem 1rem; background:#161b22; border:1px solid #30363d;
    border-radius:.5rem; color:#e6edf3; }
  .spin { width:2.2rem; height:2.2rem; margin:0 auto 1.25rem; border:3px solid #30363d;
    border-top-color:#3b82f6; border-radius:50%; animation:s 1s linear infinite; }
  @keyframes s { to { transform:rotate(360deg); } }
</style></head>
<body><div class="card">
  <div class="spin"></div>
  <h1>We&rsquo;ll be right back</h1>
  <p>The site is down for planned maintenance.</p>
  {{if .Reason}}<div class="reason">{{.Reason}}</div>{{end}}
  {{if gt .ETASecs 0}}<p>Estimated back in about {{etaMins .ETASecs}} minute(s).</p>{{end}}
</div></body></html>`

var pageTmpl = template.Must(template.New("maintenance").Funcs(template.FuncMap{
	"etaMins": func(secs int) int {
		if m := secs / 60; m > 0 {
			return m
		}
		return 1
	},
}).Parse(pageSrc))

// fallback503 is what a visitor gets if the template itself fails. Plain,
// and deliberately so: this renders while the site is ALREADY in trouble,
// and the one thing worse than an unstyled 503 is half of a styled one.
const fallback503 = `<!doctype html><meta charset="utf-8">` +
	`<title>Down for maintenance</title>` +
	`<body style="font:16px system-ui;padding:3rem;text-align:center">` +
	`<h1>Down for maintenance</h1><p>Back shortly.</p>`

// renderPage renders the 503 body for the given state.
func renderPage(s State) []byte { return renderWith(pageTmpl, s) }

// renderWith is renderPage with the template as an argument, so the
// fallback branch can be exercised. Without the seam the only way to test
// it is to break the real page, and a test that has to break the thing it
// guards is one nobody runs twice.
func renderWith(t *template.Template, s State) []byte {
	var buf bytes.Buffer
	// The error was discarded here, so a template failure returned a
	// TRUNCATED maintenance page -- the one page guaranteed to be seen by
	// everybody, rendered while nobody is watching logs for rendering bugs.
	// Executing into a buffer is what makes recovery possible at all:
	// nothing has reached the client yet, so there is still a choice.
	if err := t.Execute(&buf, s); err != nil {
		log.Printf("maintenance: 503 page failed to render (%v); serving the plain one", err)
		return []byte(fallback503)
	}
	return buf.Bytes()
}
