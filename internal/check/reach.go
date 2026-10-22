package check

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/site"
)

// redirects checks, on a live crawl, that the site names every page by the
// exact URL it answers 200 at. A sitemap entry, canonical, alternate or link
// that redirects, even only to add a trailing slash, costs a hop and leaves
// search engines two URLs for one page.
func (c *run) redirects() {
	if !c.site.Live {
		return
	}
	for _, u := range c.site.Sitemap.URLs {
		p, ok := c.served(u)
		if !ok {
			continue
		}
		if !sameURL(u, p.URL) {
			c.add("sitemap.url.redirect", Error, p.Path, "the sitemap lists URLs that redirect",
				fmt.Sprintf("List each page at the URL it answers 200 at, e.g. %s, not %s%s.", p.URL, u, codes(p.Redirects)))
		}
		if slices.ContainsFunc(p.Redirects, temporary) {
			c.add("redirect.temporary", Warn, p.Path, "a permanent move answers with a temporary redirect (302, 303 or 307)",
				"Stop redirecting by naming the final URL everywhere; where a redirect must stay, answer 301 or 308, so search engines move to the new URL.")
		}
	}
	for _, from := range slices.Sorted(maps.Keys(c.site.Hops)) {
		chain := c.site.Hops[from]
		if len(chain) < 3 {
			continue // one redirect: the checks above and below name it
		}
		c.add("redirect.chain", Warn, from, "a URL redirects more than once",
			fmt.Sprintf("Redirect each URL straight to its final one, e.g. %s straight to %s; it now goes %s. Each hop costs crawl budget and a share of the ranking signals.", chain[0].URL, chain[len(chain)-1].URL, describe(chain)))
	}
	for _, p := range c.indexable {
		if p.URL == "" {
			continue
		}
		if p.Canonical != "" {
			// A canonical on another scheme or host is meta.canonical.host already.
			if canon := absURL(p.URL, p.Canonical); !sameURL(canon, p.URL) && c.onBase(canon) {
				c.add("meta.canonical.redirect", Error, p.Path, "the canonical is not the URL the page answers at",
					fmt.Sprintf("Name the URL each page answers at as its canonical, e.g. %s, not %s; or serve the page at the canonical without a redirect.", p.URL, canon))
			}
		}
		for _, href := range sortedValues(p.Hreflang) {
			if alt, to, ok := c.redirected(p, href); ok {
				c.add("hreflang.redirect", Warn, p.Path, "hreflang alternates name URLs that redirect",
					fmt.Sprintf("Name each alternate at the URL it answers at, e.g. %s, not %s.", to, alt))
				break
			}
		}
		for _, href := range p.Links {
			if from, to, ok := c.redirected(p, href); ok {
				c.add("links.redirect", Info, p.Path, "internal links point at URLs that redirect",
					fmt.Sprintf("Link the final URL, e.g. %s, not %s.", to, from))
				break
			}
		}
	}
}

// onBase reports whether u has the scheme and host the site is audited at.
func (c *run) onBase(u string) bool {
	parsed, err := url.Parse(u)
	return err == nil && parsed.Host == c.site.Base.Host && parsed.Scheme == c.site.Base.Scheme
}

// canonicalTargets checks a canonical that names another page: that page must
// exist and answer 200, or the page hands its ranking to nothing. A canonical
// on another host is meta.canonical.host; one naming the page itself at
// another URL is meta.canonical.redirect.
func (c *run) canonicalTargets() {
	for _, p := range c.site.Pages {
		if p.Noindex() || p.Canonical == "" || c.canonicalSelf(p) {
			continue
		}
		lp := c.site.Resolve(p, p.Canonical)
		if lp == "" || site.LooksLikeFile(lp) {
			continue
		}
		if _, ok := c.site.Page(lp); ok {
			continue
		}
		if why, failed := c.site.Failed[lp]; failed {
			c.add("meta.canonical.target", Error, p.Path, "the canonical points at a URL that "+why,
				"Point the canonical at the URL the page should rank under, one that answers 200 with it.")
		} else if !c.site.Live {
			c.add("meta.canonical.target", Error, p.Path, "the canonical points at a path with no page",
				"Point the canonical at an existing page, or at the page itself.")
		}
	}
}

func describe(chain []site.Hop) string {
	parts := make([]string, len(chain))
	for i, h := range chain {
		parts[i] = fmt.Sprintf("%s (%d)", h.URL, h.Code)
	}
	return strings.Join(parts, ", then ")
}

// redirected resolves href on page p and reports whether it names a crawled
// page by an address other than the one that page answered at.
func (c *run) redirected(p site.Page, href string) (from, to string, ok bool) {
	lp := c.site.Resolve(p, href)
	if lp == "" || site.LooksLikeFile(lp) {
		return "", "", false
	}
	target, found := c.site.Page(lp)
	if !found || target.URL == "" {
		return "", "", false
	}
	u, err := url.Parse(absURL(p.URL, href))
	if err != nil {
		return "", "", false
	}
	u.RawQuery = ""
	return u.String(), target.URL, !sameURL(u.String(), target.URL)
}

