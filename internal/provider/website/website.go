// Package website is the site provider: it reads a built site from disk or
// crawls a live one, judges it with the check package, and plans the files
// findable can write itself (llms.txt and the AI crawler block in
// robots.txt). Everything else it finds is a MANUAL action: a fix for a
// person or an agent to make in the site's source.
//
// Writes go only to the target's write_dir, through os.Root, so a symlink
// there cannot lead a write outside it.
package website

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/zoolcoder/findable/internal/check"
	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/plan"
	"github.com/zoolcoder/findable/internal/site"
)

// UserAgent identifies findable's crawl, so a site owner can tell it apart.
const UserAgent = "findable (+https://github.com/zoolcoder/findable)"

type Provider struct {
	Crawler site.Crawler
}

func New() Provider { return Provider{Crawler: site.Crawler{UserAgent: UserAgent}} }

// Read loads the target's site: the directory at Path, or a crawl of URL.
func (p Provider) Read(ctx context.Context, t config.Target) (site.Site, error) {
	base, err := t.Base()
	if err != nil {
		return site.Site{}, err
	}
	if t.Path != "" {
		s, err := site.LoadDir(t.Path, base)
		if err == nil && len(s.Pages) == 0 {
			err = fmt.Errorf("%s holds no .html pages; is the site built?", t.Path)
		}
		return s, err
	}
	c := p.Crawler
	c.Max = t.MaxPages
	return c.Crawl(ctx, base)
}

// Audit is every finding for the target, with its pages.
func (p Provider) Audit(ctx context.Context, t config.Target) ([]check.Finding, error) {
	s, err := p.Read(ctx, t)
	if err != nil {
		return nil, err
	}
	return check.Run(s, t), nil
}

// Plan writes llms.txt and the robots.txt block when they differ, and lists
// every finding as MANUAL.
func (p Provider) Plan(ctx context.Context, t config.Target) ([]plan.Action, error) {
	s, err := p.Read(ctx, t)
	if err != nil {
		return nil, err
	}
	var actions []plan.Action
	if t.WriteDir != "" {
		gen, err := generated(s, t)
		if err != nil {
			return nil, err
		}
		actions = append(actions, gen...)
	}
	for _, f := range check.Run(s, t) {
		actions = append(actions, plan.Action{Op: plan.OpManual, Target: t.Name, Resource: f.Check,
			Detail: fmt.Sprintf("[%s] %s: %s. Fix: %s", f.Severity, f.Where(), f.Message, f.Fix)})
	}
	return actions, nil
}

// Status counts pages and findings by severity.
func (p Provider) Status(ctx context.Context, t config.Target) (string, error) {
	s, err := p.Read(ctx, t)
	if err != nil {
		return "", err
	}
	count := map[check.Severity]int{}
	for _, f := range check.Run(s, t) {
		count[f.Severity]++
	}
	return fmt.Sprintf("%d pages, %d errors, %d warnings, %d notes", len(s.Pages), count[check.Error], count[check.Warn], count[check.Info]), nil
}

func generated(s site.Site, t config.Target) ([]plan.Action, error) {
	var out []plan.Action
	if t.LLMsTxt != nil {
		a, err := fileAction(t, "llms.txt", LLMsTxt(s, t), "generated from the site's titles and descriptions")
		if err != nil {
			return nil, err
		}
		out = append(out, a...)
	}
	a, err := robotsAction(s, t)
	if err != nil {
		return nil, err
	}
	return append(out, a...), nil
}

func robotsAction(s site.Site, t config.Target) ([]plan.Action, error) {
	have, err := readFile(t.WriteDir, "robots.txt")
	block := RobotsBlock(t.AICrawlers.Policies())
	switch {
	case errors.Is(err, fs.ErrNotExist) && s.Robots.Found:
		// The build writes robots.txt from somewhere else; findable cannot
		// own a file it does not see.
		if block == "" {
			return nil, nil
		}
		return []plan.Action{{Op: plan.OpManual, Target: t.Name, Resource: "robots.txt",
			Detail: "the build writes robots.txt outside write_dir; add these lines to it: " + strings.ReplaceAll(block, "\n", " | ")}}, nil
	case errors.Is(err, fs.ErrNotExist):
		body := "User-agent: *\nAllow: /\n\nSitemap: " + s.URL("/sitemap.xml") + "\n"
		if block != "" {
			body += "\n" + block
		}
		return fileAction(t, "robots.txt", body, "allows every crawler and names the sitemap")
	case err != nil:
		return nil, err
	}
	want := ReplaceBlock(have, block)
	if want == have {
		return nil, nil
	}
	return []plan.Action{write(t, "robots.txt", want, plan.OpUpdate, "AI crawler block, from ai_crawlers")}, nil
}

