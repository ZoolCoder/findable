package check

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/zoolcoder/findable/internal/site"
)

// Length bounds Google shows in full on a phone, in characters. Google cuts
// by pixel width, so these are a guide, not a rule.
const (
	titleMin, titleMax = 15, 60
	descMin, descMax   = 50, 160
)

// meta checks each indexable page's head: title, description, h1, lang,
// canonical, Open Graph and image alt text.
func (c *run) meta() {
	titles, descs := map[string][]string{}, map[string][]string{}
	for _, p := range c.indexable {
		if p.Title != "" {
			titles[p.Title] = append(titles[p.Title], p.Path)
		}
		if p.Description != "" {
			descs[p.Description] = append(descs[p.Description], p.Path)
		}
		switch n := utf8.RuneCountInString(p.Title); {
		case n == 0:
			c.add("meta.title.missing", Error, p.Path, "the page has no <title>", "Write a unique title naming the page's subject and the brand.")
		case n > titleMax:
			c.add("meta.title.length", Warn, p.Path, fmt.Sprintf("the title is over %d characters and is cut short in results", titleMax), "Put the words a searcher types first; keep it under 60 characters.")
		case n < titleMin:
			c.add("meta.title.length", Warn, p.Path, fmt.Sprintf("the title is under %d characters", titleMin), "Name what the page offers, not only the brand.")
		}
		switch n := utf8.RuneCountInString(p.Description); {
		case n == 0:
			c.add("meta.description.missing", Warn, p.Path, "the page has no meta description", "Write one or two sentences that answer why to click: Google shows it under the title.")
		case n > descMax:
			c.add("meta.description.length", Info, p.Path, fmt.Sprintf("the description is over %d characters and is cut short", descMax), "Keep the point in the first 150 characters.")
		case n < descMin:
			c.add("meta.description.length", Info, p.Path, fmt.Sprintf("the description is under %d characters", descMin), "Say what the page offers and who it is for.")
		}
		switch len(p.H1) {
		case 0:
			c.add("meta.h1.missing", Warn, p.Path, "the page has no <h1>", "Give the page one visible <h1> that states its subject.")
		case 1:
		default:
			c.add("meta.h1.multiple", Info, p.Path, "the page has more than one <h1>", "Keep one <h1>; make the rest <h2>.")
		}
		if p.Lang == "" {
			c.add("meta.lang.missing", Warn, p.Path, "<html> has no lang attribute", `Set <html lang="..."> to the page's language.`)
		}
		if p.OG["og:title"] == "" || p.OG["og:image"] == "" {
			c.add("social.og.missing", Info, p.Path, "og:title or og:image is missing, so shared links show no card", "Add og:title, og:description and a 1200x630 og:image.")
		}
		if p.ImagesNoAlt > 0 {
			c.add("img.alt.missing", Warn, p.Path, "images have no alt attribute", `Describe each image in alt; use alt="" for decoration.`)
		}
	}
	dupes := func(check string, groups map[string][]string, what string) {
		for _, paths := range groups {
			if len(paths) < 2 {
				continue
			}
			for _, p := range paths {
				c.add(check, Warn, p, fmt.Sprintf("pages share the same %s", what), "Make each page's "+what+" say what is different about that page.")
			}
		}
	}
	// A canonical on the wrong host makes a page not indexable, so this
	// looks at every page that asks to be indexed.
	for _, p := range c.site.Pages {
		if !p.Noindex() {
			c.canonical(p)
		}
	}
	dupes("meta.title.duplicate", titles, "title")
	dupes("meta.description.duplicate", descs, "description")
}

func (c *run) canonical(p site.Page) {
	switch {
	case p.Canonical == "":
		c.add("meta.canonical.missing", Warn, p.Path, "the page names no canonical URL", `Add <link rel="canonical" href="`+c.site.URL(p.Path)+`">.`)
		return
	case !strings.HasPrefix(p.Canonical, "http"):
		c.add("meta.canonical.relative", Warn, p.Path, "the canonical URL is relative", "Write the canonical as an absolute URL on the public host.")
		return
	}
	u, err := url.Parse(p.Canonical)
	if err != nil {
		c.add("meta.canonical.host", Error, p.Path, "the canonical URL does not parse", "Fix the canonical URL.")
		return
	}
	if u.Host != c.site.Base.Host || u.Scheme != c.site.Base.Scheme {
		c.add("meta.canonical.host", Error, p.Path, fmt.Sprintf("the canonical points at %s://%s, not %s", u.Scheme, u.Host, c.site.Base),
			"Build with the public origin, so every canonical names it. A preview host must not be canonical.")
	}
}

