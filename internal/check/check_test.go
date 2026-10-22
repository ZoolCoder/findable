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

const (
	homeDesc  = "Example makes widgets for workshops of every size, shipped across Europe."
	aboutDesc = "Who makes Example widgets, where the workshop is, and how to reach us."
)

func metaDesc(desc string) string { return `<meta name="description" content="` + desc + `">` }

func alts(pairs ...string) string {
	var b strings.Builder
	for i := 0; i < len(pairs); i += 2 {
		b.WriteString(`<link rel="alternate" hreflang="` + pairs[i] + `" href="` + sitetest.Base + pairs[i+1] + `">`)
	}
	return b.String()
}

// i18n adds a /ro page and gives every page its alternates; over replaces
// the alternates of one file.
func i18n(over map[string]string) func(map[string]string) {
	const ro = "ro/index.html"
	return func(f map[string]string) {
		f[ro] = sitetest.Page("/ro", "Exemplu widgeturi pentru fiecare atelier", "Exemplu face widgeturi pentru ateliere de toate dimensiunile, livrate in Europa.", "/")
		replace("sitemap.xml", "</urlset>", "<url><loc>"+sitetest.Base+"/ro</loc></url></urlset>")(f)
		replace(home, "</main>", `<a href="/ro">ro</a></main>`)(f)
		for file, a := range map[string]string{
			home:  alts("en", "/", "ro", "/ro", "x-default", "/"),
			ro:    alts("ro", "/ro", "en", "/", "x-default", "/"),
			about: alts("en", "/about", "x-default", "/about"),
		} {
			if o, ok := over[file]; ok {
				a = o
			}
			head(file, a)(f)
		}
	}
}

var enRo = func(t *config.Target) { t.Locales = []string{"en", "ro"} }

// relativeLinks adds a post whose links only resolve against the
// directory its index.html is served at.
func relativeLinks(f map[string]string) {
	const desc = "A post about widgets, with links relative to the directory it is served at."
	f["blog/post/index.html"] = sitetest.Page("/blog/post", "Blog post about widgets today", desc, "other", "../x")
	f["blog/post/other/index.html"] = sitetest.Page("/blog/post/other", "Another post about widgets", desc+" Other.", "/blog/post")
	f["blog/x/index.html"] = sitetest.Page("/blog/x", "A page next to the blog post", desc+" X.", "/blog/post")
	replace(home, "</main>", `<a href="/blog/post">post</a></main>`)(f)
	replace("sitemap.xml", "</urlset>", "<url><loc>"+sitetest.Base+"/blog/post</loc></url><url><loc>"+sitetest.Base+"/blog/post/other</loc></url><url><loc>"+sitetest.Base+"/blog/x</loc></url></urlset>")(f)
}

// moveAbout serves the about page at /story, a path nothing recognises.
func moveAbout(f map[string]string) {
	for _, file := range []string{home, about, "sitemap.xml"} {
		f[file] = strings.ReplaceAll(f[file], "/about", "/story")
	}
	f["story/index.html"] = f[about]
	delete(f, about)
}

func noOrganization(f map[string]string) {
	replace(home, `"@type":"Organization"`, `"@type":"Thing"`)(f)
	replace(about, `"@type":"Organization"`, `"@type":"Thing"`)(f)
}

