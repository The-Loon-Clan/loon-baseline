// Package sitemap generates sitemaps.org-conformant XML: one file per content
// kind, split at the 50,000-URL cap, plus the top-level index that ties them
// together, cached for serving.
//
// The split that makes this reusable: everything mechanical lives here — the
// XML shapes, the per-file cap and its paging, the index, lastmod formatting,
// the cache keys and the page-count bookkeeping the index needs to enumerate
// its children. Everything that knows what a site CONTAINS is a Source the host
// registers.
//
// That seam is the whole point. The service this was lifted from had
// regenAnime / regenManga / regenNews / regenWiki hard-coded, backed by a
// repository interface with ListAnimeForSitemap / CountMangaForSitemap and so
// on. Those are one site's domain model wearing a generic name; a framework
// package cannot have them. A Source is the same code with the nouns removed.
//
// The host owns scheduling and routing: call Generate on whatever loop it likes
// and serve the cache from wherever it likes. This package does not import
// loon/core, gin, or a job runtime — it is a generator, not a plugin.
package sitemap

import (
	"context"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/the-loon-clan/loon-baseline/cache"
)

// URLsPerFile is the sitemaps.org cap on URLs in a single sitemap file. Sources
// are paged against it, and a kind producing more simply yields more files.
const URLsPerFile = 50000

// Entry is one URL in a sitemap.
type Entry struct {
	// Loc is the absolute URL. Sources build it, because only the site knows
	// that an anime with id 42 lives at /anime/42 rather than /a/42 or
	// /titles/42-slug.
	Loc string
	// Lastmod is optional; a zero time omits the element rather than emitting
	// an epoch, which crawlers read as "modified in 1970".
	Lastmod time.Time
}

// Source is one kind of content a site publishes — anime, manga, news, wiki,
// products, whatever the site is. The host implements one per kind.
//
// Count and Page are separate so a large kind can be paged without holding
// every URL in memory: a 400k-title catalogue is 8 files, not one 400k-element
// slice.
type Source interface {
	// Kind names the sitemap file and its cache key: "anime" -> sitemap-anime.xml,
	// overflow pages -> sitemap-anime-2.xml. Must be stable and URL-safe.
	Kind() string
	// Count is the total number of URLs, used to compute the page count.
	Count(ctx context.Context) (int, error)
	// Page returns one page of entries. Implementations MUST order stably
	// (by id, not by a mutable column) or paging will skip and duplicate.
	Page(ctx context.Context, limit, offset int) ([]Entry, error)
}

// Config is the generator's settings.
type Config struct {
	// BaseURL is the site root without a trailing slash ("https://example.com").
	BaseURL string
	// StaticPaths are unsegmented public pages ("/", "/browse", "/tos") that
	// aren't backed by a Source. Emitted as the "static" kind with the
	// generation time as lastmod.
	StaticPaths []string
	// TTL is how long generated files stay in the cache. Set it LONGER than
	// the regeneration interval: equal and a slow run leaves the cache empty
	// between expiry and rewrite, which serves crawlers a 404 for the gap.
	TTL time.Duration
}

// Generator turns Sources into cached sitemap XML.
//
// The cache is baseline's own cache.Cache, so a host binds cache/memory or
// cache/redis and this package owns no client and no second abstraction for
// the same job.
type Generator struct {
	cfg     Config
	cache   cache.Cache
	sources []Source
}

// New returns a Generator. Sources may be empty — a site with only static
// pages still gets a valid index.
func New(cfg Config, c cache.Cache, sources ...Source) *Generator {
	return &Generator{cfg: cfg, cache: c, sources: sources}
}

// Result reports what a run produced, for the caller's job log.
type Result struct {
	Files int
	URLs  int
	// PerKind is files+URLs by kind, so an operator can see WHICH kind grew
	// or vanished rather than only that the total moved.
	PerKind map[string]KindResult
}

// KindResult is one kind's contribution.
type KindResult struct {
	Files int
	URLs  int
}

// Generate rebuilds every sitemap and the index.
//
// A failing Source fails the whole run rather than silently publishing a
// sitemap missing a content type: a half-written sitemap is worse than a stale
// one, because crawlers treat absence as delisting.
func (g *Generator) Generate(ctx context.Context) (Result, error) {
	res := Result{PerKind: map[string]KindResult{}}

	if len(g.cfg.StaticPaths) > 0 {
		files, urls, err := g.regenStatic(ctx)
		if err != nil {
			return res, fmt.Errorf("static: %w", err)
		}
		res.PerKind["static"] = KindResult{Files: files, URLs: urls}
		res.Files += files
		res.URLs += urls
	}

	for _, src := range g.sources {
		files, urls, err := g.regenSource(ctx, src)
		if err != nil {
			return res, fmt.Errorf("%s: %w", src.Kind(), err)
		}
		res.PerKind[src.Kind()] = KindResult{Files: files, URLs: urls}
		res.Files += files
		res.URLs += urls
	}

	if err := g.regenIndex(ctx); err != nil {
		return res, fmt.Errorf("index: %w", err)
	}
	return res, nil
}

