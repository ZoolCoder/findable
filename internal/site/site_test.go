package site

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	if s.LLMsFull.Found {
		t.Error("llms-full.txt found in a site without one")
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
	sm := collectSitemap([]string{"sitemap.xml"}, true, func(loc string) ([]byte, error) { return []byte(files[loc]), nil })
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

func TestCrawlRecordsRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/a/", http.StatusTemporaryRedirect)
		case "/a/", "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<a href="/a">a</a>`))
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
	a, ok := s.Page("/a")
	if !ok || a.URL != srv.URL+"/a/" || !slices.Equal(a.Redirects, []int{307}) {
		t.Fatalf("page %+v", a)
	}
	if h, _ := s.Page("/"); h.URL != srv.URL+"/" || len(h.Redirects) != 0 {
		t.Fatalf("home %+v", h)
	}
}

// TestCrawlRecordsRedirectChains follows /old through two redirects to /new
// and keeps every hop, and reads the canonical a page names even though
// nothing links to it.
func TestCrawlRecordsRedirectChains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<link rel="canonical" href="/canon"><a href="/old">old</a>`))
		case "/old":
			http.Redirect(w, r, "/mid", http.StatusMovedPermanently)
		case "/mid":
			http.Redirect(w, r, "/new", http.StatusFound)
		case "/new":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`new`))
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
	want := []Hop{{srv.URL + "/old", 301}, {srv.URL + "/mid", 302}, {srv.URL + "/new", 200}}
	if !slices.Equal(s.Hops["/old"], want) || len(s.Hops) != 1 {
		t.Errorf("hops %+v", s.Hops)
	}
	if s.Failed["/old"] != "redirects to /new" || s.Failed["/canon"] != "answers 404" {
		t.Errorf("failed %v", s.Failed)
	}
	if p, ok := s.Page("/new"); !ok || len(p.Redirects) != 0 {
		t.Errorf("new %+v %v", p, ok)
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
	for in, want := range map[string]string{
		"/": "/", "/index.html": "/", "/a/": "/a", "/a/index.html": "/a", "/a.html": "/a", "/myindex.html": "/myindex",
		"/a//": "/a", "/a///": "/a", "/a//index.html": "/a", "//": "/",
	} {
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

func TestResolveUsesServedPath(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"blog/post/index.html": sitetest.Page("/blog/post", "Post", "A post", "other", "../x"),
		"foo.html":             sitetest.Page("/foo", "Foo", "A foo", "bar"),
		"index.html":           sitetest.Page("/", "Home", "Home", "blog/post/"),
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := url.Parse(sitetest.Base)
	s, err := LoadDir(root, base)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/blog/post": "/blog/post/", "/foo": "/foo.html", "/": "/"} {
		if p, _ := s.Page(path); p.Served != want {
			t.Errorf("%s served at %q, want %q", path, p.Served, want)
		}
	}
	post, _ := s.Page("/blog/post")
	if got := s.Resolve(post, "other"); got != "/blog/post/other" {
		t.Errorf("other: %q", got)
	}
	if got := s.Resolve(post, "../x"); got != "/blog/x" {
		t.Errorf("../x: %q", got)
	}
	if got := s.Resolve(Page{Path: "/foo"}, "bar"); got != "/bar" {
		t.Errorf("fallback to Path: %q", got)
	}
	if got := s.Resolve(post, "https://elsewhere.example/a"); got != "" {
		t.Errorf("off-site: %q", got)
	}
}

func TestRobotsBOMAndSitemapLines(t *testing.T) {
	r := ParseRobots("\ufeffUser-agent: *\nDisallow: /\nSitemap: https://a/1.xml\nSitemap: https://a/2.xml\n")
	if r.Allowed(robotsToken, "/x") || len(r.Sitemaps) != 2 {
		t.Errorf("%+v", r)
	}
	r = ParseRobots("User-agent: a\nSitemap: https://a/1.xml\nUser-agent: b\nDisallow: /x\n")
	if len(r.Groups) != 1 || len(r.Groups[0].Agents) != 2 {
		t.Errorf("groups %+v", r.Groups)
	}
}

func TestCollectSitemapReportsEveryProblem(t *testing.T) {
	files := map[string]string{
		"/index.xml": `<sitemapindex><sitemap><loc>/a.xml</loc></sitemap><sitemap><loc>/b.xml</loc></sitemap></sitemapindex>`,
		"/a.xml":     `<html></html>`,
		"/b.xml":     `<html></html>`,
	}
	read := func(loc string) ([]byte, error) {
		if b, ok := files[loc]; ok {
			return []byte(b), nil
		}
		return nil, errNotFound
	}
	sm := collectSitemap([]string{"/index.xml", "/other.xml"}, false, read)
	if !strings.Contains(sm.Err, "/a.xml") || !strings.Contains(sm.Err, "/b.xml") || !strings.Contains(sm.Err, "/other.xml") {
		t.Errorf("err %q", sm.Err)
	}
	sm = collectSitemap([]string{"/none.xml"}, true, read)
	if sm.Found || sm.Err != "" {
		t.Errorf("missing optional root: %+v", sm)
	}
	sm = collectSitemap([]string{"/none.xml"}, false, read)
	if sm.Err == "" {
		t.Error("missing declared root not reported")
	}
}

func TestCollectSitemapCaps(t *testing.T) {
	var urls strings.Builder
	urls.WriteString("<urlset>")
	for range maxSitemapURLs + 10 {
		urls.WriteString("<url><loc>https://example.com/p</loc></url>")
	}
	urls.WriteString("</urlset>")
	sm := collectSitemap([]string{"a"}, false, func(string) ([]byte, error) { return []byte(urls.String()), nil })
	if len(sm.URLs) != maxSitemapURLs || !strings.Contains(sm.Err, "more than") {
		t.Errorf("urls %d err %q", len(sm.URLs), sm.Err)
	}

	var index strings.Builder
	index.WriteString("<sitemapindex>")
	for i := range maxSitemapFiles + 5 {
		fmt.Fprintf(&index, "<sitemap><loc>/c%d.xml</loc></sitemap>", i)
	}
	index.WriteString("</sitemapindex>")
	sm = collectSitemap([]string{"i"}, false, func(loc string) ([]byte, error) {
		if loc == "i" {
			return []byte(index.String()), nil
		}
		return []byte("<urlset></urlset>"), nil
	})
	if !strings.Contains(sm.Err, "left unread") {
		t.Errorf("err %q", sm.Err)
	}
}

func TestCrawlHonoursRobotsOnTheRequestedPath(t *testing.T) {
	var (
		mu   sync.Mutex
		hits []string
	)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\nDisallow: /about.html\nSitemap: " + srv.URL + "/map.xml\n"))
		case "/map.xml":
			_, _ = w.Write([]byte(`<urlset><url><loc>` + srv.URL + `/</loc></url><url><loc>` + srv.URL + `/private/</loc></url><url><loc>` + srv.URL + `/about.html</loc></url><url><loc>` + srv.URL + `/brochure.pdf</loc></url></urlset>`))
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(sitetest.Page("/", "Home", "Home page")))
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
	if !slices.Equal(s.Skipped, []string{"/about", "/private"}) {
		t.Errorf("skipped %v", s.Skipped)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, h := range hits {
		if h == "/private" || h == "/private/" || h == "/about" || h == "/about.html" || h == "/brochure.pdf" {
			t.Errorf("fetched %s", h)
		}
	}
	if len(s.Failed) != 0 {
		t.Errorf("failed %v", s.Failed)
	}
}

func TestCrawlReadsEverySitemapLine(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("\ufeffUser-agent: *\nAllow: /\nSitemap: " + srv.URL + "/en.xml\nSitemap: " + srv.URL + "/ro.xml\n"))
		case "/en.xml":
			_, _ = w.Write([]byte(`<urlset><url><loc>` + srv.URL + `/</loc></url></urlset>`))
		case "/ro.xml":
			_, _ = w.Write([]byte(`<urlset><url><loc>` + srv.URL + `/ro</loc></url></urlset>`))
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(sitetest.Page(r.URL.Path, "Page", "A page")))
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	s, err := Crawler{Client: srv.Client()}.Crawl(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sitemap.URLs) != 2 || len(s.Pages) != 2 || s.Sitemap.Err != "" {
		t.Errorf("sitemap %+v pages %d", s.Sitemap, len(s.Pages))
	}
}

func TestCrawlFollowsRelativeLinksFromServedPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/", "/blog/post/", "/blog/post/other":
		case "/blog/post":
			http.Redirect(w, r, "/blog/post/", http.StatusMovedPermanently)
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		links := map[string][]string{"/": {"/blog/post/"}, "/blog/post/": {"other"}}[r.URL.Path]
		_, _ = w.Write([]byte(sitetest.Page(r.URL.Path, "Page", "A page", links...)))
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	s, err := Crawler{Client: srv.Client()}.Crawl(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Page("/blog/post/other"); !ok || len(s.Failed) != 0 {
		t.Errorf("pages %d failed %v", len(s.Pages), s.Failed)
	}
}

func TestCrawlRobotsUnreachableAborts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	if _, err := (Crawler{Client: srv.Client()}).Crawl(context.Background(), base); err == nil || !strings.Contains(err.Error(), "unreachable (503)") {
		t.Errorf("err %v", err)
	}
}

func TestCrawlCancelledReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Crawler{Client: srv.Client()}).Crawl(ctx, base); err == nil {
		t.Error("cancelled crawl returned no error")
	}
}

func TestCrawlOversizedSitemapIsReported(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("Sitemap: " + srv.URL + "/big.xml\n"))
		case "/big.xml":
			_, _ = w.Write([]byte("<urlset>" + strings.Repeat(" ", maxSitemap+10) + "</urlset>"))
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
	if !strings.Contains(s.Sitemap.Err, "larger than 50 MB") {
		t.Errorf("err %q", s.Sitemap.Err)
	}
}

func TestCrawlTestsRobotsOnTheFetchedURLWithoutQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/robots.txt":
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /*?\n"))
		case "/":
			_, _ = w.Write([]byte(`<a href="/search?q=x">search</a>`))
		case "/search":
			_, _ = w.Write([]byte(`results`))
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
	if len(s.Pages) != 2 || len(s.Skipped) != 0 {
		t.Errorf("pages %d skipped %v: /search is fetched without its query, so the rule does not apply", len(s.Pages), s.Skipped)
	}
}

func TestCrawlCapsFollowedPaths(t *testing.T) {
	defer func(old int) { maxFollowPaths = old }(maxFollowPaths)
	maxFollowPaths = 2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<a href="/a">a</a><a href="/b">b</a><a href="/c">c</a>`))
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	s, err := Crawler{Client: srv.Client()}.Crawl(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pages) != 2 || !s.Truncated {
		t.Errorf("pages %d truncated %v: links past the cap are dropped and the crawl says so", len(s.Pages), s.Truncated)
	}
}

