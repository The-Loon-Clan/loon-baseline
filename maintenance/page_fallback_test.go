package maintenance

import (
	"html/template"
	"strings"
	"testing"
)

// The 503 body is the one page guaranteed to be seen by everybody, and it
// renders while the site is already in trouble. Its Execute error used to be
// discarded, which meant a template failure served HALF a maintenance page.
func TestRenderFallsBackRatherThanServingHalfAPage(t *testing.T) {
	broken := template.Must(template.New("broken").Parse(
		`<p>this much renders</p>{{.NoSuchField}}<p>and this never does</p>`))

	out := string(renderWith(broken, State{}))
	if out != fallback503 {
		t.Fatalf("served a partial page instead of the fallback:\n%s", out)
	}
	if strings.Contains(out, "this much renders") {
		t.Error("the truncated output leaked into the response")
	}
}

// And the real page still renders through the same path.
func TestRenderUsesTheRealPageWhenItWorks(t *testing.T) {
	out := string(renderPage(State{}))
	if out == fallback503 {
		t.Fatal("the real 503 page fell back; the template no longer executes")
	}
	if !strings.Contains(strings.ToLower(out), "<!doctype html>") {
		t.Errorf("not a document:\n%s", out[:min(200, len(out))])
	}
}
