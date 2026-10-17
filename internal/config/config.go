// Package config reads the YAML file that declares what a site should have.
// It reads that file and nothing else, so `validate` runs offline.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Version is the config schema this build reads.
const Version = 1

// Providers lists the provider names a target may use. A new provider adds
// its name here and its package under internal/provider.
var Providers = []string{"site"}

// Config is the whole file.
type Config struct {
	Version int      `yaml:"version"`
	Targets []Target `yaml:"targets"`
	// Dir is the directory the file was read from; relative target paths
	// resolve against it. Empty for standard input, which resolves against
	// the working directory.
	Dir string `yaml:"-"`
}

// Target is one website.
type Target struct {
	Name     string `yaml:"name"`
	Provider string `yaml:"provider"`
	// Path is a built site on disk (dist/, public/, _site/). Set Path or URL.
	Path string `yaml:"path"`
	// URL is a live site to crawl from its sitemap.
	URL string `yaml:"url"`
	// BaseURL is the public origin canonicals and the sitemap should use.
	// Defaults to URL; required with Path.
	BaseURL string `yaml:"base_url"`
	// WriteDir is where apply writes generated files, usually the folder the
	// build copies as-is (public/, static/). Empty: plan only.
	WriteDir string `yaml:"write_dir"`
	// MaxPages caps a live crawl. Default 300.
	MaxPages int `yaml:"max_pages"`
	// Locales the site publishes, by URL prefix (/ro/..., /ar/...). The first
	// is the default, served without a prefix. Every indexable page should
	// link all of them with hreflang.
	Locales []string `yaml:"locales"`
	// Noindex lists the path patterns that may ask not to be indexed.
	Noindex []string `yaml:"noindex"`
	// Ignore turns checks off by id prefix ("social.", "content.thin").
	Ignore []string `yaml:"ignore"`
	// MinWords below which an indexable page is thin. 0 keeps the default
	// (150), -1 turns the check off.
	MinWords   int          `yaml:"min_words"`
	AICrawlers AICrawlers   `yaml:"ai_crawlers"`
	Schema     []SchemaRule `yaml:"schema"`
	LLMsTxt    *LLMsTxt     `yaml:"llms_txt"`
	Business   *Business    `yaml:"business"`
	// BrandName is the name people search for the site by. Defaults to
	// business.name.
	BrandName string `yaml:"brand"`
	// Queries are the searches a page is meant to rank for.
	Queries []Query `yaml:"queries"`
}

// SearchName is the brand the titles and the WebSite block should carry, or
// "" when the config names none.
func (t Target) SearchName() string {
	if t.BrandName != "" || t.Business == nil {
		return t.BrandName
	}
	return t.Business.Name
}

// Query is one search and the page that should answer it.
type Query struct {
	Query string `yaml:"query"`
	Path  string `yaml:"path"`
}

// AICrawlers is the site's policy per crawler kind: allow, block or any.
// Search crawlers decide whether AI answers can cite the site; user agents
// fetch a page a person asked an assistant to read; training crawlers
// collect model training data.
type AICrawlers struct {
	Search   string `yaml:"search"`
	User     string `yaml:"user"`
	Training string `yaml:"training"`
}

// Policies fills in the defaults: search and user allowed (blocking them
// hides the site from AI answers), training left to the owner.
func (a AICrawlers) Policies() AICrawlers {
	return AICrawlers{Search: or(a.Search, "allow"), User: or(a.User, "allow"), Training: or(a.Training, "any")}
}

// SchemaRule says which JSON-LD types the pages matching Path must carry.
type SchemaRule struct {
	Path  string   `yaml:"path"`
	Types []string `yaml:"types"`
}

// LLMsTxt asks apply to write llms.txt (llmstxt.org) from the site's own
// page titles and descriptions.
type LLMsTxt struct {
	Title    string        `yaml:"title"`
	Summary  string        `yaml:"summary"`
	Details  string        `yaml:"details"`
	Sections []LLMsSection `yaml:"sections"`
}

// LLMsSection lists every indexable page matching Path under Name.
type LLMsSection struct {
	Name     string `yaml:"name"`
	Path     string `yaml:"path"`
	Optional bool   `yaml:"optional"`
}

// Business is the name and phone numbers every page's structured data must
// agree on (NAP consistency).
type Business struct {
	Name   string   `yaml:"name"`
	Phones []string `yaml:"phones"`
}

// Root is the target's built site, resolved against the config's directory.
func (c Config) Root(t Target) string { return c.Resolve(t.Path) }

// Resolve resolves a path from the file against the file's directory.
func (c Config) Resolve(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.Dir, p)
}

