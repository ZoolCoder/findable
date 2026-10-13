package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zoolcoder/findable/internal/sitetest"
)

func TestParse(t *testing.T) {
	p, err := Parse(strings.NewReader(`<!doctype html><html lang="ro"><head>
<title>  A   title </title>
<meta name="description" content="Desc">
<meta name="robots" content="noindex">
<meta name="googlebot" content="nofollow">
<link rel="canonical" href="https://a.example/x">
<link rel="alternate" hreflang="EN" href="/en/x">
<meta property="og:image" content="/og.jpg">
<script type="application/ld+json">{"@type":"Thing","name":"two  spaces"}</script>
<script type="application/ld+json">{broken</script>
<style>.x{}</style>
</head><body><h1>One <b>head</b></h1><h1>Two</h1>
<img src="a.png"><img src="b.png" alt=""><img src="c.png" role="presentation">
<a href="/y">some link text</a><script>var notWords = 1</script>
</body></html>`), "/x")
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case p.Lang != "ro", p.Title != "A title", p.Description != "Desc", p.Canonical != "https://a.example/x":
		t.Errorf("head %+v", p)
	case p.MetaRobots != "noindex,nofollow" || !p.Noindex():
		t.Errorf("robots %q", p.MetaRobots)
	case p.Hreflang["en"] != "/en/x", p.OG["og:image"] != "/og.jpg":
		t.Errorf("links %+v %+v", p.Hreflang, p.OG)
	case !slices.Equal(p.H1, []string{"One head", "Two"}), p.ImagesNoAlt != 1:
		t.Errorf("body %+v", p)
	case len(p.JSONLD) != 1 || !strings.Contains(string(p.JSONLD[0]), "two  spaces") || len(p.JSONLDErrs) != 1:
		t.Errorf("json-ld %s %v", p.JSONLD, p.JSONLDErrs)
	case !slices.Equal(p.Links, []string{"/y"}), p.Words != 6:
		t.Errorf("links %v words %d", p.Links, p.Words)
	}
}

func TestNoindexFromHeader(t *testing.T) {
	for v, want := range map[string]bool{"noindex, nofollow": true, "none": true, "nofollow": false, "": false} {
		if got := (Page{XRobots: v}).Noindex(); got != want {
			t.Errorf("%q: %v", v, got)
		}
	}
}

func TestLocalPath(t *testing.T) {
	base, _ := url.Parse("https://example.com")
	s := Site{Base: base}
	for href, want := range map[string]string{
		"/a/":                        "/a",
		"b":                          "/x/b",
		"../c/index.html":            "/c",
		"https://www.example.com/d":  "/d",
		"https://other.example/e":    "",
		"mailto:a@example.com":       "",
		"#top":                       "",
		"/f.html?q=1#h":              "/f",
		"https://example.com":        "/",
		"tel:+40123":                 "",
		"//example.com/g/index.html": "/g",
	} {
		if got := s.LocalPath("/x/y", href); got != want {
			t.Errorf("%q: %q, want %q", href, got, want)
		}
	}
}

func TestRobots(t *testing.T) {
	r := ParseRobots(`# comment
User-agent: *
Disallow: /admin
Allow: /admin/public

User-agent: GPTBot
User-agent: CCBot
Disallow: /

User-agent: Bingbot
Disallow: /*.pdf$
Disallow:

Sitemap: https://example.com/sitemap.xml
`)
	for _, c := range []struct {
		agent, path string
		want        bool
	}{
		{"Googlebot", "/", true},
		{"Googlebot", "/admin/x", false},
		{"Googlebot", "/admin/public/x", true},
		{"gptbot", "/", false},
		{"CCBot", "/anything", false},
		{"Bingbot", "/a.pdf", false},
		{"Bingbot", "/a.pdf?x", true},
		{"Bingbot", "/admin", true}, // a named group replaces the * group
	} {
		if got := r.Allowed(c.agent, c.path); got != c.want {
			t.Errorf("%s %s: %v", c.agent, c.path, got)
		}
	}
	if !r.Named("GPTBot") || r.Named("Googlebot") || !slices.Equal(r.Sitemaps, []string{"https://example.com/sitemap.xml"}) {
		t.Errorf("named or sitemaps wrong: %+v", r)
	}
	if !ParseRobots("").Allowed("GPTBot", "/") {
		t.Error("an empty robots.txt blocks")
	}
}

