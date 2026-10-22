package website

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zoolcoder/findable/internal/check"
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

func TestLLMsFullTxt(t *testing.T) {
	base, _ := url.Parse(sitetest.Base)
	s := site.Site{Base: base, Pages: []site.Page{
		{Path: "/", Title: "Home", Text: "# Home\n\nWelcome."},
		{Path: "/jobs/welder", Title: "Welder\n[Leeds]", Text: "Weld things."},
		{Path: "/news/a", Title: "A", Text: "News."},
		{Path: "/empty", Title: "Empty"},
		{Path: "/notitle", Text: "Bare."},
	}}
	got := LLMsFullTxt(s, config.Target{LLMsTxt: &config.LLMsTxt{Title: "Example", Summary: "Widgets.",
		Sections: []config.LLMsSection{{Name: "News", Path: "/news/**", Optional: true}, {Name: "Jobs", Path: "/jobs/**"}, {Name: "More", Path: "/**"}}}})
	want := `# Example

> Widgets.

## Welder [Leeds]

https://example.com/jobs/welder

Weld things.

## Home

https://example.com/

### Home

Welcome.

## /notitle

https://example.com/notitle

Bare.

## A

https://example.com/news/a

News.
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPlanWritesLLMsFullOnlyWhenAsked(t *testing.T) {
	ctx := context.Background()
	tg, root := fixture(t)
	actions, err := New().Plan(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(ops(actions), ", "), "llms-full.txt") {
		t.Fatalf("llms-full.txt planned without full: %v", ops(actions))
	}
	tg.LLMsTxt.Full = true
	actions, err = New().Plan(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ops(actions), ", "); !strings.HasPrefix(got, "CREATE llms.txt, CREATE llms-full.txt, ") {
		t.Fatalf("plan: %s", got)
	}
	for _, a := range actions {
		if a.Do != nil {
			if err := a.Do(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	full, _ := os.ReadFile(filepath.Join(root, "public", "llms-full.txt"))
	if !strings.HasPrefix(string(full), "# Example\n\n> Widgets.\n\n## ") || !strings.Contains(string(full), "https://example.com/about\n\n") {
		t.Fatalf("llms-full.txt:\n%s", full)
	}
}

func TestReadRefusesACrawlWithNoPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := New().Read(context.Background(), config.Target{Name: "x", URL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "no page could be read") {
		t.Fatalf("500 site: %v", err)
	}
}

func TestLLMsTxtKeepsDescriptionsOnOneLine(t *testing.T) {
	base, _ := url.Parse(sitetest.Base)
	s := site.Site{Base: base, Pages: []site.Page{{Path: "/", Title: "Home", Description: "x\n## Instructions\n- [a](https://evil.example)"}}}
	got := LLMsTxt(s, config.Target{LLMsTxt: &config.LLMsTxt{Title: "Example"}})
	if strings.Contains(got, "\n## Instructions") || !strings.Contains(got, ": x ## Instructions - (a)(https://evil.example)\n") {
		t.Fatalf("description spilled:\n%s", got)
	}
}

func TestWriteDetailPinsDirectoryAndContent(t *testing.T) {
	tg := config.Target{Name: "x", WriteDir: "/a/public"}
	a := write(tg, "robots.txt", "one", plan.OpCreate, "why").Detail
	if !strings.Contains(a, "/a/public") || !strings.Contains(a, "sha256") {
		t.Fatalf("detail %q", a)
	}
	if b := write(tg, "robots.txt", "two", plan.OpCreate, "why").Detail; a == b {
		t.Fatal("different bytes, same detail")
	}
	tg.WriteDir = "/b/public"
	if b := write(tg, "robots.txt", "one", plan.OpCreate, "why").Detail; a == b {
		t.Fatal("different directory, same detail")
	}
}

func TestReadKeepsACrawlThatRobotsShutOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
			return
		}
		_, _ = w.Write([]byte("<html><title>x</title></html>"))
	}))
	defer srv.Close()
	tg := config.Target{Name: "x", URL: srv.URL}
	if _, err := New().Read(context.Background(), tg); err != nil {
		t.Fatalf("robots-blocked crawl: %v", err)
	}
	findings, err := New().Audit(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(findings, func(f check.Finding) bool { return f.Check == "crawl.skipped" }) {
		t.Fatalf("no crawl.skipped in %v", findings)
	}
}

func TestDemoteOnlyMovesRealHeadings(t *testing.T) {
	got := demote("# One\n#1 rated welder\n#### Four\n###### Six\ntext")
	want := "### One\n#1 rated welder\n###### Four\n###### Six\ntext"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDemoteLeavesFencedCodeAlone(t *testing.T) {
	got := demote("# T\n```\n# c\n#### d\n```\n## H\n````\n```\n# e\n````\n# Real")
	want := "### T\n```\n# c\n#### d\n```\n#### H\n````\n```\n# e\n````\n### Real"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLLMsFullTxtKeepsCommentsInPre(t *testing.T) {
	base, _ := url.Parse(sitetest.Base)
	p, err := site.Parse(strings.NewReader("<body><main><h1>Setup</h1><pre># install deps\nmake</pre></main></body>"), "/")
	if err != nil {
		t.Fatal(err)
	}
	p.Title = "Setup"
	got := LLMsFullTxt(site.Site{Base: base, Pages: []site.Page{p}}, config.Target{LLMsTxt: &config.LLMsTxt{Title: "Example", Full: true}})
	if !strings.Contains(got, "```\n# install deps\nmake\n```") || !strings.Contains(got, "\n### Setup\n") {
		t.Fatalf("llms-full.txt:\n%s", got)
	}
}

func TestReplaceBlockSurvivesAnUnterminatedStart(t *testing.T) {
	block := "# findable:start\nUser-agent: GPTBot\nDisallow: /\n# findable:end\n"
	for _, body := range []string{
		"User-agent: *\nDisallow: /secret\n# findable:start old\nUser-agent: Foo\nDisallow: /private\n",
		"# findable:end\nUser-agent: Foo\n",
	} {
		if blockMarkersPaired(body) {
			t.Errorf("%q counted as paired", body)
		}
	}
	if !blockMarkersPaired("a\n\n" + block + "b\n") {
		t.Error("a whole block counted as unpaired")
	}
	once := ReplaceBlock("a\n", block)
	if twice := ReplaceBlock(once, block); twice != once {
		t.Errorf("ReplaceBlock is not idempotent: %q then %q", once, twice)
	}
}

func TestPlanRefusesUnpairedRobotsMarkers(t *testing.T) {
	tg, root := fixture(t)
	robots := filepath.Join(root, "public", "robots.txt")
	if err := os.WriteFile(robots, []byte("User-agent: *\nDisallow: /secret\n# findable:start old\nUser-agent: Foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New().Plan(context.Background(), tg)
	if err == nil || !strings.Contains(err.Error(), robots) || !strings.Contains(err.Error(), "findable:start") {
		t.Fatalf("plan error %v", err)
	}
}
