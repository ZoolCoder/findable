package site

import (
	"bytes"
	"compress/gzip"
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

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{"/": "/", "/index.html": "/", "/a/": "/a", "/a/index.html": "/a", "/a.html": "/a", "/myindex.html": "/myindex"} {
		if got := CleanPath(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

// TestCrawlStaysOnTheHost serves a site that tries to lead the crawl away: a
// robots.txt sitemap on another host, a page that redirects off the site, a
// page that only exists as .html, a 404 and a page robots.txt keeps
// findable out of. Nothing may be fetched from the other host.
func TestCrawlStaysOnTheHost(t *testing.T) {
	var offHost int
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offHost++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(sitetest.Page("/", "Elsewhere", "Not this site")))
	}))
	defer other.Close()
	var srv *httptest.Server
	html := func(w http.ResponseWriter, path string) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(sitetest.Page(path, "Page "+path, "A page")))
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: findable\nDisallow: /private\n\nSitemap: " + srv.URL + "/sitemap.xml\nSitemap: " + other.URL + "/sitemap.xml\n"))
		case "/sitemap.xml":
			_, _ = w.Write([]byte(`<urlset>` +
				`<url><loc>` + srv.URL + `/</loc></url>` +
				`<url><loc>` + srv.URL + `/about.html</loc></url>` +
				`<url><loc>` + srv.URL + `/gone</loc></url>` +
				`<url><loc>` + srv.URL + `/away</loc></url>` +
				`<url><loc>` + srv.URL + `/old</loc></url>` +
				`<url><loc>` + srv.URL + `/private/x</loc></url></urlset>`))
		case "/", "/about.html", "/new":
			html(w, CleanPath(r.URL.Path))
		case "/away":
			http.Redirect(w, r, other.URL+"/", http.StatusMovedPermanently)
		case "/old":
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
		case "/private/x":
			t.Error("fetched a path robots.txt disallows for findable")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	s, err := Crawler{Client: srv.Client()}.Crawl(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range s.Pages {
		got = append(got, p.Path)
	}
	if !slices.Equal(got, []string{"/", "/about", "/new"}) {
		t.Errorf("pages %v", got)
	}
	if offHost != 0 {
		t.Errorf("%d requests reached the other host", offHost)
	}
	for path, want := range map[string]string{"/gone": "answers 404", "/away": "redirects (301)", "/old": "redirects to /new"} {
		if !strings.Contains(s.Failed[path], want) {
			t.Errorf("%s: %q, want %q", path, s.Failed[path], want)
		}
	}
	if !slices.Equal(s.Skipped, []string{"/private/x"}) || s.Truncated {
		t.Errorf("skipped %v truncated %v", s.Skipped, s.Truncated)
	}
}

func TestCrawlRefusesASitemapOnAnotherHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			_, _ = w.Write([]byte("Sitemap: https://elsewhere.example/sitemap.xml\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(sitetest.Page("/", "Home", "Home")))
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	s, err := Crawler{Client: srv.Client()}.Crawl(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if s.Sitemap.Found || len(s.Pages) != 1 {
		t.Errorf("sitemap %+v pages %d", s.Sitemap, len(s.Pages))
	}
}

func TestGzipSitemap(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(`<urlset><url><loc>https://example.com/a</loc></url></urlset>`))
	_ = zw.Close()
	urls, _, err := parseSitemap(buf.Bytes())
	if err != nil || !slices.Equal(urls, []string{"https://example.com/a"}) {
		t.Fatalf("%v %v", urls, err)
	}

	buf.Reset()
	zw = gzip.NewWriter(&buf)
	_, _ = zw.Write(bytes.Repeat([]byte(" "), maxSitemap+1))
	_ = zw.Close()
	if _, _, err := parseSitemap(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "unpacks to more than") {
		t.Errorf("a gzip bomb: %v", err)
	}
}

func TestCrawlTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<a href="/a">a</a><a href="/b">b</a><a href="/c">c</a>`))
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	s, _ := Crawler{Client: srv.Client(), Max: 2}.Crawl(context.Background(), base)
	if !s.Truncated || len(s.Pages) != 2 {
		t.Errorf("truncated %v pages %d", s.Truncated, len(s.Pages))
	}
}
