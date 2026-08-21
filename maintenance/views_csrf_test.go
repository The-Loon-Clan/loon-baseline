package maintenance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Every POST form on the maintenance admin page carries a CSRF token, IN BOTH
// STATES.
//
// The /end form only renders when maintenance is ACTIVE, which is why the live
// crawl of the running site never saw it and reported this page clean while
// that form had no token. The consequence was the bad kind of quiet: turning
// maintenance ON would have left the operator unable to turn it OFF from the
// UI — the button 403s, on a site already showing a maintenance page to
// everybody.
//
// A crawler sees the states a site happens to be in. A test can ask for both.

var postForm = regexp.MustCompile(`(?s)<form[^>]*method="post"[^>]*>(.*?)</form>`)

func renderAdmin(t *testing.T, c *Controller, token string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	g, _ := gin.CreateTestContext(rec)
	g.Request = httptest.NewRequest(http.MethodGet, "/admin/p/maintenance", nil)
	if token != "" {
		g.Set("csrf_token", token)
	}
	html, err := c.renderAdmin(g)
	if err != nil {
		t.Fatalf("renderAdmin: %v", err)
	}
	return string(html)
}

func assertEveryFormTokened(t *testing.T, page, token, state string) {
	t.Helper()
	forms := postForm.FindAllStringSubmatch(page, -1)
	if len(forms) == 0 {
		t.Fatalf("%s: no POST form rendered at all", state)
	}
	for _, f := range forms {
		if !strings.Contains(f[0], `name="_csrf"`) {
			t.Errorf("%s: a form has no CSRF field, so it 403s for every human:\n%s", state, f[0])
			continue
		}
		if token != "" && !strings.Contains(f[0], token) {
			t.Errorf("%s: the field is present but carries no token", state)
		}
	}
}

func TestBothMaintenanceStatesCarryATokenOnEveryForm(t *testing.T) {
	c := NewController(NewMock())

	// OFF: the "begin" form.
	assertEveryFormTokened(t, renderAdmin(t, c, "tok-off"), "tok-off", "inactive")

	// ON: the "end" form — the one a crawl of a healthy site can never reach.
	if err := c.Begin(context.Background(), "upgrading the database", 0); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	assertEveryFormTokened(t, renderAdmin(t, c, "tok-on"), "tok-on", "active")
}

// The field must be there even when the host published no token: an empty
// value is ignored, a missing field is a 403 nobody can diagnose.
func TestTheFieldIsPresentEvenWithNoToken(t *testing.T) {
	c := NewController(NewMock())
	assertEveryFormTokened(t, renderAdmin(t, c, ""), "", "inactive, no host token")
}
