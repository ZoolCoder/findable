package check

import (
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/site"
	"github.com/zoolcoder/findable/internal/sitetest"
)

const (
	home  = "index.html"
	about = "about/index.html"
)

// load writes the fixture with edit applied and reads it back.
func load(t *testing.T, edit func(map[string]string)) site.Site {
	t.Helper()
	files := sitetest.Files()
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := url.Parse(sitetest.Base)
	s, err := site.LoadDir(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func target() config.Target {
	return config.Target{Name: "example", BaseURL: sitetest.Base, LLMsTxt: &config.LLMsTxt{Title: "Example"}}
}

// replace edits one fixture file.
func replace(file, old, new string) func(map[string]string) {
	return func(f map[string]string) {
		if !strings.Contains(f[file], old) {
			panic(file + " lacks " + old)
		}
		f[file] = strings.Replace(f[file], old, new, 1)
	}
}

func head(file, tag string) func(map[string]string) { return replace(file, "</head>", tag+"</head>") }

func ld(file, json string) func(map[string]string) {
	return head(file, `<script type="application/ld+json">`+json+`</script>`)
}

const job = `{"@context":"https://schema.org","@type":"JobPosting","title":"Welder","description":"Weld.","datePosted":"2026-09-01",
"hiringOrganization":{"@type":"Organization","name":"Example"},"jobLocation":{"@type":"Place"},
"baseSalary":{"@type":"MonetaryAmount"},"employmentType":"FULL_TIME","validThrough":"2026-12-31"}`

func TestChecks(t *testing.T) {
	now = func() time.Time { return time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = time.Now })

	for _, c := range []struct {
		name   string
		edit   func(map[string]string)
		target func(*config.Target)
		want   []string
	}{
		{name: "clean fixture"},
		{name: "title missing", edit: replace(home, "<title>Example widgets for every workshop</title>", ""), want: []string{"meta.title.missing"}},
		{name: "title too long", edit: replace(home, "<title>Example", "<title>"+strings.Repeat("Long ", 12)), want: []string{"meta.title.length"}},
		{name: "duplicate title", edit: replace(about, "<title>About Example and the people behind it", "<title>Example widgets for every workshop"), want: []string{"meta.title.duplicate"}},
		{name: "no h1, no lang, image without alt", edit: func(f map[string]string) {
			replace(home, `<html lang="en">`, "<html>")(f)
			replace(home, "<h1>Example widgets for every workshop</h1>", `<img src="a.png">`)(f)
		}, want: []string{"img.alt.missing", "meta.h1.missing", "meta.lang.missing"}},
		{name: "canonical on another host", edit: replace(about, `href="https://example.com/about"`, `href="https://other.example/about"`),
			want: []string{"meta.canonical.host", "sitemap.url.canonical"}},
		{name: "relative canonical", edit: replace(about, `href="https://example.com/about"`, `href="/about"`), want: []string{"meta.canonical.relative"}},
		{name: "stray noindex", edit: head(about, `<meta name="robots" content="noindex">`), want: []string{"index.noindex", "index.noindex.listed"}},
		{name: "intended noindex still in the sitemap", edit: head(about, `<meta name="robots" content="noindex">`),
			target: func(t *config.Target) { t.Noindex = []string{"/about"} }, want: []string{"index.noindex.listed"}},
		{name: "broken link", edit: replace(home, "</main>", `<a href="/nope">x</a></main>`), want: []string{"links.broken"}},
		{name: "orphan", edit: replace(home, `<a href="/about">/about</a>`, ""), want: []string{"links.orphan"}},
		{name: "no sitemap", edit: func(f map[string]string) { delete(f, "sitemap.xml") }, want: []string{"sitemap.missing"}},
		{name: "sitemap lists a missing page and another host", edit: replace("sitemap.xml", "</urlset>",
			"<url><loc>https://example.com/gone</loc></url><url><loc>https://other.example/x</loc></url></urlset>"),
			want: []string{"sitemap.url.host", "sitemap.url.unknown"}},
		{name: "page left out of the sitemap", edit: replace("sitemap.xml", "<url><loc>https://example.com/about</loc></url>", ""), want: []string{"sitemap.page.missing"}},
		{name: "robots blocks Googlebot", edit: func(f map[string]string) {
			f["robots.txt"] = "User-agent: Googlebot\nDisallow: /\n"
		}, want: []string{"robots.blocks.all", "robots.sitemap"}},
		{name: "no robots.txt", edit: func(f map[string]string) { delete(f, "robots.txt") }, want: []string{"robots.missing"}},
		{name: "AI search crawler blocked against policy", edit: func(f map[string]string) {
			f["robots.txt"] = sitetest.Robots + "\nUser-agent: OAI-SearchBot\nDisallow: /\n"
		}, want: []string{"ai.crawler.blocked"}},
		{name: "training crawlers let in against policy", target: func(t *config.Target) { t.AICrawlers.Training = "block" }, want: []string{"ai.crawler.allowed"}},
		{name: "no llms.txt and none configured", target: func(t *config.Target) { t.LLMsTxt = nil }, want: []string{"ai.llms.missing"}},
		{name: "llms.txt malformed with a dead link", edit: func(f map[string]string) {
			f["llms.txt"] = "Example\n\n- [About](https://example.com/about)\n- [Gone](/gone)\n"
		}, want: []string{"ai.llms.format", "ai.llms.link"}},
		{name: "hreflang missing", target: func(t *config.Target) { t.Locales = []string{"en", "ro"} }, want: []string{"hreflang.missing"}},
		{name: "thin content", target: func(t *config.Target) { t.MinWords = 1000 }, want: []string{"content.thin"}},
		{name: "invalid JSON-LD", edit: ld(home, "{broken"), want: []string{"schema.invalid.json"}},
		{name: "expected type missing", target: func(t *config.Target) {
			t.Schema = []config.SchemaRule{{Path: "/about", Types: []string{"BreadcrumbList"}}}
		}, want: []string{"schema.expected"}},
		{name: "valid JobPosting", edit: ld(about, job)},
		{name: "expired JobPosting", edit: ld(about, strings.Replace(job, "2026-12-31", "2026-10-01", 1)), want: []string{"schema.jobposting.expired"}},
		{name: "JobPosting short of fields", edit: ld(about, `{"@type":"JobPosting","title":"Welder","hiringOrganization":{"@type":"Organization"}}`),
			want: []string{"schema.jobposting.recommended", "schema.jobposting.required"}},
		{name: "JobPosting on a listing", edit: ld(about, "["+job+","+job+"]"), want: []string{"schema.jobposting.listing"}},
		{name: "breadcrumb out of order", edit: ld(about, `{"@type":"BreadcrumbList","itemListElement":[{"position":2,"name":"Home","item":"/"}]}`),
			want: []string{"schema.breadcrumb.position"}},
		{name: "branch name matches the business", edit: ld(about, `{"@type":"EmploymentAgency","name":"Example — Leeds","address":"1 Road","telephone":"+44 1 2","url":"/"}`),
			target: func(t *config.Target) { t.Business = &config.Business{Name: "Example", Phones: []string{"+44 (1) 2"}} }},
		{name: "NAP mismatch", edit: ld(about, `{"@type":"EmploymentAgency","name":"Examples Ltd","address":"1 Road","telephone":"+44 9","url":"/"}`),
			target: func(t *config.Target) { t.Business = &config.Business{Name: "Example", Phones: []string{"+44 1 2"}} },
			want:   []string{"local.nap.mismatch"}},
		{name: "ignored checks", edit: replace(home, "<title>Example widgets for every workshop</title>", ""),
			target: func(t *config.Target) { t.Ignore = []string{"meta.title"} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			tg := target()
			if c.target != nil {
				c.target(&tg)
			}
			var got []string
			for _, f := range Run(load(t, c.edit), tg) {
				got = append(got, f.Check)
			}
			slices.Sort(got)
			got = slices.Compact(got)
			if !slices.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestFindingsMergeAndRank(t *testing.T) {
	s := load(t, func(f map[string]string) {
		replace(about, "<title>About Example and the people behind it", "<title>Example widgets for every workshop")(f)
		replace(home, "</main>", `<a href="/nope">x</a></main>`)(f)
	})
	got := Run(s, target())
	if len(got) != 2 || got[0].Check != "links.broken" || got[1].Check != "meta.title.duplicate" {
		t.Fatalf("order %+v", got)
	}
	if !slices.Equal(got[1].Pages, []string{"/", "/about"}) || got[1].Where() != "/ (+1 more)" {
		t.Fatalf("merge %+v", got[1])
	}
	if !reflect.DeepEqual(Run(s, target()), got) {
		t.Fatal("two runs differ")
	}
}