func (g *Generator) regenStatic(ctx context.Context) (files, urls int, err error) {
	now := time.Now()
	set := make([]urlEntry, 0, len(g.cfg.StaticPaths))
	for _, p := range g.cfg.StaticPaths {
		set = append(set, urlEntry{Loc: g.cfg.BaseURL + p, Lastmod: formatLastmod(now)})
	}
	if err := g.writeSitemap(ctx, "static", set); err != nil {
		return 0, 0, err
	}
	if err := g.setPageCount(ctx, "static", 1); err != nil {
		return 1, len(set), err
	}
	return 1, len(set), nil
}

func (g *Generator) regenSource(ctx context.Context, src Source) (files, urls int, err error) {
	total, err := src.Count(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("count: %w", err)
	}
	for offset := 0; offset < total; offset += URLsPerFile {
		page, err := src.Page(ctx, URLsPerFile, offset)
		if err != nil {
			return files, urls, fmt.Errorf("page offset=%d: %w", offset, err)
		}
		set := make([]urlEntry, 0, len(page))
		for _, e := range page {
			set = append(set, urlEntry{Loc: e.Loc, Lastmod: formatLastmod(e.Lastmod)})
		}
		name := PageName(src.Kind(), files+1)
		if err := g.writeSitemap(ctx, name, set); err != nil {
			return files, urls, fmt.Errorf("write %s: %w", name, err)
		}
		files++
		urls += len(set)
	}
	// Persist the page count so the index can enumerate the sub-files without
	// re-counting the source.
	if err := g.setPageCount(ctx, src.Kind(), files); err != nil {
		return files, urls, fmt.Errorf("page count: %w", err)
	}
	return files, urls, nil
}

func (g *Generator) regenIndex(ctx context.Context) error {
	now := formatLastmod(time.Now())
	var entries []indexEntry

	kinds := make([]string, 0, len(g.sources)+1)
	if len(g.cfg.StaticPaths) > 0 {
		kinds = append(kinds, "static")
	}
	for _, s := range g.sources {
		kinds = append(kinds, s.Kind())
	}

	for _, kind := range kinds {
		n, err := g.getPageCount(ctx, kind)
		if err != nil {
			return fmt.Errorf("page count %s: %w", kind, err)
		}
		for page := 1; page <= n; page++ {
			entries = append(entries, indexEntry{
				Loc:     fmt.Sprintf("%s/sitemap-%s.xml", g.cfg.BaseURL, PageName(kind, page)),
				Lastmod: now,
			})
		}
	}

	wrap := struct {
		XMLName  xml.Name     `xml:"sitemapindex"`
		Xmlns    string       `xml:"xmlns,attr"`
		Sitemaps []indexEntry `xml:"sitemap"`
	}{Xmlns: xmlns, Sitemaps: entries}

	body, err := marshal(wrap)
	if err != nil {
		return err
	}
	return g.cache.Set(ctx, "index", body, g.cfg.TTL)
}

const xmlns = "http://www.sitemaps.org/schemas/sitemap/0.9"

type urlEntry struct {
	Loc     string `xml:"loc"`
	Lastmod string `xml:"lastmod,omitempty"`
}

type indexEntry struct {
	Loc     string `xml:"loc"`
	Lastmod string `xml:"lastmod,omitempty"`
}

func (g *Generator) writeSitemap(ctx context.Context, name string, urls []urlEntry) error {
	wrap := struct {
		XMLName xml.Name   `xml:"urlset"`
		Xmlns   string     `xml:"xmlns,attr"`
		URLs    []urlEntry `xml:"url"`
	}{Xmlns: xmlns, URLs: urls}

	body, err := marshal(wrap)
	if err != nil {
		return err
	}
	return g.cache.Set(ctx, name, body, g.cfg.TTL)
}

func marshal(v any) ([]byte, error) {
	b, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(xml.Header + string(b)), nil
}

// PageName is the cache key / filename suffix for a kind's Nth page. Page 1 is
// bare ("anime") and 2+ are suffixed ("anime-2"), matching the URL convention
// so /sitemap-anime.xml maps to key "anime" and /sitemap-anime-2.xml to
// "anime-2". Exported because the host's route handler needs the same mapping.
func PageName(kind string, page int) string {
	if page <= 1 {
		return kind
	}
	return fmt.Sprintf("%s-%d", kind, page)
}

// formatLastmod renders a time as the W3C Datetime form crawlers expect. A zero
// time returns "" so the omitempty tag drops the element — emitting the epoch
// would tell a crawler the page was last touched in 1970.
func formatLastmod(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

func (g *Generator) setPageCount(ctx context.Context, kind string, count int) error {
	return g.cache.Set(ctx, kind+":pagecount", []byte(fmt.Sprintf("%d", count)), g.cfg.TTL)
}

// getPageCount reads back how many files a kind wrote. A miss is 0 pages, not
// an error: a kind with no rows writes no files and belongs in no index.
func (g *Generator) getPageCount(ctx context.Context, kind string) (int, error) {
	body, ok, err := g.cache.Get(ctx, kind+":pagecount")
	if err != nil {
		return 0, err
	}
	if !ok || len(body) == 0 {
		return 0, nil
	}
	var n int
	if _, err := fmt.Sscanf(string(body), "%d", &n); err != nil {
		return 0, fmt.Errorf("page count %q is not a number: %w", body, err)
	}
	return n, nil
}
