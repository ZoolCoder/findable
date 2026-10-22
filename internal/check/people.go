package check

import (
	"fmt"
	"slices"
	"strings"

	"github.com/zoolcoder/findable/internal/config"
)

// articleTypes are the schema.org Article subtypes a site is likely to use.
var articleTypes = []string{"Article", "BlogPosting", "NewsArticle", "TechArticle", "ScholarlyArticle", "Report", "LiveBlogPosting"}

// aboutPaths are where a page about the people behind a site usually lives,
// locale prefix stripped. A page of @type AboutPage counts wherever it is.
var aboutPaths = []string{"/about", "/about-us", "/team", "/our-team", "/company", "/who-we-are", "/people"}

// article follows Google's Article guidelines: a named author and dates.
// Search engines and AI answers weigh who wrote a page and when.
func (c *run) article(path string, n node, ids idIndex) {
	kind := articleKind(n)
	if !ids.named(n["author"]) {
		c.add("schema.article.author", Warn, path, fmt.Sprintf("%q has no named author", kind),
			"Add author: {\"@type\":\"Person\",\"name\":\"...\",\"url\":\"...\"}, linking the author's own page or profile.")
	}
	if !n.has("datePublished") {
		c.add("schema.article.dates", Warn, path, fmt.Sprintf("%q has no datePublished", kind),
			"Add datePublished as an ISO 8601 date (2026-10-23 or 2026-10-23T09:00:00+02:00), and dateModified when the text changes.")
	}
	for _, k := range []string{"datePublished", "dateModified"} {
		if v := n.str(k); v != "" && !isoDate(v) {
			c.add("schema.article.dates", Warn, path, fmt.Sprintf("%q %s is not an ISO 8601 date: %q", kind, k, v), "Write it as 2026-10-23 or 2026-10-23T09:00:00+02:00.")
		}
	}
	if !n.has("dateModified") {
		c.add("schema.article.dates", Info, path, fmt.Sprintf("%q has no dateModified", kind), "Add dateModified: readers and AI answers prefer a page that says when it was last checked.")
	}
}

// articleKind is the type that made the node an Article, not whichever type
// the page happens to list first.
func articleKind(n node) string {
	types := n.types()
	if i := slices.IndexFunc(types, func(t string) bool { return slices.Contains(articleTypes, t) }); i >= 0 {
		return types[i]
	}
	return types[0]
}

func isoDate(s string) bool {
	_, ok := parseDeadline(s)
	return ok
}

// idIndex finds the nodes of one page by @id, so a property that only
// references a Person elsewhere in the same @graph can still be read.
type idIndex map[string]node

func indexByID(ns []node) idIndex {
	ids := idIndex{}
	for _, n := range ns {
		if id := n.str("@id"); id != "" {
			ids[id] = n
		}
	}
	return ids
}

// named reports whether a property holds at least one thing with a name: a
// plain name, a Person or Organization, or a reference to one, alone or in a
// list.
func (ids idIndex) named(v any) bool {
	switch v := v.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]any:
		n := node(v)
		if n.has("name") {
			return true
		}
		if id := n.str("@id"); id != "" {
			return ids[id].has("name")
		}
	case []any:
		return slices.ContainsFunc(v, ids.named)
	}
	return false
}

// namedPerson reports whether a property holds a Person with a name, alone or
// in a list.
func namedPerson(v any) bool {
	switch v := v.(type) {
	case map[string]any:
		n := node(v)
		return n.is("Person") && n.has("name")
	case []any:
		return slices.ContainsFunc(v, namedPerson)
	}
	return false
}

// people looks for the page that says who is behind the site and whether
// its structured data names them. Every page counts: a noindex about page
// is reported by the indexing checks, not as missing.
func (c *run) people() {
	found := false
	for _, p := range c.site.Pages {
		ns := nodes(p.JSONLD)
		ids := indexByID(ns)
		if !isAboutPath(p.Path, c.t.Locales) && !slices.ContainsFunc(ns, func(n node) bool { return n.is("AboutPage") }) {
			continue
		}
		found = true
		if !slices.ContainsFunc(ns, func(n node) bool { return ids.namesPerson(n) }) {
			c.add("about.people.missing", Info, p.Path, "the about page names no person in its structured data",
				"Add a Person block (name, jobTitle, sameAs) for each of the people behind the site, or list them as the Organization's founder or employee.")
		}
	}
	if !found {
		c.add("about.missing", Info, "", "no page says who is behind the site",
			"Add an about page at /about: who runs the site, with names and roles, and a Person block for each.")
	}
}

func isAboutPath(path string, locales []string) bool {
	path = strings.TrimSuffix(config.StripLocale(path, locales), "/")
	return slices.Contains(aboutPaths, path)
}

// namesPerson accepts a Person node, a Person the node is about (an
// AboutPage's mainEntity), or people listed on an organisation.
func (ids idIndex) namesPerson(n node) bool {
	if n.is("Person") && n.has("name") {
		return true
	}
	if slices.ContainsFunc([]string{"mainEntity", "about", "mainEntityOfPage", "author", "creator"}, func(k string) bool { return namedPerson(n[k]) }) {
		return true
	}
	return slices.ContainsFunc([]string{"founder", "founders", "employee", "employees", "member", "members"}, func(k string) bool { return ids.named(n[k]) })
}