// indexing checks noindex: on pages that should rank, and in the sitemap.
func (c *run) indexing() {
	listed := c.sitemapPaths()
	for _, p := range c.site.Pages {
		if !p.Noindex() {
			continue
		}
		how := "meta robots"
		if strings.Contains(strings.ToLower(p.XRobots), "noindex") || strings.Contains(strings.ToLower(p.XRobots), "none") {
			how = "the X-Robots-Tag header"
		}
		if !c.allowedNoindex(p.Path) {
			c.add("index.noindex", Error, p.Path, "the page asks not to be indexed ("+how+")",
				"Remove noindex, or add the path to noindex: in the config if it is meant to stay out.")
		}
		if listed[p.Path] {
			c.add("index.noindex.listed", Error, p.Path, "a noindex page is in the sitemap", "Drop noindex pages from the sitemap: it lists what should be indexed.")
		}
	}
}

// links checks internal links. A directory read knows every page, so a link
// to a path with no page is broken; a live crawl only knows what it fetched.
func (c *run) links() {
	inbound := map[string]bool{}
	for _, p := range c.site.Pages {
		for _, href := range p.Links {
			lp := c.site.Resolve(p, href)
			if lp == "" || site.LooksLikeFile(lp) {
				continue
			}
			if lp != p.Path {
				inbound[lp] = true
			}
			if _, ok := c.site.Page(lp); !ok && !c.site.Live {
				c.add("links.broken", Error, p.Path, "links to "+lp+", which is not a page", "Fix the link or build the page.")
			}
		}
	}
	for _, p := range c.indexable {
		if p.Path != "/" && !inbound[p.Path] {
			c.add("links.orphan", Warn, p.Path, "no other page links here", "Link it from a related page or the navigation: crawlers and readers find pages by links.")
		}
	}
}

func (c *run) sitemapPaths() map[string]bool {
	out := map[string]bool{}
	for _, u := range c.site.Sitemap.URLs {
		if lp := c.site.LocalPath("/", u); lp != "" {
			out[lp] = true
		}
	}
	return out
}

func (c *run) sitemap() {
	sm := c.site.Sitemap
	if !sm.Found {
		if sm.Err != "" {
			c.add("sitemap.invalid", Error, "", "the sitemap could not be read: "+sm.Err, "Serve the sitemap named in robots.txt (or /sitemap.xml) with a 200 and a valid <urlset> or <sitemapindex>.")
			return
		}
		c.add("sitemap.missing", Error, "", "there is no sitemap.xml", "Publish /sitemap.xml listing every indexable page, and name it in robots.txt.")
		return
	}
	if sm.Err != "" {
		c.add("sitemap.invalid", Error, "", "the sitemap does not parse: "+sm.Err, "Serve a valid <urlset> or <sitemapindex>.")
	}
	listed := c.sitemapPaths()
	for _, u := range sm.URLs {
		lp := c.site.LocalPath("/", u)
		switch {
		case lp == "":
			c.add("sitemap.url.host", Error, "", "the sitemap lists URLs on another host", "List only URLs on "+c.site.Base.String()+".")
		case !strings.HasPrefix(u, c.site.Base.Scheme+"://"+c.site.Base.Host):
			c.add("sitemap.url.host", Warn, lp, "the sitemap URL is not on the canonical origin "+c.site.Base.String(), "List each page by its canonical URL.")
		case site.LooksLikeFile(lp):
			// PDFs, images and video are valid sitemap entries; nothing to check.
		default:
			if p, ok := c.site.Page(lp); ok {
				if !c.canonicalSelf(p) {
					c.add("sitemap.url.canonical", Warn, lp, "the sitemap lists a page whose canonical is another URL", "List the canonical URL instead.")
				}
			} else if why, failed := c.site.Failed[lp]; failed {
				c.add("sitemap.url.status", Error, lp, "the sitemap lists a URL that "+why, "List only URLs that answer 200 with the page itself: drop it, or list where it redirects.")
			} else if !c.site.Live {
				c.add("sitemap.url.unknown", Error, lp, "the sitemap lists a path with no page", "Drop it from the sitemap or build the page.")
			}
		}
	}
	for _, p := range c.indexable {
		if !listed[p.Path] {
			c.add("sitemap.page.missing", Warn, p.Path, "an indexable page is not in the sitemap", "Add it to the sitemap.")
		}
	}
}

