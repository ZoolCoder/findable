package check

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zoolcoder/findable/internal/config"
)

// now is the clock JobPosting expiry is judged by; tests replace it.
var now = time.Now

// localTypes are the schema.org LocalBusiness subtypes a site is likely to
// use; orgTypes adds the plain organisations.
var (
	localTypes = []string{"LocalBusiness", "EmploymentAgency", "ProfessionalService", "Store", "FinancialService",
		"LegalService", "MedicalBusiness", "FoodEstablishment", "Restaurant", "AutomotiveBusiness", "LodgingBusiness",
		"HomeAndConstructionBusiness", "TravelAgency", "RealEstateAgent"}
	orgTypes = append([]string{"Organization", "Corporation", "NGO", "EducationalOrganization", "OnlineBusiness"}, localTypes...)
)

// node is one top-level JSON-LD object.
type node map[string]any

func (n node) types() []string {
	switch t := n["@type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func (n node) is(want string) bool {
	return slices.ContainsFunc(n.types(), func(have string) bool {
		switch want {
		case "LocalBusiness":
			return slices.Contains(localTypes, have)
		case "Organization":
			return slices.Contains(orgTypes, have)
		}
		return have == want
	})
}

func (n node) has(key string) bool {
	switch v := n[key].(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	}
	return true
}

func (n node) str(key string) string {
	s, _ := n[key].(string)
	return strings.TrimSpace(s)
}

// nodes flattens the page's JSON-LD: arrays and @graph become one list.
func nodes(raws []json.RawMessage) []node {
	var out []node
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case []any:
			for _, e := range v {
				walk(e)
			}
		case map[string]any:
			if g, ok := v["@graph"]; ok {
				walk(g)
			}
			if _, ok := v["@type"]; ok {
				out = append(out, node(v))
			}
		}
	}
	for _, raw := range raws {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			walk(v)
		}
	}
	return out
}

func (c *run) schema() {
	for _, p := range c.site.Pages {
		for _, e := range p.JSONLDErrs {
			c.add("schema.invalid.json", Error, p.Path, "a JSON-LD block does not parse: "+e, "Fix the JSON; Google skips the whole block.")
		}
	}
	for _, p := range c.indexable {
		ns := nodes(p.JSONLD)
		for _, rule := range c.t.Schema {
			if !config.Match(rule.Path, p.Path, c.t.Locales) {
				continue
			}
			for _, want := range rule.Types {
				if !slices.ContainsFunc(ns, func(n node) bool { return n.is(want) }) {
					c.add("schema.expected", Warn, p.Path, "the page has no "+want+" structured data", "Add a "+want+" JSON-LD block (schema.org/"+want+").")
				}
			}
		}
		jobs := 0
		for _, n := range ns {
			switch {
			case n.is("JobPosting"):
				jobs++
				c.jobPosting(p.Path, n)
			case n.is("BreadcrumbList"):
				c.breadcrumb(p.Path, n)
			case n.is("LocalBusiness"):
				c.fields("schema.localbusiness.fields", Warn, p.Path, n, "name", "address")
				c.fields("schema.localbusiness.fields", Info, p.Path, n, "telephone", "url")
			case n.is("Organization"):
				c.fields("schema.organization.fields", Info, p.Path, n, "name", "url", "logo")
			}
		}
		if jobs > 1 {
			c.add("schema.jobposting.listing", Warn, p.Path, "the page carries more than one JobPosting",
				"Put JobPosting only on each job's own page; Google does not allow it on lists of jobs.")
		}
	}
}

func (c *run) fields(check string, sev Severity, path string, n node, keys ...string) {
	for _, k := range keys {
		if !n.has(k) {
			c.add(check, sev, path, fmt.Sprintf("%s has no %s", n.types()[0], k), "Add "+k+" to the "+n.types()[0]+" block.")
		}
	}
}