func TestCollectSitemapCountsEachUnreadChildOnce(t *testing.T) {
	var index strings.Builder
	index.WriteString("<sitemapindex>")
	for range 2 {
		for i := range maxSitemapFiles + 10 {
			fmt.Fprintf(&index, "<sitemap><loc>/c%d.xml</loc></sitemap>", i)
		}
	}
	index.WriteString("</sitemapindex>")
	sm := collectSitemap([]string{"i"}, false, func(loc string) ([]byte, error) {
		if loc == "i" {
			return []byte(index.String()), nil
		}
		return []byte("<urlset></urlset>"), nil
	})
	if want := "stopped after 50 sitemaps; 11 left unread"; !strings.Contains(sm.Err, want) {
		t.Errorf("err %q want %q", sm.Err, want)
	}
}

func TestCollectSitemapFullURLCapIsNotTruncation(t *testing.T) {
	var urls strings.Builder
	urls.WriteString("<urlset>")
	for range maxSitemapURLs {
		urls.WriteString("<url><loc>https://example.com/p</loc></url>")
	}
	urls.WriteString("</urlset>")
	read := func(loc string) ([]byte, error) {
		if loc == "i" {
			return []byte("<sitemapindex><sitemap><loc>/a.xml</loc></sitemap><sitemap><loc>/b.xml</loc></sitemap></sitemapindex>"), nil
		}
		return []byte(urls.String()), nil
	}
	sm := collectSitemap([]string{"i"}, false, read)
	if strings.Contains(sm.Err, "more than") || !strings.Contains(sm.Err, "1 left unread") {
		t.Errorf("err %q: exactly %d URLs were read, nothing was cut", sm.Err, maxSitemapURLs)
	}
}

