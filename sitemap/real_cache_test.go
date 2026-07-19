package sitemap

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/the-loon-clan/loon-baseline/cache/memory"
)

// The generator must work against baseline's REAL cache impls, not only the
// test double. This is the claim a host relies on; the double proves nothing
// about it.
func TestGenerator_WorksWithBaselineMemoryCache(t *testing.T) {
	c := memory.New()
	g := New(Config{
		BaseURL:     "https://example.com",
		StaticPaths: []string{"/", "/browse"},
		TTL:         time.Hour,
	}, c, &stubSource{kind: "anime", n: 2})

	res, err := g.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate against cache/memory: %v", err)
	}
	if res.URLs != 4 {
		t.Errorf("URLs = %d, want 4", res.URLs)
	}
	body, ok, err := c.Get(context.Background(), "index")
	if err != nil || !ok {
		t.Fatalf("index not in cache: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(string(body), "sitemap-anime.xml") {
		t.Errorf("index does not list the anime sitemap:\n%s", body)
	}
}