// jobPosting follows Google's JobPosting rules: the required properties,
// expiry, and the recommended ones that feed the job search filters.
func (c *run) jobPosting(path string, n node) {
	required := []string{"title", "description", "datePosted", "hiringOrganization"}
	if n.str("jobLocationType") == "TELECOMMUTE" {
		required = append(required, "applicantLocationRequirements")
	} else {
		required = append(required, "jobLocation")
	}
	for _, k := range required {
		if !n.has(k) {
			c.add("schema.jobposting.required", Error, path, "JobPosting has no "+k, "Add "+k+"; without it the job is not shown in Google's job search.")
		}
	}
	if org, ok := n["hiringOrganization"].(map[string]any); ok && !node(org).has("name") {
		c.add("schema.jobposting.required", Error, path, "JobPosting hiringOrganization has no name", "Name the hiring organisation.")
	}
	for _, k := range []string{"baseSalary", "employmentType", "validThrough"} {
		if !n.has(k) {
			c.add("schema.jobposting.recommended", Info, path, "JobPosting has no "+k, "Add "+k+": job search filters on it.")
		}
	}
	if until, ok := parseDate(n.str("validThrough")); ok && until.Before(now()) {
		c.add("schema.jobposting.expired", Info, path, "the job's validThrough has passed, so Google treats it as closed",
			"If the role is still open, set validThrough to the real closing date. If it is filled, say so on the page, or remove the page (404/410) or its JobPosting markup.")
	}
}

func parseDate(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (c *run) breadcrumb(path string, n node) {
	items, _ := n["itemListElement"].([]any)
	if len(items) == 0 {
		c.add("schema.breadcrumb.items", Warn, path, "BreadcrumbList has no itemListElement", "List each crumb as a ListItem.")
		return
	}
	for i, it := range items {
		li, _ := it.(map[string]any)
		pos, _ := li["position"].(float64)
		if int(pos) != i+1 {
			c.add("schema.breadcrumb.position", Warn, path, "BreadcrumbList positions do not count 1, 2, 3", "Number the ListItems from 1 in order.")
			return
		}
		if !node(li).has("name") && !hasNestedName(li["item"]) {
			c.add("schema.breadcrumb.items", Warn, path, "a breadcrumb has no name", "Give every ListItem a name.")
			return
		}
		if i < len(items)-1 && !node(li).has("item") {
			c.add("schema.breadcrumb.items", Warn, path, "a breadcrumb other than the last has no item URL", "Give every ListItem but the last an item URL.")
			return
		}
	}
}

func hasNestedName(v any) bool {
	m, ok := v.(map[string]any)
	return ok && node(m).has("name")
}

// local checks that the structured data names the business the way the
// config does: one name and the same phone numbers everywhere (NAP).
func (c *run) local() {
	b := c.t.Business
	if b == nil {
		return
	}
	phones := map[string]bool{}
	for _, p := range b.Phones {
		phones[digits(p)] = true
	}
	found := false
	for _, p := range c.site.Pages {
		for _, n := range nodes(p.JSONLD) {
			if !n.is("Organization") {
				continue
			}
			found = true
			// A branch may add its place to the name: "Acme — Leeds".
			if name := n.str("name"); name != "" && !sameBusiness(name, b.Name) {
				c.add("local.nap.mismatch", Warn, p.Path, fmt.Sprintf("structured data names the business %q, not %q", name, b.Name), "Use one name everywhere: on the site, Google Business Profile and directories.")
			}
			if tel := n.str("telephone"); tel != "" && len(phones) > 0 && !phones[digits(tel)] {
				c.add("local.nap.mismatch", Warn, p.Path, fmt.Sprintf("structured data lists phone %s, which is not in business.phones", tel), "Use the same numbers everywhere, or add it to the config.")
			}
		}
	}
	if !found {
		c.add("local.schema.missing", Warn, "", "no page carries Organization or LocalBusiness structured data", "Add an Organization block on the home page and a LocalBusiness block for each office.")
	}
}

func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// sameBusiness accepts the configured name, alone or followed by a branch.
func sameBusiness(name, want string) bool {
	name, want = strings.ToLower(squash(name)), strings.ToLower(squash(want))
	return name == want || strings.HasPrefix(name, want+" ") || strings.HasPrefix(name, want+",")
}

func squash(s string) string { return strings.Join(strings.Fields(s), " ") }
