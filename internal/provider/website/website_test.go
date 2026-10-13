package website

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/plan"
	"github.com/zoolcoder/findable/internal/site"
	"github.com/zoolcoder/findable/internal/sitetest"
)

func fixture(t *testing.T) (config.Target, string) {
	t.Helper()
	root := t.TempDir()
	sitetest.Write(t, root)
	return config.Target{Name: "example", Provider: "site", Path: filepath.Join(root, "dist"), BaseURL: sitetest.Base,
		WriteDir: filepath.Join(root, "public"), AICrawlers: config.AICrawlers{Training: "block"},
		LLMsTxt: &config.LLMsTxt{Title: "Example", Summary: "Widgets."}}, root
}

func ops(actions []plan.Action) []string {
	var out []string
	for _, a := range actions {
		out = append(out, string(a.Op)+" "+a.Resource)
	}
	return out
}

func TestPlanApplyConverges(t *testing.T) {
	ctx := context.Background()
	tg, root := fixture(t)
	actions, err := New().Plan(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ops(actions), ", "); got != "CREATE llms.txt, UPDATE robots.txt, MANUAL ai.crawler.allowed" {
		t.Fatalf("plan: %s", got)
	}
	for _, a := range actions {
		if a.Do != nil {
			if err := a.Do(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	robots, _ := os.ReadFile(filepath.Join(root, "public", "robots.txt"))
	if !strings.HasPrefix(string(robots), sitetest.Robots) || !strings.Contains(string(robots), "User-agent: GPTBot\n") {
		t.Fatalf("robots.txt:\n%s", robots)
	}
	llms, _ := os.ReadFile(filepath.Join(root, "public", "llms.txt"))
	if !strings.Contains(string(llms), "- [About Example and the people behind it](https://example.com/about): Who makes") {
		t.Fatalf("llms.txt:\n%s", llms)
	}
	again, err := New().Plan(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	// The block is in public/ now; dist/ shows it after the next build.
	if got := strings.Join(ops(again), ", "); got != "MANUAL ai.crawler.allowed" {
		t.Fatalf("second plan: %s", got)
	}
}

func TestRobotsOutsideWriteDirIsManual(t *testing.T) {
	tg, root := fixture(t)
	_ = os.Remove(filepath.Join(root, "public", "robots.txt"))
	tg.LLMsTxt = nil
	actions, err := New().Plan(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	if actions[0].Op != plan.OpManual || actions[0].Resource != "robots.txt" || !strings.Contains(actions[0].Detail, "User-agent: CCBot") {
		t.Fatalf("%+v", actions[0])
	}
	_ = os.Remove(filepath.Join(root, "dist", "robots.txt"))
	actions, _ = New().Plan(context.Background(), tg)
	if actions[0].Op != plan.OpCreate || actions[0].Resource != "robots.txt" {
		t.Fatalf("%+v", actions[0])
	}
}

func TestReadNeedsPages(t *testing.T) {
	tg := config.Target{Name: "x", Path: t.TempDir(), BaseURL: sitetest.Base}
	if _, err := New().Read(context.Background(), tg); err == nil || !strings.Contains(err.Error(), "is the site built") {
		t.Fatalf("empty dir: %v", err)
	}
}

func TestStatus(t *testing.T) {
	tg, _ := fixture(t)
	got, err := New().Status(context.Background(), tg)
	if err != nil || got != "2 pages, 0 errors, 1 warnings, 0 notes" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestRobotsBlock(t *testing.T) {
	if RobotsBlock(config.AICrawlers{}.Policies()) != "" {
		t.Fatal("default policy writes a block")
	}
	block := RobotsBlock(config.AICrawlers{Training: "block"})
	r := site.ParseRobots("User-agent: *\nDisallow: /private\n\n" + block)
	for agent, want := range map[string]bool{"GPTBot": false, "Google-Extended": false, "OAI-SearchBot": true, "Googlebot": true} {
		if r.Allowed(agent, "/") != want {
			t.Errorf("%s allowed: %v", agent, !want)
		}
	}
	if r.Allowed("OAI-SearchBot", "/private") {
		t.Error("the block dropped the * rules for allowed crawlers")
	}
}

func TestReplaceBlock(t *testing.T) {
	block := "# findable:start\nUser-agent: GPTBot\nDisallow: /\n# findable:end\n"
	other := "# findable:start\nUser-agent: CCBot\nDisallow: /\n# findable:end\n"
	for _, c := range []struct{ name, body, block, want string }{
		{"append", "User-agent: *\nAllow: /", block, "User-agent: *\nAllow: /\n\n" + block},
		{"append to empty", "", block, block},
		{"replace", "a\n\n" + other + "b\n", block, "a\n\n" + block + "b\n"},
		{"remove", "a\n\n" + block, "", "a\n"},
		{"nothing to remove", "a\n", "", "a\n"},
		{"unchanged", "a\n\n" + block, block, "a\n\n" + block},
	} {
		if got := ReplaceBlock(c.body, c.block); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLLMsTxt(t *testing.T) {
	base, _ := url.Parse(sitetest.Base)
	page := func(path, title string) site.Page {
		return site.Page{Path: path, Title: title, Description: "About " + path + "."}
	}
	s := site.Site{Base: base, Pages: []site.Page{
		page("/", "Home"), page("/jobs/welder", "Welder [Leeds]"), page("/news/a", "A"),
		page("/ro/jobs/welder", "Sudor"), {Path: "/hidden", MetaRobots: "noindex"},
	}}
	got := LLMsTxt(s, config.Target{Locales: []string{"en", "ro"}, LLMsTxt: &config.LLMsTxt{Title: "Example", Summary: "Widgets.",
		Sections: []config.LLMsSection{{Name: "Jobs", Path: "/jobs/**"}, {Name: "News", Path: "/news/**", Optional: true}, {Name: "More", Path: "/**"}}}})
	want := `# Example

> Widgets.

## Jobs

- [Welder (Leeds)](https://example.com/jobs/welder): About /jobs/welder.

## More

- [Home](https://example.com/): About /.

## Optional

- [A](https://example.com/news/a): About /news/a.
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