func fileAction(t config.Target, name, want, why string) ([]plan.Action, error) {
	have, err := readFile(t.WriteDir, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []plan.Action{write(t, name, want, plan.OpCreate, why)}, nil
	case err != nil:
		return nil, err
	case have != want:
		return []plan.Action{write(t, name, want, plan.OpUpdate, why)}, nil
	}
	return nil, nil
}

func readFile(dir, name string) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(name)
	return string(b), err
}

func write(t config.Target, name, content string, op plan.Op, why string) plan.Action {
	return plan.Action{Op: op, Target: t.Name, Resource: name,
		Detail: fmt.Sprintf("%s, %d bytes; rebuild and deploy to publish it", why, len(content)),
		Do: func(context.Context) error {
			if err := os.MkdirAll(t.WriteDir, 0o755); err != nil {
				return err
			}
			root, err := os.OpenRoot(t.WriteDir)
			if err != nil {
				return err
			}
			defer func() { _ = root.Close() }()
			return root.WriteFile(name, []byte(content), 0o644)
		}}
}

const (
	blockStart = "# findable:start"
	blockEnd   = "# findable:end"
)

// RobotsBlock is the robots.txt lines for the crawler kinds the policy
// blocks, or "" when it blocks none. Allowed kinds get no lines: a named
// group would replace the * group's rules for that crawler, and with them
// any Disallow the owner wrote for everyone.
func RobotsBlock(policy config.AICrawlers) string {
	want := map[string]string{"search": policy.Search, "user": policy.User, "training": policy.Training}
	var agents []string
	for _, kind := range check.Kinds {
		if want[kind] == "block" {
			agents = append(agents, check.Crawlers[kind]...)
		}
	}
	if len(agents) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(blockStart + " (written by findable from ai_crawlers; edit the config, not these lines)\n")
	for _, a := range agents {
		b.WriteString("User-agent: " + a + "\n")
	}
	b.WriteString("Disallow: /\n" + blockEnd + "\n")
	return b.String()
}

// ReplaceBlock swaps findable's block in a robots.txt body, appends it when
// there is none, and removes it when block is "".
func ReplaceBlock(body, block string) string {
	lines := strings.SplitAfter(body, "\n")
	start := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, blockStart) })
	end := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, blockEnd) })
	if start >= 0 && end > start {
		rest := strings.Join(lines[:start], "") + block + strings.Join(lines[end+1:], "")
		if block == "" {
			rest = strings.Join(lines[:start], "") + strings.TrimPrefix(strings.Join(lines[end+1:], ""), "\n")
			return strings.TrimRight(rest, "\n") + "\n"
		}
		return rest
	}
	if block == "" {
		return body
	}
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if body != "" && !strings.HasSuffix(body, "\n\n") {
		body += "\n"
	}
	return body + block
}

// LLMsTxt renders llms.txt (llmstxt.org) from the indexable pages in the
// default locale, under the config's sections.
func LLMsTxt(s site.Site, t config.Target) string {
	cfg := t.LLMsTxt
	var b strings.Builder
	b.WriteString("# " + cfg.Title + "\n")
	if cfg.Summary != "" {
		b.WriteString("\n> " + cfg.Summary + "\n")
	}
	if cfg.Details != "" {
		b.WriteString("\n" + strings.TrimSpace(cfg.Details) + "\n")
	}
	sections := cfg.Sections
	if len(sections) == 0 {
		sections = []config.LLMsSection{{Name: "Pages", Path: "/**"}}
	}
	listed := map[string]bool{}
	var main, optional []string
	for _, sec := range sections {
		var items []string
		for _, p := range check.Indexable(s) {
			if listed[p.Path] || config.StripLocale(p.Path, t.Locales) != p.Path || !config.Match(sec.Path, p.Path, nil) {
				continue
			}
			listed[p.Path] = true
			item := "- [" + linkText(p) + "](" + s.URL(p.Path) + ")"
			if p.Description != "" {
				item += ": " + p.Description
			}
			items = append(items, item)
		}
		if len(items) == 0 {
			continue
		}
		if sec.Optional {
			optional = append(optional, items...)
			continue
		}
		main = append(main, "\n## "+sec.Name+"\n\n"+strings.Join(items, "\n")+"\n")
	}
	for _, m := range main {
		b.WriteString(m)
	}
	if len(optional) > 0 {
		b.WriteString("\n## Optional\n\n" + strings.Join(optional, "\n") + "\n")
	}
	return b.String()
}

func linkText(p site.Page) string {
	t := p.Title
	if t == "" {
		t = p.Path
	}
	return strings.NewReplacer("[", "(", "]", ")").Replace(t)
}