// Base is the target's public origin.
func (t Target) Base() (*url.URL, error) {
	u, err := url.Parse(or(t.BaseURL, t.URL))
	if err != nil {
		return nil, err
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "", "", "", ""
	return u, nil
}

// Names lists the target names in file order.
func (c Config) Names() []string {
	names := make([]string, 0, len(c.Targets))
	for _, t := range c.Targets {
		names = append(names, t.Name)
	}
	return names
}

// Find returns the named target.
func (c Config) Find(name string) (Target, bool) {
	for _, t := range c.Targets {
		if t.Name == name {
			return t, true
		}
	}
	return Target{}, false
}

// Load reads path, or standard input when path is "-".
func Load(path string, stdin io.Reader) (Config, error) {
	var (
		data []byte
		err  error
		dir  string
	)
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
		dir = filepath.Dir(path)
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Dir = dir
	return cfg, nil
}

// Parse decodes and validates a config. Unknown keys are refused: a typo
// that is silently ignored is a setting that silently does nothing.
func Parse(data []byte) (Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, err
	}
	return cfg, cfg.Validate()
}

var (
	kebab  = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	locale = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)

// Validate reports every problem at once, so one run fixes the file.
func (c Config) Validate() error {
	var errs []error
	if c.Version != Version {
		errs = append(errs, fmt.Errorf("version is %d, this build reads version %d", c.Version, Version))
	}
	seen := map[string]bool{}
	for i, t := range c.Targets {
		where := fmt.Sprintf("targets[%d]", i)
		if t.Name != "" {
			where = "target " + t.Name
		}
		fail := func(format string, a ...any) {
			errs = append(errs, fmt.Errorf("%s: %s", where, fmt.Sprintf(format, a...)))
		}
		switch {
		case !kebab.MatchString(t.Name):
			fail("name %q must be kebab-case", t.Name)
		case seen[t.Name]:
			fail("declared twice")
		}
		seen[t.Name] = true
		if !slices.Contains(Providers, t.Provider) {
			fail("provider %q is not one of %s", t.Provider, strings.Join(Providers, ", "))
		}
		t.validateSource(fail)
		for _, l := range t.Locales {
			if !locale.MatchString(l) {
				fail("locale %q is not a language code such as en or pt-BR", l)
			}
		}
		for _, kind := range []struct{ name, v string }{{"search", t.AICrawlers.Search}, {"user", t.AICrawlers.User}, {"training", t.AICrawlers.Training}} {
			if kind.v != "" && !slices.Contains([]string{"allow", "block", "any"}, kind.v) {
				fail("ai_crawlers.%s is %q, not allow, block or any", kind.name, kind.v)
			}
		}
		for j, r := range t.Schema {
			if !strings.HasPrefix(r.Path, "/") || len(r.Types) == 0 {
				fail("schema[%d] needs a path starting with / and at least one type", j)
			}
		}
		for j, q := range t.Queries {
			if strings.TrimSpace(q.Query) == "" || !strings.HasPrefix(q.Path, "/") {
				fail("queries[%d] needs a query and a path starting with /", j)
			}
		}
		for _, p := range t.Noindex {
			if !strings.HasPrefix(p, "/") {
				fail("noindex path %q must start with /", p)
			}
		}
		if t.LLMsTxt != nil {
			if t.LLMsTxt.Title == "" {
				fail("llms_txt.title is required")
			}
			if t.WriteDir == "" {
				fail("llms_txt needs write_dir: apply writes llms.txt there")
			}
		}
		if t.MaxPages < 0 {
			fail("max_pages must not be negative")
		}
	}
	return errors.Join(errs...)
}

func (t Target) validateSource(fail func(string, ...any)) {
	switch {
	case t.Path == "" && t.URL == "":
		fail("path (a built site) or url (a live one) is required")
	case t.Path != "" && t.URL != "":
		fail("set path or url, not both")
	case t.Path != "" && t.BaseURL == "":
		fail("base_url is required with path: canonicals and the sitemap are judged against it")
	}
	for _, f := range []struct{ name, v string }{{"url", t.URL}, {"base_url", t.BaseURL}} {
		if f.v == "" {
			continue
		}
		if u, err := url.Parse(f.v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			fail("%s %q must be an absolute http(s) URL", f.name, f.v)
		}
	}
}

// Match reports whether a URL path matches a pattern: * matches within one
// segment, /** matches any number of segments. A locale prefix is dropped
// first, so /jobs/* also matches /ro/jobs/x.
func Match(pattern, path string, locales []string) bool {
	return globRE(pattern).MatchString(StripLocale(path, locales))
}

// StripLocale drops a leading /<locale> segment for any locale but the
// default (the first), which is served without one.
func StripLocale(path string, locales []string) string {
	for _, l := range locales[min(1, len(locales)):] {
		switch {
		case path == "/"+l:
			return "/"
		case strings.HasPrefix(path, "/"+l+"/"):
			return strings.TrimPrefix(path, "/"+l)
		}
	}
	return path
}

func globRE(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch {
		case strings.HasPrefix(pattern[i:], "/**"):
			b.WriteString("(/.*)?")
			i += 2
		case pattern[i] == '*':
			b.WriteString("[^/]*")
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func or(v, def string) string {
	if v != "" {
		return v
	}
	return def
}