func TestLoadDirSitemapIndexOnDisk(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"index.html":  sitetest.Page("/", "Home", "Home page"),
		"sitemap.xml": `<sitemapindex><sitemap><loc>https://example.com/a.xml</loc></sitemap><sitemap><loc>https://other.example/b.xml</loc></sitemap></sitemapindex>`,
		"a.xml":       `<urlset><url><loc>https://example.com/x</loc></url></urlset>`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := url.Parse("https://example.com")
	s, err := LoadDir(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Sitemap.URLs, []string{"https://example.com/x"}) || !strings.Contains(s.Sitemap.Err, "is on another host") {
		t.Errorf("sitemap %+v", s.Sitemap)
	}
}

func TestLoadDirKeepsGoingPastAnUnparseablePage(t *testing.T) {
	dir := t.TempDir()
	deep := strings.Repeat("<div>", 600) + "x"
	for name, body := range map[string]string{"deep/index.html": deep, "ok.html": sitetest.Page("/ok", "Ok", "An ok page")} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := url.Parse("https://example.com")
	s, err := LoadDir(dir, base)
	if err != nil {
		t.Fatal(err)
	}
	why := s.Failed["/deep/"]
	if len(s.Pages) != 1 || !strings.Contains(why, "could not be parsed") {
		t.Errorf("pages %d failed %v", len(s.Pages), s.Failed)
	}
}

