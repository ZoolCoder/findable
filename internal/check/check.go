// Package check judges a site read against a target's config and returns
// findings. Every check is deterministic: the same site and config give the
// same findings in the same order, so a plan diff means something changed.
// It performs no I/O.
package check

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/site"
)

// Severity ranks a finding.
type Severity string

const (
	// Error keeps pages out of the index or out of rich results.
	Error Severity = "error"
	// Warn costs ranking, clicks or citations.
	Warn Severity = "warn"
	// Info is worth doing when the errors and warnings are gone.
	Info Severity = "info"
)

func (s Severity) rank() int { return slices.Index([]Severity{Error, Warn, Info}, s) }

// Finding is one problem, on one or more pages.
type Finding struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	// Pages are the URL paths the problem is on; empty for the site as a whole.
	Pages   []string `json:"pages,omitempty"`
	Message string   `json:"message"`
	Fix     string   `json:"fix"`
}

// Where names the pages for one line of output.
func (f Finding) Where() string {
	switch len(f.Pages) {
	case 0:
		return "site"
	case 1:
		return f.Pages[0]
	default:
		return fmt.Sprintf("%s (+%d more)", f.Pages[0], len(f.Pages)-1)
	}
}

// Run judges s against t. Findings with the same check and message are merged
// into one with every page listed.
func Run(s site.Site, t config.Target) []Finding {
	c := &run{site: s, t: t}
	c.index()
	for _, check := range []func(){c.crawl, c.meta, c.indexing, c.links, c.sitemap, c.redirects, c.canonicalTargets, c.robots, c.hreflang, c.schema, c.local, c.people, c.brand, c.queries, c.ai, c.content} {
		check()
	}
	return merge(c.out, t.Ignore)
}

type run struct {
	site      site.Site
	t         config.Target
	out       []Finding
	indexable []site.Page
}

func (c *run) add(check string, sev Severity, page, msg, fix string) {
	f := Finding{Check: check, Severity: sev, Message: msg, Fix: fix}
	if page != "" {
		f.Pages = []string{page}
	}
	c.out = append(c.out, f)
}

func (c *run) index() { c.indexable = Indexable(c.site) }

// Indexable are the pages meant to rank: not noindex, and canonical to
// themselves or naming no canonical.
func Indexable(s site.Site) []site.Page {
	var out []site.Page
	for _, p := range s.Pages {
		if !p.Noindex() && canonicalSelf(s, p) {
			out = append(out, p)
		}
	}
	return out
}

func canonicalSelf(s site.Site, p site.Page) bool {
	return p.Canonical == "" || s.Resolve(p, p.Canonical) == p.Path
}

func (c *run) canonicalSelf(p site.Page) bool { return canonicalSelf(c.site, p) }

func (c *run) allowedNoindex(path string) bool {
	return slices.ContainsFunc(c.t.Noindex, func(pat string) bool { return config.Match(pat, path, c.t.Locales) })
}

func merge(in []Finding, ignore []string) []Finding {
	var out []Finding
	at := map[string]int{}
	for _, f := range in {
		if slices.ContainsFunc(ignore, func(p string) bool { return strings.HasPrefix(f.Check, p) }) {
			continue
		}
		key := f.Check + "\x00" + f.Message
		if i, ok := at[key]; ok {
			out[i].Pages = append(out[i].Pages, f.Pages...)
			continue
		}
		at[key] = len(out)
		out = append(out, f)
	}
	for i := range out {
		slices.Sort(out[i].Pages)
		out[i].Pages = slices.Compact(out[i].Pages)
	}
	slices.SortStableFunc(out, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.Severity.rank(), b.Severity.rank()), cmp.Compare(a.Check, b.Check),
			cmp.Compare(a.Where(), b.Where()), cmp.Compare(a.Message, b.Message))
	})
	return out
}
