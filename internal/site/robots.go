package site

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Robots is a parsed robots.txt (RFC 9309).
type Robots struct {
	Groups   []RobotsGroup
	Sitemaps []string
}

// RobotsGroup is one or more user-agent lines and the rules under them.
type RobotsGroup struct {
	Agents []string
	Rules  []RobotsRule
}

// RobotsRule is one allow or disallow line.
type RobotsRule struct {
	Allow   bool
	Pattern string
}

// ParseRobots reads a robots.txt body. Lines it does not understand are
// skipped, as crawlers skip them.
func ParseRobots(body string) Robots {
	var (
		r         Robots
		cur       *RobotsGroup
		lastAgent bool
	)
	body = strings.TrimPrefix(body, "\ufeff")
	for line := range strings.Lines(body) {
		line, _, _ = strings.Cut(line, "#")
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, val = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(val)
		switch key {
		case "user-agent":
			if !lastAgent {
				r.Groups = append(r.Groups, RobotsGroup{})
				cur = &r.Groups[len(r.Groups)-1]
			}
			cur.Agents = append(cur.Agents, strings.ToLower(val))
			lastAgent = true
			continue
		case "allow", "disallow":
			// An empty disallow allows everything: no rule at all.
			if cur != nil && val != "" {
				cur.Rules = append(cur.Rules, RobotsRule{Allow: key == "allow", Pattern: encodeRobotsPath(val)})
			}
			lastAgent = false
		case "sitemap":
			r.Sitemaps = append(r.Sitemaps, val)
		}
	}
	return r
}

// rules returns the rules for agent: every group naming it, merged, or the
// groups for * when none does.
func (r Robots) rules(agent string) (rules []RobotsRule, named bool) {
	agent = strings.ToLower(agent)
	for _, want := range []string{agent, "*"} {
		for _, g := range r.Groups {
			if slices.Contains(g.Agents, want) {
				rules = append(rules, g.Rules...)
				named = true
			}
		}
		if named {
			return rules, want == agent
		}
	}
	return nil, false
}

// Named reports whether a group names agent itself rather than leaving it
// to the * group.
func (r Robots) Named(agent string) bool {
	_, named := r.rules(agent)
	return named
}

// Allowed reports whether agent may fetch path. The longest matching rule
// wins; on a tie, allow wins.
func (r Robots) Allowed(agent, path string) bool {
	rules, _ := r.rules(agent)
	path = encodeRobotsPath(path)
	best, allow := -1, true
	for _, rule := range rules {
		if !robotsMatch(rule.Pattern, path) {
			continue
		}
		n := len(rule.Pattern)
		if n > best || (n == best && rule.Allow) {
			best, allow = n, rule.Allow
		}
	}
	return allow
}

// encodeRobotsPath puts a pattern or path in the form RFC 9309 matches on:
// non-ASCII bytes percent-encoded, the hex of every escape upper case.
func encodeRobotsPath(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 0x80:
			fmt.Fprintf(&b, "%%%02X", c)
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			b.WriteString("%" + strings.ToUpper(s[i+1:i+3]))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func robotsMatch(pattern, path string) bool {
	if !strings.ContainsAny(pattern, "*$") {
		return strings.HasPrefix(path, pattern)
	}
	var b strings.Builder
	b.WriteString("^")
	end := strings.HasSuffix(pattern, "$")
	for _, part := range strings.Split(strings.TrimSuffix(pattern, "$"), "*") {
		b.WriteString(regexp.QuoteMeta(part))
		b.WriteString(".*")
	}
	re := strings.TrimSuffix(b.String(), ".*")
	if end {
		re += "$"
	}
	ok, _ := regexp.MatchString(re, path)
	return ok
}