// crawl says what a crawl or directory read could not read, so a clean report
// is not mistaken for a full one.
func (c *run) crawl() {
	for _, p := range c.site.Skipped {
		c.add("crawl.skipped", Info, p, "robots.txt keeps findable out, so the page was not checked", "Allow findable in robots.txt, or audit the built directory instead.")
	}
	listed := c.sitemapPaths()
	for _, p := range slices.Sorted(maps.Keys(c.site.Failed)) {
		if listed[p] {
			continue // sitemap.url.status reports it
		}
		why := c.site.Failed[p]
		sev := Error
		if strings.HasPrefix(why, "redirects to ") {
			sev = Info
		}
		c.add("crawl.failed", sev, p, "the page could not be read: it "+why, "Fix the URL, or the link that leads to it.")
	}
	for _, p := range slices.Sorted(maps.Keys(c.site.Shadowed)) {
		c.add("crawl.shadowed", Warn, p, fmt.Sprintf("two files serve this path; %s was not checked", c.site.Shadowed[p]), "Keep one file per path: delete the other, or make it a redirect.")
	}
	if c.site.Truncated {
		c.add("crawl.truncated", Info, "", fmt.Sprintf("the crawl stopped at max_pages (%d pages); the rest was not checked", len(c.site.Pages)), "Raise max_pages, or audit the built directory.")
	}
}

func (c *run) robots() {
	if !c.site.Robots.Found {
		c.add("robots.missing", Warn, "", "there is no robots.txt", "Publish /robots.txt with a Sitemap: line.")
		return
	}
	r := site.ParseRobots(c.site.Robots.Body)
	if len(r.Sitemaps) == 0 {
		c.add("robots.sitemap", Info, "", "robots.txt does not name the sitemap", "Add Sitemap: "+c.site.URL("/sitemap.xml")+".")
	}
	for _, bot := range []string{"Googlebot", "Bingbot"} {
		if !r.Allowed(bot, "/") {
			c.add("robots.blocks.all", Error, "", "robots.txt blocks "+bot+" from the whole site", "Remove the Disallow: / that applies to "+bot+".")
		}
	}
}

// hreflang checks that each locale's pages point at each other. It runs
// only when the config lists more than one locale.
func (c *run) hreflang() {
	if len(c.t.Locales) < 2 {
		return
	}
	for _, p := range c.indexable {
		if len(p.Hreflang) == 0 {
			c.add("hreflang.missing", Warn, p.Path, "the page links no language versions", `Add <link rel="alternate" hreflang="..."> for every locale and x-default.`)
			continue
		}
		if _, ok := p.Hreflang["x-default"]; !ok {
			c.add("hreflang.xdefault", Info, p.Path, "no x-default alternate", "Add hreflang=\"x-default\" pointing at the default-language page.")
		}
		own := c.locale(p.Path)
		if href, ok := p.Hreflang[own]; !ok || c.site.Resolve(p, href) != p.Path {
			c.add("hreflang.self", Warn, p.Path, "the page's own language ("+own+") does not point back at it", "Each page lists itself among its alternates.")
		}
		for code, href := range p.Hreflang {
			if code != "x-default" && !validLang(code) {
				c.add("hreflang.invalid", Error, p.Path, fmt.Sprintf("hreflang %q is not a language code", code), "Use ISO 639-1 codes, optionally with a region: en, ro, pt-br.")
				continue
			}
			other, ok := c.site.Page(c.site.Resolve(p, href))
			if !ok || other.Path == p.Path || code == "x-default" {
				continue
			}
			if back, ok := other.Hreflang[own]; !ok || c.site.Resolve(other, back) != p.Path {
				c.add("hreflang.reciprocal", Warn, p.Path, "an alternate does not link back", other.Path+" must list "+p.Path+" as hreflang=\""+own+"\"; Google ignores one-way pairs.")
			}
		}
	}
}

// locale is the page's language: its path prefix, or the default.
func (c *run) locale(path string) string {
	for _, l := range c.t.Locales[1:] {
		if path == "/"+l || strings.HasPrefix(path, "/"+l+"/") {
			return strings.ToLower(l)
		}
	}
	return strings.ToLower(c.t.Locales[0])
}

func validLang(code string) bool {
	lang, region, _ := strings.Cut(code, "-")
	return len(lang) == 2 && isLetters(lang) && (region == "" || len(region) == 2 && isLetters(region) || len(region) == 4 && isLetters(region))
}

func isLetters(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return r < 'a' || r > 'z' })
}

// content flags thin pages. Word counts include navigation and footer, so the
// bar is low on purpose.
func (c *run) content() {
	floor := c.t.MinWords
	switch {
	case floor < 0:
		return
	case floor == 0:
		floor = 150
	}
	for _, p := range c.indexable {
		if p.Words < floor {
			c.add("content.thin", Info, p.Path, fmt.Sprintf("the page has under %d words", floor), "Add what a reader needs to act: who it is for, what happens next, the facts they ask about.")
		}
	}
}