func TestLoadDir(t *testing.T) {
	root := t.TempDir()
	sitetest.Write(t, root)
	base, _ := url.Parse(sitetest.Base)
	s, err := LoadDir(filepath.Join(root, "dist"), base)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, p := range s.Pages {
		paths = append(paths, p.Path)
	}
	if !slices.Equal(paths, []string{"/", "/about"}) {
		t.Errorf("pages %v (404.html must be skipped)", paths)
	}
	if !s.Robots.Found || s.LLMs.Found || !s.Sitemap.Found || len(s.Sitemap.URLs) != 2 || s.Live {
		t.Errorf("files %+v", s)
	}
	if _, err := LoadDir(filepath.Join(root, "missing"), base); err == nil {
		t.Error("missing dir accepted")
	}
}

func TestSitemapIndex(t *testing.T) {
	files := map[string]string{
		"sitemap.xml": `<sitemapindex><sitemap><loc>/a.xml</loc></sitemap><sitemap><loc>/broken.xml</loc></sitemap><sitemap><loc>/a.xml</loc></sitemap></sitemapindex>`,
		"/a.xml":      `<urlset><url><loc> https://example.com/x </loc></url></urlset>`,
		"/broken.xml": `<html></html>`,
	}
	sm := collectSitemap("sitemap.xml", func(loc string) ([]byte, error) { return []byte(files[loc]), nil })
	if !sm.Found || !slices.Equal(sm.URLs, []string{"https://example.com/x"}) || !strings.Contains(sm.Err, "<html>") {
		t.Errorf("%+v", sm)
	}
}

func TestCrawl(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "test-agent" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nAllow: /\nSitemap: " + srv.URL + "/map.xml\n"))
		case "/map.xml":
			_, _ = w.Write([]byte(`<urlset><url><loc>` + srv.URL + `/</loc></url><url><loc>` + srv.URL + `/b</loc></url><url><loc>https://elsewhere.example/c</loc></url></urlset>`))
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(sitetest.Page("/", "Home", "Home page")))
		case "/b":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("X-Robots-Tag", "noindex")
			_, _ = w.Write([]byte(sitetest.Page("/b", "B", "B page")))
		default:
			// A single-page app: every unknown path answers its shell.
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html></html>"))
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	s, err := Crawler{Client: srv.Client(), UserAgent: "test-agent"}.Crawl(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pages) != 2 || s.Pages[1].Path != "/b" || !s.Pages[1].Noindex() || !s.Live {
		t.Fatalf("pages %+v", s.Pages)
	}
	if s.LLMs.Found {
		t.Error("the SPA shell counted as llms.txt")
	}
	if len(s.Sitemap.URLs) != 3 {
		t.Errorf("sitemap %+v", s.Sitemap)
	}
}

func TestCrawlFollowsLinksWithoutASitemap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`<a href="/a">a</a><a href="/b.pdf">pdf</a><a href="https://other.example/">x</a>`))
		case "/a":
			_, _ = w.Write([]byte(`<a href="/">home</a><a href="/c">c</a>`))
		case "/c":
			_, _ = w.Write([]byte(`c`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	s, err := Crawler{Client: srv.Client(), Max: 2}.Crawl(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pages) != 2 || s.Sitemap.Found || s.Robots.Found {
		t.Fatalf("crawl %+v", s)
	}
	s, _ = Crawler{Client: srv.Client()}.Crawl(context.Background(), base)
	if len(s.Pages) != 3 {
		t.Fatalf("pages %d", len(s.Pages))
	}
}
