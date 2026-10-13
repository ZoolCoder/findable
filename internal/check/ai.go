package check

import (
	"regexp"
	"strings"

	"github.com/zoolcoder/findable/internal/site"
)

// Crawlers are the AI user agents by what they do with a page, from each
// vendor's own documentation. Search crawlers decide whether an AI answer
// can cite the site. User agents fetch a page because a person asked an
// assistant to read it. Training crawlers collect model training data;
// Google-Extended controls Gemini training and grounding and does not touch
// Google Search.
var Crawlers = map[string][]string{
	"search":   {"OAI-SearchBot", "Claude-SearchBot", "PerplexityBot"},
	"user":     {"ChatGPT-User", "Claude-User", "Perplexity-User"},
	"training": {"GPTBot", "ClaudeBot", "CCBot", "Google-Extended", "Applebot-Extended", "Meta-ExternalAgent", "Bytespider"},
}

// Kinds is Crawlers' keys in a stable order.
var Kinds = []string{"search", "user", "training"}

var mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

func (c *run) ai() {
	robots := site.ParseRobots(c.site.Robots.Body)
	policy := c.t.AICrawlers.Policies()
	for _, kind := range Kinds {
		want := map[string]string{"search": policy.Search, "user": policy.User, "training": policy.Training}[kind]
		var wrong []string
		for _, agent := range Crawlers[kind] {
			allowed := robots.Allowed(agent, "/")
			if (want == "allow" && !allowed) || (want == "block" && allowed) {
				wrong = append(wrong, agent)
			}
		}
		switch {
		case len(wrong) == 0:
		case want == "allow":
			c.add("ai.crawler.blocked", Warn, "", "robots.txt blocks AI "+kind+" crawlers: "+strings.Join(wrong, ", "),
				"Blocked "+kind+" crawlers cannot read the site, so AI answers cannot cite it. Allow them, or set ai_crawlers."+kind+": block if that is the owner's choice.")
		default:
			c.add("ai.crawler.allowed", Warn, "", "robots.txt lets AI "+kind+" crawlers in, but the config says block: "+strings.Join(wrong, ", "),
				"Run apply to write the block into robots.txt, or change ai_crawlers."+kind+".")
		}
	}
	c.llms()
}

// llms checks llms.txt (llmstxt.org). It is a proposal few crawlers read
// yet, so a missing file is only a note.
func (c *run) llms() {
	f := c.site.LLMs
	if !f.Found {
		if c.t.LLMsTxt == nil {
			c.add("ai.llms.missing", Info, "", "there is no llms.txt", "Optional: add llms_txt: to the config and apply writes one from the site's own titles and descriptions.")
		}
		return
	}
	first := ""
	for line := range strings.Lines(f.Body) {
		if first = strings.TrimSpace(line); first != "" {
			break
		}
	}
	if !strings.HasPrefix(first, "# ") {
		c.add("ai.llms.format", Warn, "", "llms.txt does not open with a # title line", "Start with \"# <site name>\", then a > summary.")
	}
	if c.site.Live {
		return
	}
	for _, m := range mdLink.FindAllStringSubmatch(f.Body, -1) {
		lp := c.site.LocalPath("/", m[1])
		if lp == "" || site.LooksLikeFile(lp) {
			continue
		}
		if _, ok := c.site.Page(lp); !ok {
			c.add("ai.llms.link", Warn, lp, "llms.txt links a path with no page", "Regenerate llms.txt, or fix the link.")
		}
	}
}
