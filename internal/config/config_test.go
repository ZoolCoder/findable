package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	cfg, err := Parse([]byte(`
version: 1
targets:
  - name: shop
    provider: site
    path: dist
    base_url: https://example.com
    write_dir: public
    max_pages: 50
    noindex: [/admin]
    ignore: [/tmp/**]
    min_words: 120
    locales: [en, ro]
    ai_crawlers: {training: block}
    schema:
      - {path: /jobs/*, types: [JobPosting]}
    llms_txt:
      title: Shop
      full: true
      sections:
        - {name: Jobs, path: /jobs/**}
    brand: Shop Co
    queries:
      - {query: shop software, path: /}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Names(); len(got) != 1 || got[0] != "shop" {
		t.Fatalf("names %v", got)
	}
	tg, _ := cfg.Find("shop")
	if p := tg.AICrawlers.Policies(); p.Search != "allow" || p.Training != "block" {
		t.Fatalf("policies %+v", p)
	}
	if b, err := tg.Base(); err != nil || b.String() != "https://example.com" {
		t.Fatalf("base %v %v", b, err)
	}
	if tg.MaxPages != 50 || tg.WriteDir != "public" || tg.Noindex[0] != "/admin" || tg.Ignore[0] != "/tmp/**" || tg.MinWords != 120 {
		t.Fatalf("fields %+v", tg)
	}
	if !tg.LLMsTxt.Full {
		t.Fatal("llms_txt.full not read")
	}
	if tg.SearchName() != "Shop Co" || len(tg.Queries) != 1 {
		t.Fatalf("brand %q queries %v", tg.SearchName(), tg.Queries)
	}
}

func TestParseReportsEveryProblem(t *testing.T) {
	_, err := Parse([]byte(`
version: 2
targets:
  - name: Bad_Name
    provider: s3
  - name: ok
    provider: site
    path: dist
    url: https://a.example
  - name: ok
    provider: site
    path: dist
  - name: crawlers
    provider: site
    url: ftp://x
    locales: [English]
    ai_crawlers: {search: maybe}
    llms_txt: {}
    queries: [{query: shop}]
  - {name: extras, provider: site, path: dist, base_url: https://e.example, schema: [{path: jobs, types: []}], noindex: [admin], max_pages: -1}
  - {name: both, provider: site, url: https://a.example, base_url: https://b.example}
`))
	if err == nil {
		t.Fatal("invalid config accepted")
	}
	for _, want := range []string{
		"version is 2", "kebab-case", `provider "s3"`, "path (a built site) or url", "not both",
		"declared twice", "base_url is required", "absolute http(s) URL", `locale "English"`,
		"ai_crawlers.search", "llms_txt.title", "needs write_dir", "queries[0] needs",
		"schema[0] needs a path starting with /", `noindex path "admin" must start with /`, "max_pages must not be negative",
		"base_url applies only with path",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
}

func TestParseRefusesNoTargets(t *testing.T) {
	_, err := Parse([]byte("version: 1\ntargets: []\n"))
	if err == nil || !strings.Contains(err.Error(), "no targets") {
		t.Fatalf("empty targets: %v", err)
	}
}

func TestParseRefusesUnknownKeys(t *testing.T) {
	if _, err := Parse([]byte("version: 1\ntragets: []\n")); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestLoadResolvesPathsAgainstTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.yaml")
	if err := os.WriteFile(path, []byte("version: 1\ntargets:\n  - {name: a, provider: site, path: out, base_url: https://a.example}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Root(cfg.Targets[0]); got != filepath.Join(dir, "out") {
		t.Fatalf("root %q", got)
	}
	cfg, err = Load("-", strings.NewReader("version: 1\ntargets:\n  - {name: a, provider: site, path: out, base_url: https://a.example}\n"))
	if err != nil || cfg.Dir != "" {
		t.Fatalf("stdin: %+v %v", cfg, err)
	}
}

func TestMatch(t *testing.T) {
	locales := []string{"en", "ro", "ar"}
	for _, c := range []struct {
		pattern, path string
		want          bool
	}{
		{"/jobs/*", "/jobs/welder", true},
		{"/jobs/*", "/ro/jobs/welder", true},
		{"/jobs/*", "/jobs/a/b", false},
		{"/jobs/**", "/jobs", true},
		{"/jobs/**", "/ar/jobs/a/b", true},
		{"/", "/ro", true},
		{"/en/x", "/en/x", true}, // the default locale has no prefix to strip
		{"/admin*", "/admin-login", true},
	} {
		if got := Match(c.pattern, c.path, locales); got != c.want {
			t.Errorf("Match(%q, %q) = %v", c.pattern, c.path, got)
		}
	}
}
