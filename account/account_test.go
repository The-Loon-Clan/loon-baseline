package account_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"

	"github.com/the-loon-clan/loon-baseline/account"
	"github.com/the-loon-clan/loon-baseline/authflow"
)

// The account.html template is parsed at runtime (Views → ParseFS), so a
// template edit that doesn't compile-fail can still break the page. Render it
// end to end and assert the profile fields + the self-contained layout class.
func TestAccountViewRenders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	u := &core.User{
		ID: 1, Username: "alice", Role: core.RoleAdmin,
		CreatedAt: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
	}
	views, err := account.Views(authflow.Flow{}, func(*gin.Context) (*core.User, bool) { return u, true })
	if err != nil {
		t.Fatalf("Views (template parse): %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("want 1 view, got %d", len(views))
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/p/account", nil)
	html, err := views[0].Render(c)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := string(html)
	// Values, the role name, and the self-contained grid class (which replaced
	// the Bootstrap dl.row that collapsed to stacked on minimal-CSS hosts).
	for _, want := range []string{"alice", "Admin", "2026-07-15", "Profile", "Change password", "acct-dl", "not set"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered account page missing %q", want)
		}
	}
	// "not set" only shows when Email is empty — sanity-check the branch by
	// rendering a user WITH an email.
	u2 := &core.User{ID: 2, Username: "bob", Email: "bob@example.com", Role: core.RoleUser, CreatedAt: u.CreatedAt}
	views2, _ := account.Views(authflow.Flow{}, func(*gin.Context) (*core.User, bool) { return u2, true })
	html2, err := views2[0].Render(c)
	if err != nil {
		t.Fatalf("render 2: %v", err)
	}
	if !strings.Contains(string(html2), "bob@example.com") {
		t.Errorf("email not rendered when set")
	}
}