// fakeSite answers requests from a function, so crawl tests need no server.
type fakeSite func(req *http.Request) (status int, header http.Header, body string)

func (f fakeSite) RoundTrip(req *http.Request) (*http.Response, error) {
	status, header, body := f(req)
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func htmlHeader() http.Header { return http.Header{"Content-Type": {"text/html"}} }

func redirectTo(loc string) http.Header { return http.Header{"Location": {loc}} }

func fakeBase(t *testing.T) *url.URL {
	t.Helper()
	base, err := url.Parse(sitetest.Base)
	if err != nil {
		t.Fatal(err)
	}
	return base
}

func TestPathsKeepControlCharactersEncoded(t *testing.T) {
	const href = "/x/%0a==%20fake%1b[2J"
	const want = "/x/%0A== fake%1B[2J"
	s := Site{Base: fakeBase(t)}
	if got := s.LocalPath("/", href); got != want {
		t.Errorf("LocalPath %q, want %q", got, want)
	}
	if got := s.URL(want); !strings.Contains(got, "%0A") || !strings.Contains(got, "%1B") {
		t.Errorf("URL %q lost the control characters", got)
	}

	var asked string
	client := &http.Client{Transport: fakeSite(func(req *http.Request) (int, http.Header, string) {
		switch req.URL.Path {
		case "/":
			return 200, htmlHeader(), sitetest.Page("/", "Home", "Home page", href)
		case "/robots.txt", "/sitemap.xml", "/llms.txt", "/llms-full.txt":
			return 404, nil, ""
		}
		asked = req.URL.Path
		return 404, nil, ""
	})}
	got, err := Crawler{Client: client}.Crawl(context.Background(), fakeBase(t))
	if err != nil {
		t.Fatal(err)
	}
	if asked != "/x/\n== fake\x1b[2J" {
		t.Errorf("the server was asked for %q", asked)
	}
	if _, ok := got.Failed[want]; !ok || len(got.Failed) != 1 {
		t.Errorf("failed %v, want one entry keyed %q", got.Failed, want)
	}
}

func TestLooksLikeFile(t *testing.T) {
	for p, want := range map[string]bool{
		"/docs/v2.0": false, "/blog/node.js-tips": false, "/releases/v1.2": false, "/index.html": false,
		"/a/b.pdf": true, "/logo.PNG": true, "/app.js": true, "/": false, "/about": false,
	} {
		if got := LooksLikeFile(p); got != want {
			t.Errorf("LooksLikeFile(%s) = %v, want %v", p, got, want)
		}
	}
}

func TestCrawlReadsDottedPageNames(t *testing.T) {
	client := &http.Client{Transport: fakeSite(func(req *http.Request) (int, http.Header, string) {
		switch req.URL.Path {
		case "/":
			return 200, htmlHeader(), sitetest.Page("/", "Home", "Home page", "/docs/v2.0")
		case "/docs/v2.0":
			return 200, htmlHeader(), sitetest.Page("/docs/v2.0", "Docs", "Docs page")
		}
		return 404, nil, ""
	})}
	s, err := Crawler{Client: client}.Crawl(context.Background(), fakeBase(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Page("/docs/v2.0"); !ok {
		t.Errorf("pages %v, failed %v: /docs/v2.0 was never read", s.Pages, s.Failed)
	}
}

func TestLiteralPercentInAPathSurvivesTheRoundTrip(t *testing.T) {
	s := Site{Base: fakeBase(t)}
	// The link names a literal "%0A", not a newline: its path decodes to "%0A".
	got := s.LocalPath("/", "/a%250Ab")
	if got != "/a%250Ab" {
		t.Errorf("LocalPath %q, want the percent sign encoded as %%25", got)
	}
	if u := s.URL(got); u != sitetest.Base+"/a%250Ab" {
		t.Errorf("URL %q asks for a different path than the link named", u)
	}
}

func TestCanonicalAndRedirectTargetsHonourRobots(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	client := &http.Client{Transport: fakeSite(func(req *http.Request) (int, http.Header, string) {
		mu.Lock()
		asked = append(asked, req.URL.Path)
		mu.Unlock()
		switch req.URL.Path {
		case "/robots.txt":
			return 200, nil, "User-agent: *\nDisallow: /admin/\nDisallow: /staff/\n"
		case "/":
			return 200, htmlHeader(), sitetest.Page("/", "Home", "Home page", "/old")
		case "/old":
			return 200, htmlHeader(), strings.Replace(sitetest.Page("/old", "Old", "Old page"), "</head>", `<link rel="canonical" href="/admin/"></head>`, 1)
		case "/admin/", "/staff/":
			return 200, htmlHeader(), sitetest.Page(req.URL.Path, "Hidden", "Hidden page")
		}
		return 404, nil, ""
	})}
	// /old answers 200 at /old but its canonical is the disallowed /admin/,
	// which a server may answer at /admin with no redirect to catch.
	s, err := Crawler{Client: client}.Crawl(context.Background(), fakeBase(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range asked {
		if p == "/admin/" || p == "/admin" {
			t.Errorf("fetched %s, which robots.txt disallows", p)
		}
	}
	if !slices.Equal(s.Skipped, []string{"/admin"}) {
		t.Errorf("skipped %v, want the canonical target", s.Skipped)
	}
}

func TestCrawlDoesNotFollowARedirectIntoARobotsDisallow(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	client := &http.Client{Transport: fakeSite(func(req *http.Request) (int, http.Header, string) {
		mu.Lock()
		asked = append(asked, req.URL.Path)
		mu.Unlock()
		switch req.URL.Path {
		case "/robots.txt":
			return 200, nil, "User-agent: *\nDisallow: /docs/\nDisallow: /private/\n"
		case "/":
			return 200, htmlHeader(), sitetest.Page("/", "Home", "Home page", "/docs", "/old")
		case "/docs":
			return 301, redirectTo("/docs/"), ""
		case "/old":
			return 301, redirectTo("/private/x"), ""
		case "/docs/", "/private/x":
			return 200, htmlHeader(), sitetest.Page(req.URL.Path, "Hidden", "Hidden page")
		}
		return 404, nil, ""
	})}
	s, err := Crawler{Client: client}.Crawl(context.Background(), fakeBase(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range asked {
		if p == "/docs/" || p == "/private/x" {
			t.Errorf("fetched %s, which robots.txt disallows", p)
		}
	}
	if _, ok := s.Page("/docs"); ok || len(s.Pages) != 1 {
		t.Errorf("pages %v: a page behind a disallowed redirect was audited", s.Pages)
	}
	if !slices.Equal(s.Skipped, []string{"/docs", "/old"}) {
		t.Errorf("skipped %v", s.Skipped)
	}
	if len(s.Failed) != 0 {
		t.Errorf("failed %v", s.Failed)
	}
}

func TestCrawlStopsWhenEveryFetchRedirectsToANewPath(t *testing.T) {
	var requests atomic.Int64
	client := &http.Client{Transport: fakeSite(func(req *http.Request) (int, http.Header, string) {
		if req.URL.Path == "/robots.txt" || req.URL.Path == "/sitemap.xml" || req.URL.Path == "/llms.txt" || req.URL.Path == "/llms-full.txt" {
			return 404, nil, ""
		}
		requests.Add(1)
		n, _ := strconv.Atoi(strings.TrimPrefix(req.URL.Path, "/p"))
		if req.Response == nil {
			return 302, redirectTo("/p" + strconv.Itoa(n+1)), ""
		}
		return 200, htmlHeader(), sitetest.Page(req.URL.Path, "Page", "A page")
	})}
	s, err := Crawler{Client: client, Max: 2}.Crawl(context.Background(), fakeBase(t))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Truncated || len(s.Pages) != 0 {
		t.Errorf("truncated %v, pages %d", s.Truncated, len(s.Pages))
	}
	if limit := 2*attemptsPerPage + 1; requests.Load() > int64(limit*2) {
		t.Errorf("%d requests for a budget of %d fetches", requests.Load(), limit)
	}
}

func TestCrawlFollowsFiveRedirectsAndNoMore(t *testing.T) {
	for hopsCount, wantRefused := range map[int]bool{5: false, 6: true} {
		client := &http.Client{Transport: fakeSite(func(req *http.Request) (int, http.Header, string) {
			n, ok := strings.CutPrefix(req.URL.Path, "/c/")
			i, _ := strconv.Atoi(n)
			switch {
			case !ok:
				return 404, nil, ""
			case i < hopsCount:
				return 301, redirectTo("/c/" + strconv.Itoa(i+1)), ""
			}
			return 200, htmlHeader(), sitetest.Page(req.URL.Path, "End", "End page")
		})}
		c := Crawler{Client: sameHostClient(client, fakeBase(t), Robots{})}
		r := c.page(context.Background(), fetch{path: "/c/0", url: sitetest.Base + "/c/0"}, Robots{})
		if refused := strings.Contains(r.why, "past"); refused != wantRefused {
			t.Errorf("%d redirects: why %q, refused %v", hopsCount, r.why, refused)
		}
	}
}

func TestRobotsMatchesNonASCIIPatterns(t *testing.T) {
	r := ParseRobots("User-agent: *\nDisallow: /café\nDisallow: /a%c3%a9b\n")
	for _, p := range []string{"/caf%C3%A9", "/caf%c3%a9/menu", "/a%C3%A9b", "/aéb"} {
		if r.Allowed("findable", p) {
			t.Errorf("%s is allowed", p)
		}
	}
	if !r.Allowed("findable", "/cafe") {
		t.Error("/cafe is blocked")
	}
}

func TestLoadDirKeepsOnePagePerPath(t *testing.T) {
	root := t.TempDir()
	for name, title := range map[string]string{"a.html": "File", "a/index.html": "Directory index"} {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(sitetest.Page("/a", title, "A page")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := LoadDir(root, fakeBase(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pages) != 1 || s.Pages[0].Title != "File" {
		t.Fatalf("pages %+v: want only the a.html page", s.Pages)
	}
	if s.Shadowed["/a"] != "/a/" || len(s.Failed) != 0 {
		t.Errorf("shadowed %v failed %v", s.Shadowed, s.Failed)
	}
}