func TestChecks(t *testing.T) {
	now = func() time.Time { return time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = time.Now })

	for _, c := range []struct {
		name   string
		edit   func(map[string]string)
		target func(*config.Target)
		want   []string
		pages  map[string][]string
	}{
		{name: "clean fixture"},
		{name: "title missing", edit: replace(home, "<title>Example widgets for every workshop</title>", ""), want: []string{"meta.title.missing"}},
		{name: "title too long", edit: replace(home, "<title>Example", "<title>"+strings.Repeat("Long ", 12)), want: []string{"meta.title.length"}},
		{name: "duplicate title", edit: replace(about, "<title>About Example and the people behind it", "<title>Example widgets for every workshop"), want: []string{"meta.title.duplicate"}, pages: map[string][]string{"meta.title.duplicate": {"/", "/about"}}},
		{name: "no h1, no lang, image without alt", edit: func(f map[string]string) {
			replace(home, `<html lang="en">`, "<html>")(f)
			replace(home, "<h1>Example widgets for every workshop</h1>", `<img src="a.png">`)(f)
		}, want: []string{"img.alt.missing", "meta.h1.missing", "meta.lang.missing"}},
		{name: "canonical on another host", edit: replace(about, `href="https://example.com/about"`, `href="https://other.example/about"`),
			want: []string{"meta.canonical.host", "sitemap.url.canonical"}},
		{name: "relative canonical", edit: replace(about, `href="https://example.com/about"`, `href="/about"`), want: []string{"meta.canonical.relative"}},
		{name: "canonical to a path with no page", edit: replace(about, `href="https://example.com/about"`, `href="https://example.com/nowhere"`),
			want: []string{"meta.canonical.target", "sitemap.url.canonical"}},
		{name: "stray noindex", edit: head(about, `<meta name="robots" content="noindex">`), want: []string{"index.noindex", "index.noindex.listed"}},
		{name: "intended noindex still in the sitemap", edit: head(about, `<meta name="robots" content="noindex">`),
			target: func(t *config.Target) { t.Noindex = []string{"/about"} }, want: []string{"index.noindex.listed"}},
		{name: "article without author or dates", edit: ld(home, `{"@type":"BlogPosting","headline":"Widgets"}`), want: []string{"schema.article.author", "schema.article.dates"}},
		{name: "article with a non-ISO date", edit: ld(home, `{"@type":"Article","author":{"@type":"Person","name":"Ana"},"datePublished":"23/10/2026","dateModified":"2026-10-23"}`), want: []string{"schema.article.dates"}},
		{name: "complete article", edit: ld(home, `{"@type":"NewsArticle","author":[{"@type":"Organization","name":"Example"}],"datePublished":"2026-10-23T09:00:00+02:00","dateModified":"2026-10-23"}`)},
		{name: "about page names nobody", edit: replace(about, ","+sitetest.Person, ""), want: []string{"about.people.missing"}},
		{name: "about page names a founder", edit: func(f map[string]string) {
			replace(about, ","+sitetest.Person, "")(f)
			replace(about, `"sameAs":["https://github.com/example"]}`, `"sameAs":["https://github.com/example"],"founder":{"@type":"Person","name":"Ana Example"}}`)(f)
		}},
		{name: "about page under a locale prefix", edit: func(f map[string]string) {
			moveAbout(f)
			f["ro/about/index.html"] = strings.ReplaceAll(f["story/index.html"], "/story", "/ro/about")
			replace(home, "</main>", `<a href="/ro/about">ro</a></main>`)(f)
			replace("sitemap.xml", "</urlset>", "<url><loc>"+sitetest.Base+"/ro/about</loc></url></urlset>")(f)
		}, target: enRo, want: []string{"hreflang.missing", "meta.description.duplicate", "meta.title.duplicate"}},
		{name: "about page found by AboutPage type", edit: func(f map[string]string) {
			moveAbout(f)
			ld("story/index.html", `{"@type":"AboutPage","name":"Story"}`)(f)
		}},
		{name: "no about page", edit: moveAbout, want: []string{"about.missing"}},
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
		{name: "llms.txt present, no llms-full.txt, full not set", edit: func(f map[string]string) { f["llms.txt"] = "# Example\n" }, want: []string{"ai.llms.full.missing"}},
		{name: "llms.txt present, full set", target: func(t *config.Target) { t.LLMsTxt.Full = true }, edit: func(f map[string]string) { f["llms.txt"] = "# Example\n" }},
		{name: "llms.txt malformed with a dead link", edit: func(f map[string]string) {
			f["llms.txt"] = "Example\n\n- [About](https://example.com/about)\n- [Gone](/gone)\n"
		}, want: []string{"ai.llms.format", "ai.llms.full.missing", "ai.llms.link"}},
		{name: "relative links in a directory page", edit: relativeLinks},
		{name: "title under the minimum", edit: replace(home, "<title>Example widgets for every workshop</title>", "<title>Example</title>"), want: []string{"meta.title.length"}},
		{name: "short identical titles", edit: func(f map[string]string) {
			replace(home, "<title>Example widgets for every workshop", "<title>Home | Acme")(f)
			replace(about, "<title>About Example and the people behind it", "<title>Home | Acme")(f)
		}, want: []string{"meta.title.duplicate", "meta.title.length"}},
		{name: "description missing", edit: replace(home, metaDesc(homeDesc), ""), want: []string{"meta.description.missing"}},
		{name: "description too long", edit: replace(home, metaDesc(homeDesc), metaDesc(strings.Repeat("x", 161))), want: []string{"meta.description.length"}},
		{name: "description too short", edit: replace(home, metaDesc(homeDesc), metaDesc(strings.Repeat("x", 49))), want: []string{"meta.description.length"}},
		{name: "duplicate description", edit: replace(about, metaDesc(aboutDesc), metaDesc(homeDesc)), want: []string{"meta.description.duplicate"}},
		{name: "short identical descriptions", edit: func(f map[string]string) {
			replace(home, metaDesc(homeDesc), metaDesc("Acme staffing for everyone"))(f)
			replace(about, metaDesc(aboutDesc), metaDesc("Acme staffing for everyone"))(f)
		}, want: []string{"meta.description.duplicate", "meta.description.length"}},
		{name: "canonical missing", edit: replace(about, `<link rel="canonical" href="https://example.com/about">`, ""), want: []string{"meta.canonical.missing"}},
		{name: "canonical does not parse", edit: replace(about, `href="https://example.com/about"`, `href="http://[::1"`),
			want: []string{"meta.canonical.host", "sitemap.url.canonical"}},
		{name: "no og image", edit: replace(home, `<meta property="og:image" content="https://example.com/og.jpg">`, ""), want: []string{"social.og.missing"}},
		{name: "two h1", edit: replace(home, "</main>", "<h1>Two</h1></main>"), want: []string{"meta.h1.multiple"}},
		{name: "sitemap lists a PDF", edit: replace("sitemap.xml", "</urlset>", "<url><loc>https://example.com/files/brochure.pdf</loc></url></urlset>")},
		{name: "sitemap does not parse", edit: func(f map[string]string) { f["sitemap.xml"] = "<nope" }, want: []string{"sitemap.invalid", "sitemap.page.missing"}},
		{name: "hreflang pair is clean", edit: i18n(nil), target: enRo},
		{name: "hreflang alternate does not link back", edit: i18n(map[string]string{"ro/index.html": alts("ro", "/ro", "x-default", "/")}), target: enRo,
			want: []string{"hreflang.reciprocal"}, pages: map[string][]string{"hreflang.reciprocal": {"/"}}},
		{name: "hreflang without x-default", edit: i18n(map[string]string{"ro/index.html": alts("ro", "/ro", "en", "/")}), target: enRo,
			want: []string{"hreflang.xdefault"}},
		{name: "hreflang omits the page itself", edit: i18n(map[string]string{about: alts("x-default", "/about")}), target: enRo,
			want: []string{"hreflang.self"}, pages: map[string][]string{"hreflang.self": {"/about"}}},
		{name: "hreflang code is not a language", edit: i18n(map[string]string{home: alts("en", "/", "ro", "/ro", "english", "/", "x-default", "/")}), target: enRo,
			want: []string{"hreflang.invalid"}},
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
		{name: "breadcrumb with no items", edit: ld(about, `{"@type":"BreadcrumbList","itemListElement":[]}`), want: []string{"schema.breadcrumb.items"}},
		{name: "breadcrumb without a name", edit: ld(about, `{"@type":"BreadcrumbList","itemListElement":[{"position":1}]}`), want: []string{"schema.breadcrumb.items"},
			pages: map[string][]string{"schema.breadcrumb.items": {"/about"}}},
		{name: "breadcrumb named by its item", edit: ld(about, `{"@type":"BreadcrumbList","itemListElement":[{"position":1,"item":{"name":"H"}}]}`)},
		{name: "breadcrumb first crumb without a URL", edit: ld(about, `{"@type":"BreadcrumbList","itemListElement":[{"position":1,"name":"H"},{"position":2,"name":"A"}]}`),
			want: []string{"schema.breadcrumb.items"}},
		{name: "@type as an array", edit: ld(about, `{"@type":["Organization","LocalBusiness"],"name":"Example","address":"1 Road","telephone":"+44 1 2","url":"/","logo":"/l.png","sameAs":["https://github.com/example"]}`),
			target: func(t *config.Target) { t.Business = &config.Business{Name: "Example", Phones: []string{"+44 1 2"}} }},
		{name: "no business markup", edit: noOrganization,
			target: func(t *config.Target) { t.Business = &config.Business{Name: "Example"} }, want: []string{"local.schema.missing"}},
		{name: "breadcrumb out of order", edit: ld(about, `{"@type":"BreadcrumbList","itemListElement":[{"position":2,"name":"Home","item":"/"}]}`),
			want: []string{"schema.breadcrumb.position"}},
		{name: "branch name matches the business", edit: ld(about, `{"@type":"EmploymentAgency","name":"Example — Leeds","address":"1 Road","telephone":"+44 1 2","url":"/","logo":"/l.png","sameAs":["https://github.com/example"]}`),
			target: func(t *config.Target) { t.Business = &config.Business{Name: "Example", Phones: []string{"+44 (1) 2"}} }},
		{name: "NAP mismatch", edit: ld(about, `{"@type":"EmploymentAgency","name":"Examples Ltd","address":"1 Road","telephone":"+44 9","url":"/","logo":"/l.png","sameAs":["https://github.com/example"]}`),
			target: func(t *config.Target) { t.Business = &config.Business{Name: "Example", Phones: []string{"+44 1 2"}} },
			want:   []string{"local.nap.mismatch"}},
		{name: "Organization without a logo", edit: ld(about, `{"@type":"Organization","name":"Example","url":"/","sameAs":["https://github.com/example"]}`),
			want: []string{"schema.organization.fields"}},
		{name: "LocalBusiness without address or phone", edit: ld(about, `{"@type":"LocalBusiness","name":"Example"}`),
			want: []string{"schema.localbusiness.fields", "schema.organization.fields", "schema.organization.sameas"}},
		{name: "local business subtype without sameAs", edit: ld(about, `{"@type":"EmploymentAgency","name":"Example","address":"1 Road","telephone":"1","url":"/","logo":"/l.png"}`),
			want: []string{"schema.organization.sameas"}},
		{name: "Organization and LocalBusiness without sameAs", edit: ld(about, `{"@type":["Organization","LocalBusiness"],"name":"Example","address":"1 Road","telephone":"1","url":"/","logo":"/l.png"}`),
			want: []string{"schema.organization.sameas"}},
		{name: "article author as an @id reference", edit: ld(home, `{"@graph":[{"@type":"Article","author":{"@id":"#ana"},"datePublished":"2026-10-23","dateModified":"2026-10-23"},{"@type":"Person","@id":"#ana","name":"Ana"}]}`)},
		{name: "article author reference with no node", edit: ld(home, `{"@type":"Article","author":{"@id":"#ana"},"datePublished":"2026-10-23","dateModified":"2026-10-23"}`),
			want: []string{"schema.article.author"}},
		{name: "article author as a string", edit: ld(home, `{"@type":"Article","author":"Ana Example","datePublished":"2026-10-23","dateModified":"2026-10-23"}`)},
		{name: "article dates without seconds", edit: ld(home, `{"@type":"Article","author":"Ana","datePublished":"2026-10-23T09:00+02:00","dateModified":"2026-10-23T09:00Z"}`)},
		{name: "about page names its mainEntity person", edit: func(f map[string]string) {
			replace(about, ","+sitetest.Person, ","+`{"@type":"AboutPage","mainEntity":{"@type":"Person","name":"Ana"}}`)(f)
		}},
		{name: "about page mainEntity is an organization", edit: func(f map[string]string) {
			replace(about, ","+sitetest.Person, ","+`{"@type":"AboutPage","mainEntity":{"@type":"Organization","name":"Example"}}`)(f)
		}, want: []string{"about.people.missing"}},
		{name: "breadcrumb with one item as an object", edit: ld(about, `{"@type":"BreadcrumbList","itemListElement":{"@type":"ListItem","position":1,"name":"Home"}}`)},
		{name: "Organization without sameAs", edit: replace(home, `,"sameAs":["https://github.com/example"]`, ""), want: []string{"schema.organization.sameas"}},
		{name: "brand named in titles and WebSite", target: func(t *config.Target) { t.BrandName = "Example" }},
		{name: "brand from business.name, spelled apart", target: func(t *config.Target) { t.Business = &config.Business{Name: "Exam ple"} },
			want: []string{"local.nap.mismatch"}},
		{name: "brand missing", target: func(t *config.Target) { t.BrandName = "Acme" }, want: []string{"meta.title.brand", "schema.website.name"}},
		{name: "query in title, h1 and description", target: func(t *config.Target) {
			t.Queries = []config.Query{{Query: "Widgets for workshops", Path: "/"}}
		}},
		{name: "query missing and page missing", target: func(t *config.Target) {
			t.Queries = []config.Query{{Query: "AI integration", Path: "/about"}, {Query: "widgets", Path: "/shop"}}
		}, want: []string{"content.query", "content.query.page"}},
		{name: "ignored checks", edit: replace(home, "<title>Example widgets for every workshop</title>", ""),
			target: func(t *config.Target) { t.Ignore = []string{"meta.title"} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			tg := target()
			if c.target != nil {
				c.target(&tg)
			}
			var got []string
			pages := map[string][]string{}
			for _, f := range Run(load(t, c.edit), tg) {
				got = append(got, f.Check)
				pages[f.Check] = f.Pages
			}
			for id, want := range c.pages {
				if !slices.Equal(pages[id], want) {
					t.Errorf("%s pages = %v, want %v", id, pages[id], want)
				}
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

// TestRedirectFindings checks a site that names its pages without the
// trailing slash it serves them at, behind a 307: the sitemap, canonicals,
// alternates and links all point at the redirect.
func TestRedirectFindings(t *testing.T) {
	base, _ := url.Parse(sitetest.Base)
	page := func(path, served string, hops ...int) site.Page {
		return site.Page{Path: path, URL: sitetest.Base + served, Redirects: hops, Title: "Example page for " + path,
			Canonical: sitetest.Base + path, H1: []string{"x"}, Links: []string{"/about", "/about/#team"},
			Hreflang: map[string]string{"en": sitetest.Base + path}}
	}
	s := site.Site{Base: base, Live: true, Pages: []site.Page{page("/", "/"), page("/about", "/about/", 307)},
		Sitemap: site.Sitemap{Found: true, URLs: []string{sitetest.Base, sitetest.Base + "/about"}},
		Hops: map[string][]site.Hop{
			"/about": {{URL: sitetest.Base + "/about", Code: 307}, {URL: sitetest.Base + "/about/", Code: 200}},
			"/old":   {{URL: sitetest.Base + "/old", Code: 301}, {URL: sitetest.Base + "/mid", Code: 302}, {URL: sitetest.Base + "/", Code: 200}},
		}}
	got := map[string]Finding{}
	for _, f := range Run(s, target()) {
		got[f.Check] = f
	}
	for id, sev := range map[string]Severity{"sitemap.url.redirect": Error, "meta.canonical.redirect": Error, "redirect.temporary": Warn, "links.redirect": Info} {
		if f, ok := got[id]; !ok || f.Severity != sev || !slices.Contains(f.Pages, "/about") && id != "links.redirect" {
			t.Errorf("%s: %+v", id, f)
		}
	}
	if f := got["sitemap.url.redirect"]; !strings.Contains(f.Fix, "e.g. https://example.com/about/, not https://example.com/about (307)") {
		t.Errorf("fix %q", f.Fix)
	}
	if !slices.Equal(got["links.redirect"].Pages, []string{"/", "/about"}) {
		t.Errorf("links.redirect pages %v", got["links.redirect"].Pages)
	}
	if _, ok := got["hreflang.redirect"]; !ok {
		t.Error("no hreflang.redirect")
	}
	if f := got["redirect.chain"]; f.Severity != Warn || !slices.Equal(f.Pages, []string{"/old"}) ||
		!strings.Contains(f.Fix, "Redirect each URL straight to its final one, e.g. https://example.com/old straight to https://example.com/; it now goes https://example.com/old (301), then https://example.com/mid (302), then https://example.com/ (200).") {
		t.Errorf("redirect.chain %+v", f)
	}

	// Named by the URLs they answer at, the same pages are clean.
	for i := range s.Pages {
		p := &s.Pages[i]
		p.Canonical, p.Hreflang["en"], p.Redirects, p.Links = p.URL, p.URL, nil, []string{"/about/"}
	}
	s.Sitemap.URLs = []string{sitetest.Base + "/", sitetest.Base + "/about/"}
	s.Hops = map[string][]site.Hop{"/about": s.Hops["/about"]}
	for _, f := range Run(s, target()) {
		if strings.Contains(f.Check, "redirect") {
			t.Errorf("clean site reported %+v", f)
		}
	}
}

// TestLiveCrawlFindings checks what only a live crawl reports: sitemap URLs
// that did not answer with their page, and the parts the crawl could not read.
func TestLiveCrawlFindings(t *testing.T) {
	s := load(t, func(f map[string]string) {
		f["sitemap.xml"] = strings.Replace(f["sitemap.xml"], "</urlset>", `<url><loc>`+sitetest.Base+`/gone</loc></url></urlset>`, 1)
	})
	s.Live = true
	s.Failed = map[string]string{"/gone": "answers 404", "/linked": "answers 500"}
	for i := range s.Pages {
		if s.Pages[i].Path == "/about" {
			s.Pages[i].Canonical = sitetest.Base + "/gone"
		}
	}
	s.Skipped = []string{"/private"}
	s.Truncated = true
	got := map[string]Finding{}
	for _, f := range Run(s, target()) {
		got[f.Check] = f
	}
	if f := got["sitemap.url.status"]; f.Severity != Error || !strings.Contains(f.Message, "answers 404") || !slices.Contains(f.Pages, "/gone") {
		t.Errorf("sitemap.url.status %+v", f)
	}
	if _, ok := got["sitemap.url.unknown"]; ok {
		t.Error("a live crawl reported sitemap.url.unknown")
	}
	if f, ok := got["crawl.failed"]; !ok || f.Severity != Error || !slices.Contains(f.Pages, "/linked") || slices.Contains(f.Pages, "/gone") {
		t.Errorf("crawl.failed %+v", f)
	}
	if f := got["meta.canonical.target"]; f.Severity != Error || !strings.Contains(f.Message, "answers 404") || !slices.Equal(f.Pages, []string{"/about"}) {
		t.Errorf("meta.canonical.target %+v", f)
	}
	if !slices.Contains(got["crawl.skipped"].Pages, "/private") || got["crawl.truncated"].Severity != Info {
		t.Errorf("crawl findings %+v %+v", got["crawl.skipped"], got["crawl.truncated"])
	}
}

func TestValidLang(t *testing.T) {
	for code, want := range map[string]bool{"en": true, "pt-br": true, "zh-hant": true,
		"eng": false, "e1": false, "en-1": false, "en-us-x": false, "EN": false} {
		if got := validLang(code); got != want {
			t.Errorf("validLang(%q) = %v", code, got)
		}
	}
}

func TestLocale(t *testing.T) {
	c := &run{t: config.Target{Locales: []string{"en", "ro"}}}
	for path, want := range map[string]string{"/ro": "ro", "/ro/x": "ro", "/rome": "en", "/": "en"} {
		if got := c.locale(path); got != want {
			t.Errorf("locale(%q) = %q", path, got)
		}
	}
}

// TestJobPostingLastDay checks a date-only validThrough holds all that day.
func TestJobPostingLastDay(t *testing.T) {
	now = func() time.Time { return time.Date(2026, 10, 13, 15, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = time.Now })
	for through, want := range map[string]bool{
		"2026-10-13": false, "2026-10-12": true,
		"2026-10-13T14:00:00Z": true, "2026-10-13T16:00:00Z": false, "2026-10-13T14:00": true,
	} {
		expired := false
		for _, f := range Run(load(t, ld(about, strings.Replace(job, "2026-12-31", through, 1))), target()) {
			expired = expired || f.Check == "schema.jobposting.expired"
		}
		if expired != want {
			t.Errorf("validThrough %s: expired = %v", through, expired)
		}
	}
}

// TestPageControlledTextIsQuoted checks that control characters in structured
// data cannot reach a message raw, where they would forge rows or inject escapes.
func TestPageControlledTextIsQuoted(t *testing.T) {
	article := `{"@type":["X\u001b[31m","Article"],"datePublished":"x\u001b[31mforged\nrow"}`
	edit := ld(about, `[{"@type":"Organization\u001b[31m","name":"Example","telephone":"+44\nforged"},`+article+`]`)
	tg := target()
	tg.Business = &config.Business{Name: "Example", Phones: []string{"+44 1 2"}}
	for _, f := range Run(load(t, edit), tg) {
		if strings.ContainsAny(f.Message+f.Fix, "\x1b\n") {
			t.Errorf("%s carries a raw control character: %q", f.Check, f.Message)
		}
	}
}

// TestUnreadableSitemapIsNotMissing covers a sitemap named in robots.txt that
// answers 404: it was looked for, so "there is no sitemap.xml" would mislead.
func TestUnreadableSitemapIsNotMissing(t *testing.T) {
	s := load(t, nil)
	s.Sitemap = site.Sitemap{Err: sitetest.Base + "/s.xml: 404"}
	var found *Finding
	for _, f := range Run(s, target()) {
		if f.Check == "sitemap.missing" {
			t.Errorf("reported sitemap.missing: %+v", f)
		}
		if f.Check == "sitemap.invalid" {
			found = &f
		}
	}
	if found == nil || !strings.Contains(found.Message, "/s.xml: 404") {
		t.Errorf("want sitemap.invalid naming the URL, got %+v", found)
	}
}

func TestShadowedPageIsReported(t *testing.T) {
	s := load(t, nil)
	s.Shadowed = map[string]string{"/about": "/about/"}
	var found *Finding
	for _, f := range Run(s, target()) {
		if f.Check == "crawl.shadowed" {
			found = &f
		}
	}
	if found == nil || found.Severity != Warn || !slices.Equal(found.Pages, []string{"/about"}) || !strings.Contains(found.Message, "/about/ was not checked") {
		t.Errorf("want crawl.shadowed on /about naming /about/, got %+v", found)
	}
}

// TestMergedRedirectFixesAreExamples checks that when several pages redirect,
// the merged finding does not present one page's URLs as the fix for all.
func TestMergedRedirectFixesAreExamples(t *testing.T) {
	base, _ := url.Parse(sitetest.Base)
	page := func(path string) site.Page {
		return site.Page{Path: path, URL: sitetest.Base + path + "/", Redirects: []int{301}, Title: "Example page for " + path,
			Canonical: sitetest.Base + path + "/", H1: []string{"x"}}
	}
	s := site.Site{Base: base, Live: true, Pages: []site.Page{page("/a"), page("/b")},
		Sitemap: site.Sitemap{Found: true, URLs: []string{sitetest.Base + "/a", sitetest.Base + "/b"}},
		Hops: map[string][]site.Hop{
			"/a": {{URL: sitetest.Base + "/a", Code: 301}, {URL: sitetest.Base + "/m", Code: 301}, {URL: sitetest.Base + "/a/", Code: 200}},
			"/b": {{URL: sitetest.Base + "/b", Code: 301}, {URL: sitetest.Base + "/m", Code: 301}, {URL: sitetest.Base + "/b/", Code: 200}},
		}}
	for _, f := range Run(s, target()) {
		if f.Check != "sitemap.url.redirect" && f.Check != "redirect.chain" {
			continue
		}
		if len(f.Pages) != 2 || !strings.Contains(f.Fix, "e.g. ") {
			t.Errorf("%s: want two pages and a fix that gives an example, got %+v", f.Check, f)
		}
	}
}

// TestCanonicalOnAnotherSchemeIsReportedOnce checks that a canonical on the
// wrong scheme is meta.canonical.host only, not also a redirect.
func TestCanonicalOnAnotherSchemeIsReportedOnce(t *testing.T) {
	base, _ := url.Parse(sitetest.Base)
	p := site.Page{Path: "/a", URL: sitetest.Base + "/a/", Title: "Example page for a", H1: []string{"x"},
		Canonical: "http://example.com/a/"}
	s := site.Site{Base: base, Live: true, Pages: []site.Page{p}, Sitemap: site.Sitemap{Found: true}}
	got := map[string]bool{}
	for _, f := range Run(s, target()) {
		got[f.Check] = true
	}
	if !got["meta.canonical.host"] || got["meta.canonical.redirect"] {
		t.Errorf("want meta.canonical.host only, got %v", got)
	}
}
