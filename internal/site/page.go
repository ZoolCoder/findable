// Package site reads a website into facts the checks can judge: every page's
// head, headings, links and JSON-LD, plus robots.txt, sitemap.xml and
// llms.txt. It reads a built site from a directory or crawls a live one from
// its sitemap, and judges nothing itself.
package site

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
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

	Robots File
	LLMs   File
	// LLMsFull is llms-full.txt, the text of the pages llms.txt lists.
	LLMsFull File
	Sitemap  Sitemap

	// Failed maps a path a crawl could not read to what it answered: a
	// status, a redirect, not HTML. A directory read lists the files it could
	// not parse, by the path they are served at.
	Failed map[string]string
	// Hops records, by requested path, the redirect chain a crawl followed
	// from that path: every response from the first request to the one that
	// ended the chain, a page or a dead end. Paths that did not redirect are
	// not listed.
	Hops map[string][]Hop
	// Skipped lists the paths robots.txt keeps findable out of, whether the
	// path was queued or a redirect led to it.
	Skipped []string
	// Shadowed maps a path a directory read found two files for to the file
	// that was dropped: the other file is the page, as a server answers.
	Shadowed map[string]string
	// Truncated is true when a crawl stopped at max_pages with pages left.
	Truncated bool
}

// Hop is one response in a redirect chain: the URL asked for and its status.
type Hop struct {
	URL  string
	Code int
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
	// Served is the path as the server serves it, "/blog/post/" for
	// blog/post/index.html: relative links resolve against it, not Path.
	Served string
	// XRobots is the X-Robots-Tag response header; empty for a directory read.
	XRobots string
	// URL is the exact address the page answered 200 at, after any
	// redirects; empty for a directory read.
	URL string
	// Redirects are the status codes of the redirects followed to reach URL
	// from the address requested, in order.
	Redirects []int

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
	// Text is the readable content as Markdown-ish plain text: headings as
	// # lines, list items as - lines, paragraphs separated by a blank line.
	// Navigation, headers, footers and asides are left out, and only the
	// first <main> or <article> is read when the page has one.
	Text       string
	JSONLD     []json.RawMessage
	JSONLDErrs []string
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
	u.Path = unescapeControls(path)
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

// LocalPath turns an href into a path on this site, or "" when it points
// elsewhere (another host, mailto:, a fragment only).
func (s Site) LocalPath(from, href string) string {
	abs := s.resolveURL(from, href)
	if abs == nil {
		return ""
	}
	return CleanPath(abs.Path)
}

// Resolve is LocalPath for an href written on page p: it resolves against the
// path p is served at, which a trailing slash makes a directory.
func (s Site) Resolve(p Page, href string) string {
	return s.LocalPath(cmp.Or(p.Served, p.Path), href)
}

// resolveURL resolves href against the path from, or returns nil when it
// points elsewhere or cannot be used.
func (s Site) resolveURL(from, href string) *url.URL {
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil || (ref.Scheme != "" && ref.Scheme != "http" && ref.Scheme != "https") {
		return nil
	}
	if ref.Host == "" && ref.Path == "" {
		return nil
	}
	page := *s.Base
	page.Path = from
	abs := page.ResolveReference(ref)
	if !sameHost(abs.Host, s.Base.Host) {
		return nil
	}
	return abs
}

// requestPath is the path and query of u as a client would send them, which
// is what robots.txt rules are written against.
func requestPath(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	return p
}

// CleanPath drops trailing slashes and index.html, so /a/, /a and
// /a/index.html are one page. Control characters stay percent-encoded: a page
// path is printed in reports and a link can decode to a newline or an escape.
func CleanPath(p string) string {
	p = escapeControls(p)
	if strings.HasSuffix(p, "/index.html") {
		p = strings.TrimSuffix(p, "index.html")
	} else {
		p = strings.TrimSuffix(p, ".html")
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	return p
}

// escapeControls percent-encodes the control characters of a decoded path.
func escapeControls(p string) string {
	// A literal percent sign that reads as an escaped control is encoded too,
	// so that unescapeControls cannot mistake it for one.
	p = escapedControl.ReplaceAllStringFunc(p, func(m string) string { return "%25" + m[1:] })
	if !strings.ContainsFunc(p, unicode.IsControl) {
		return p
	}
	var b strings.Builder
	for _, r := range p {
		if !unicode.IsControl(r) {
			b.WriteRune(r)
			continue
		}
		for _, c := range []byte(string(r)) {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

var (
	escapedControl         = regexp.MustCompile(`(?i)%(?:[01][0-9a-f]|7f|c2%[89][0-9a-f])`)
	escapedControlOrEscape = regexp.MustCompile(`(?i)%(?:25)?(?:[01][0-9a-f]|7f|c2%[89][0-9a-f])`)
)

// unescapeControls undoes escapeControls, so the URL built from a page path
// asks for the path the link named.
func unescapeControls(p string) string {
	return escapedControlOrEscape.ReplaceAllStringFunc(p, func(m string) string {
		dec, err := url.PathUnescape(m)
		if err != nil {
			return m
		}
		return dec
	})
}

// sameHost treats www. and the bare domain as one site.
func sameHost(a, b string) bool {
	return strings.TrimPrefix(strings.ToLower(a), "www.") == strings.TrimPrefix(strings.ToLower(b), "www.")
}
