package sitemap

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ameNZB/loon-baseline/cache"
	"time"
)

// memCache satisfies baseline's cache.Cache and lets assertions read what was
// written. Kept rather than using cache/memory so tests can inspect the bytes
// and inject Set failures.
type memCache struct {
	m       map[string][]byte
	setErrs map[string]error
}

var _ cache.Cache = (*memCache)(nil)

func newMemCache() *memCache {
	return &memCache{m: map[string][]byte{}, setErrs: map[string]error{}}
}

func (c *memCache) Set(_ context.Context, name string, body []byte, _ time.Duration) error {
	if err := c.setErrs[name]; err != nil {
		return err
	}
	c.m[name] = body
	return nil
}

func (c *memCache) Get(_ context.Context, name string) ([]byte, bool, error) {
	v, ok := c.m[name]
	return v, ok, nil
}

func (c *memCache) Delete(_ context.Context, name string) error {
	delete(c.m, name)
	return nil
}

// stubSource yields n synthetic entries, paged.
type stubSource struct {
	kind     string
	n        int
	countErr error
	pageErr  error
	// pagesSeen records the (limit, offset) pairs asked for, so paging can be
	// asserted rather than assumed.
	pagesSeen [][2]int
}

func (s *stubSource) Kind() string { return s.kind }
func (s *stubSource) Count(context.Context) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.n, nil
}
func (s *stubSource) Page(_ context.Context, limit, offset int) ([]Entry, error) {
	if s.pageErr != nil {
		return nil, s.pageErr
	}
	s.pagesSeen = append(s.pagesSeen, [2]int{limit, offset})
	var out []Entry
	for i := offset; i < offset+limit && i < s.n; i++ {
		out = append(out, Entry{
			Loc:     fmt.Sprintf("https://example.com/%s/%d", s.kind, i),
			Lastmod: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		})
	}
	return out, nil
}

func newGen(c cache.Cache, sources ...Source) *Generator {
	return New(Config{
		BaseURL:     "https://example.com",
		StaticPaths: []string{"/", "/browse"},
		TTL:         time.Hour,
	}, c, sources...)
}