// served is the crawled page a URL names, when the crawl read it.
func (c *run) served(u string) (site.Page, bool) {
	p, ok := c.site.Page(c.site.LocalPath("/", u))
	return p, ok && p.URL != ""
}

func absURL(base, href string) string {
	b, err := url.Parse(base)
	if err != nil {
		return href
	}
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return href
	}
	u := b.ResolveReference(ref)
	u.Fragment = ""
	return u.String()
}

// sameURL compares two URLs as a server tells them apart: scheme, host, path
// with its trailing slash, and query. An empty path is the root.
func sameURL(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return a == b
	}
	key := func(u *url.URL) string {
		p := u.EscapedPath()
		if p == "" {
			p = "/"
		}
		return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + p + "?" + u.RawQuery
	}
	return key(ua) == key(ub)
}

func temporary(code int) bool { return code == 302 || code == 303 || code == 307 }

func codes(hops []int) string {
	if len(hops) == 0 {
		return ""
	}
	s := make([]string, len(hops))
	for i, h := range hops {
		s[i] = fmt.Sprint(h)
	}
	return " (" + strings.Join(s, ", ") + ")"
}

func sortedValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// brand checks what a search for the brand finds: titles that name it, and
// on each locale's home page a WebSite block with that name, which Google
// reads for the site name it shows in results.
func (c *run) brand() {
	brand := c.t.SearchName()
	if brand == "" {
		return
	}
	for _, p := range c.indexable {
		switch {
		case names(p.Title, brand):
		case c.home(p.Path):
			c.add("meta.title.brand", Warn, p.Path, "the home page title does not name the brand "+brand,
				"Put "+brand+" in the home page title: a search for the brand should find its own name in the result.")
		default:
			c.add("meta.title.brand", Info, p.Path, "the title does not name the brand "+brand,
				"End the title with \" · "+brand+"\"; an abbreviation or a slogan does not match a search for the brand.")
		}
	}
	for _, p := range c.indexable {
		if c.home(p.Path) && !slices.ContainsFunc(nodes(p.JSONLD), func(n node) bool { return n.is("WebSite") && names(n.str("name"), brand) }) {
			c.add("schema.website.name", Warn, p.Path, "the home page has no WebSite block named "+brand,
				`Add {"@type":"WebSite","name":"`+brand+`","url":"`+c.site.URL("/")+`"} to the home page: Google takes the site name in results from it.`)
		}
	}
}

func (c *run) home(path string) bool { return config.StripLocale(path, c.t.Locales) == "/" }

// names reports whether text carries the brand, ignoring case, spaces and
// punctuation, so "Zool Coder" and "ZoolCoder" match.
func names(text, brand string) bool {
	fold := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, s)
	}
	b := fold(brand)
	return b != "" && strings.Contains(fold(text), b)
}

// queries checks that each page named for a search says the search's words
// where engines weigh them most: the title and the h1, then the description.
func (c *run) queries() {
	for _, q := range c.t.Queries {
		p, ok := c.site.Page(site.CleanPath(q.Path))
		if !ok {
			c.add("content.query.page", Warn, q.Path, fmt.Sprintf("no page was read at %s to answer %q", q.Path, q.Query),
				"Build the page, or point the query at the page that answers it.")
			continue
		}
		var missing []string
		for _, f := range []struct{ name, text string }{{"title", p.Title}, {"h1", strings.Join(p.H1, " ")}, {"description", p.Description}} {
			if !mentions(f.text, q.Query) {
				missing = append(missing, f.name)
			}
		}
		if len(missing) == 0 {
			continue
		}
		sev := Warn
		if missing[0] == "description" {
			sev = Info
		}
		c.add("content.query", sev, p.Path, fmt.Sprintf("%q is not in the page's %s", q.Query, strings.Join(missing, ", ")),
			"Say the words people search in the title and the h1, and once in the description, in plain language; never stuff them.")
	}
}

// mentions reports whether every word of query appears in text. Words of
// four letters or more may differ by an ending of up to three letters
// (integration, integrations), and an Arabic word may carry its article (ال).
func mentions(text, query string) bool {
	have := words(text)
	for _, w := range words(query) {
		if !slices.ContainsFunc(have, func(h string) bool { return sameWord(h, w) || sameWord(strings.TrimPrefix(h, "ال"), w) }) {
			return false
		}
	}
	return true
}

func sameWord(a, b string) bool {
	if utf8.RuneCountInString(a) > utf8.RuneCountInString(b) {
		a, b = b, a
	}
	na, nb := utf8.RuneCountInString(a), utf8.RuneCountInString(b)
	if na < 4 {
		return a == b
	}
	return strings.HasPrefix(b, a) && nb-na <= 3
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
