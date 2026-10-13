// Package site reads a website into facts the checks can judge: every page's
// head, headings, links and JSON-LD, plus robots.txt, sitemap.xml and
// llms.txt. It reads a built site from a directory or crawls a live one from
// its sitemap, and judges nothing itself.
package site

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
)

// Site is everything one read found.
type Site struct {
	// Base is the site's public origin: canonical URLs and sitemap entries are
	// judged against it.
	Base *url.URL
	// Live is true for a crawl over HTTP. A directory read cannot see
	// response headers, and its link check can be exhaustive.
	Live  bool
	Pages []Page

	Robots  File
	LLMs    File
	Sitemap Sitemap
}

// File is a text file at the site root.
type File struct {
	Found bool
	Body  string
}

// Sitemap is the URL list, flattened across a sitemap index.
type Sitemap struct {
	Found bool
	URLs  []string
	// Err is set when the file exists but does not parse.
	Err string
}

// Page is one HTML document.
type Page struct {
	// Path is the URL path, "/" for the home page, without a trailing slash.
	Path string
	// XRobots is the X-Robots-Tag response header; empty for a directory read.
	XRobots string

	Lang        string
	Title       string
	Description string
	Canonical   string
	MetaRobots  string
	H1          []string
	Hreflang    map[string]string // lower-case hreflang code to href
	OG          map[string]string // og:title, og:image, ...
	ImagesNoAlt int
	Links       []string // every href, as written
	Words       int      // visible words in body text
	JSONLD      []json.RawMessage
	JSONLDErrs  []string
}

// Noindex reports whether the page asks not to be indexed, by meta tag or
// header.
func (p Page) Noindex() bool {
	return hasDirective(p.MetaRobots, "noindex") || hasDirective(p.XRobots, "noindex")
}

func hasDirective(v, d string) bool {
	for _, part := range strings.FieldsFunc(strings.ToLower(v), func(r rune) bool { return r == ',' || r == ' ' }) {
		if part == d || part == "none" {
			return true
		}
	}
	return false
}

// Page returns the page at path.
func (s Site) Page(path string) (Page, bool) {
	i := slices.IndexFunc(s.Pages, func(p Page) bool { return p.Path == path })
	if i < 0 {
		return Page{}, false
	}
	return s.Pages[i], true
}

// URL is the page's absolute URL on the base origin.
func (s Site) URL(path string) string {
	u := *s.Base
	u.Path = path
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

// LocalPath turns an href into a path on this site, or "" when it points
// elsewhere (another host, mailto:, a fragment only).
func (s Site) LocalPath(from, href string) string {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil || (ref.Scheme != "" && ref.Scheme != "http" && ref.Scheme != "https") {
		return ""
	}
	if ref.Host == "" && ref.Path == "" {
		return ""
	}
	page := *s.Base
	page.Path = from
	abs := page.ResolveReference(ref)
	if !sameHost(abs.Host, s.Base.Host) {
		return ""
	}
	return CleanPath(abs.Path)
}

// CleanPath drops a trailing slash and index.html, so /a/, /a and
// /a/index.html are one page.
func CleanPath(p string) string {
	p = strings.TrimSuffix(p, "index.html")
	p = strings.TrimSuffix(p, ".html")
	if p != "/" {
		p = strings.TrimSuffix(p, "/")
	}
	if p == "" {
		return "/"
	}
	return p
}

// sameHost treats www. and the bare domain as one site.
func sameHost(a, b string) bool {
	return strings.TrimPrefix(strings.ToLower(a), "www.") == strings.TrimPrefix(strings.ToLower(b), "www.")
}