func TestGenerate(t *testing.T) {
	cache := newMemCache()
	anime := &stubSource{kind: "anime", n: 3}
	g := newGen(cache, anime)

	res, err := g.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if res.PerKind["static"].URLs != 2 {
		t.Errorf("static URLs = %d, want 2", res.PerKind["static"].URLs)
	}
	if res.PerKind["anime"].URLs != 3 {
		t.Errorf("anime URLs = %d, want 3", res.PerKind["anime"].URLs)
	}
	if res.URLs != 5 || res.Files != 2 {
		t.Errorf("totals = %d urls / %d files, want 5/2", res.URLs, res.Files)
	}

	// The XML must actually parse and carry the namespace crawlers key off.
	var parsed struct {
		XMLName xml.Name `xml:"urlset"`
		Xmlns   string   `xml:"xmlns,attr"`
		URLs    []struct {
			Loc     string `xml:"loc"`
			Lastmod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(cache.m["anime"], &parsed); err != nil {
		t.Fatalf("anime sitemap is not valid XML: %v", err)
	}
	if parsed.Xmlns != xmlns {
		t.Errorf("xmlns = %q, want %q", parsed.Xmlns, xmlns)
	}
	if len(parsed.URLs) != 3 {
		t.Fatalf("%d <url> elements, want 3", len(parsed.URLs))
	}
	if parsed.URLs[0].Loc != "https://example.com/anime/0" {
		t.Errorf("Loc = %q — the Source builds the URL; the generator must not rewrite it", parsed.URLs[0].Loc)
	}
	if parsed.URLs[0].Lastmod != "2026-01-02T03:04:05Z" {
		t.Errorf("Lastmod = %q, want W3C form", parsed.URLs[0].Lastmod)
	}
}

// A kind larger than the per-file cap must split, and the index must list every
// page — otherwise the overflow is generated and never discovered.
func TestGenerate_PagesAtTheCap(t *testing.T) {
	cache := newMemCache()
	// 2.5 pages' worth.
	big := &stubSource{kind: "anime", n: URLsPerFile*2 + 10}
	g := newGen(cache, big)

	res, err := g.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.PerKind["anime"].Files != 3 {
		t.Errorf("anime files = %d, want 3 (%d urls at a %d cap)", res.PerKind["anime"].Files, big.n, URLsPerFile)
	}
	for _, name := range []string{"anime", "anime-2", "anime-3"} {
		if len(cache.m[name]) == 0 {
			t.Errorf("%s was not written", name)
		}
	}
	// Paging must advance by exactly the cap, or pages overlap or skip.
	want := [][2]int{{URLsPerFile, 0}, {URLsPerFile, URLsPerFile}, {URLsPerFile, URLsPerFile * 2}}
	if len(big.pagesSeen) != len(want) {
		t.Fatalf("paged %v, want %v", big.pagesSeen, want)
	}
	for i := range want {
		if big.pagesSeen[i] != want[i] {
			t.Errorf("page %d: asked %v, want %v", i, big.pagesSeen[i], want[i])
		}
	}

	idx := string(cache.m["index"])
	for _, want := range []string{"sitemap-anime.xml", "sitemap-anime-2.xml", "sitemap-anime-3.xml"} {
		if !strings.Contains(idx, want) {
			t.Errorf("index is missing %s — the page exists but nothing links it", want)
		}
	}
}

func TestGenerate_IndexListsEveryKind(t *testing.T) {
	cache := newMemCache()
	g := newGen(cache, &stubSource{kind: "anime", n: 1}, &stubSource{kind: "news", n: 1})

	if _, err := g.Generate(context.Background()); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var parsed struct {
		XMLName  xml.Name `xml:"sitemapindex"`
		Sitemaps []struct {
			Loc string `xml:"loc"`
		} `xml:"sitemap"`
	}
	if err := xml.Unmarshal(cache.m["index"], &parsed); err != nil {
		t.Fatalf("index is not valid XML: %v", err)
	}
	if len(parsed.Sitemaps) != 3 { // static + anime + news
		t.Fatalf("%d entries, want 3 (static, anime, news)", len(parsed.Sitemaps))
	}
	for _, s := range parsed.Sitemaps {
		if !strings.HasPrefix(s.Loc, "https://example.com/sitemap-") {
			t.Errorf("index Loc %q is not an absolute sitemap URL", s.Loc)
		}
	}
}

// A failing source must fail the run. Publishing a sitemap that silently omits
// a content type is worse than serving a stale one: crawlers read absence as
// delisting.
func TestGenerate_SourceFailureAbortsTheRun(t *testing.T) {
	t.Run("count error", func(t *testing.T) {
		g := newGen(newMemCache(), &stubSource{kind: "anime", countErr: errors.New("db down")})
		if _, err := g.Generate(context.Background()); err == nil {
			t.Error("Generate returned nil with a failing Count — the run must not publish a partial sitemap")
		}
	})
	t.Run("page error", func(t *testing.T) {
		g := newGen(newMemCache(), &stubSource{kind: "anime", n: 5, pageErr: errors.New("db down")})
		if _, err := g.Generate(context.Background()); err == nil {
			t.Error("Generate returned nil with a failing Page")
		}
	})
	t.Run("error names the kind", func(t *testing.T) {
		g := newGen(newMemCache(), &stubSource{kind: "manga", countErr: errors.New("db down")})
		_, err := g.Generate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "manga") {
			t.Errorf("err = %v — must name the kind, or an operator cannot tell which source broke", err)
		}
	})
}

// An empty kind must still be valid: a site with no news yet gets an empty
// news sitemap, not a missing file the index points at.
func TestGenerate_EmptySourceIsValid(t *testing.T) {
	cache := newMemCache()
	g := newGen(cache, &stubSource{kind: "news", n: 0})

	res, err := g.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.PerKind["news"].Files != 0 {
		t.Errorf("news files = %d, want 0 — no rows means no file to link", res.PerKind["news"].Files)
	}
	idx := string(cache.m["index"])
	if strings.Contains(idx, "sitemap-news.xml") {
		t.Error("index links sitemap-news.xml but no such file was written — a 404 for every crawler that follows it")
	}
}

func TestGenerate_NoSourcesStillProducesAnIndex(t *testing.T) {
	cache := newMemCache()
	g := newGen(cache)
	if _, err := g.Generate(context.Background()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(cache.m["index"]) == 0 {
		t.Error("no index written for a static-only site")
	}
}

func TestPageName(t *testing.T) {
	cases := map[int]string{1: "anime", 2: "anime-2", 3: "anime-3"}
	for page, want := range cases {
		if got := PageName("anime", page); got != want {
			t.Errorf("PageName(anime, %d) = %q, want %q", page, got, want)
		}
	}
	// Page 0 shouldn't happen, but must not produce "anime-0" and desync the
	// route mapping.
	if got := PageName("anime", 0); got != "anime" {
		t.Errorf("PageName(anime, 0) = %q, want %q", got, "anime")
	}
}

func TestFormatLastmod(t *testing.T) {
	if got := formatLastmod(time.Time{}); got != "" {
		t.Errorf("zero time = %q, want \"\" — omitempty must drop the element rather than claim 1970", got)
	}
	got := formatLastmod(time.Date(2026, 7, 15, 12, 0, 0, 0, time.FixedZone("PDT", -7*3600)))
	if got != "2026-07-15T19:00:00Z" {
		t.Errorf("lastmod = %q, want the UTC W3C form", got)
	}
}
