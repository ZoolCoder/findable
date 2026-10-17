// Package sitetest writes a small built site for tests: two pages that pass
// every check, a sitemap, a robots.txt, and a config that asks apply to
// write llms.txt and block AI training crawlers.
package sitetest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Base is the fixture's public origin.
const Base = "https://example.com"

// Robots is the fixture's robots.txt, in dist/ and in public/.
const Robots = "User-agent: *\nAllow: /\n\nSitemap: " + Base + "/sitemap.xml\n"

// Config is the fixture's findable.yaml, relative to the root Write fills.
const Config = `version: 1
targets:
  - name: example
    provider: site
    path: dist
    base_url: ` + Base + `
    write_dir: public
    ai_crawlers: {training: block}
    llms_txt: {title: Example}
`

// Page is a page that passes every check.
func Page(path, title, desc string, links ...string) string {
	var a strings.Builder
	for _, l := range links {
		fmt.Fprintf(&a, `<a href="%s">%s</a> `, l, l)
	}
	return `<!doctype html><html lang="en"><head><title>` + title + `</title>
<meta name="description" content="` + desc + `">
<link rel="canonical" href="` + Base + path + `">
<meta property="og:title" content="` + title + `"><meta property="og:image" content="` + Base + `/og.jpg">
<script type="application/ld+json">[{"@context":"https://schema.org","@type":"Organization","name":"Example","url":"` + Base + `","logo":"` + Base + `/logo.png","sameAs":["https://github.com/example"]},
{"@context":"https://schema.org","@type":"WebSite","name":"Example","url":"` + Base + `"}]</script>
</head><body><main><h1>` + title + `</h1><p>` + strings.Repeat("word ", 200) + `</p>` + a.String() + `</main></body></html>`
}

// Files is the built site, by path under dist/.
func Files() map[string]string {
	return map[string]string{
		"index.html":       Page("/", "Example widgets for every workshop", "Example makes widgets for workshops of every size, shipped across Europe.", "/about"),
		"about/index.html": Page("/about", "About Example and the people behind it", "Who makes Example widgets, where the workshop is, and how to reach us.", "/"),
		"404.html":         "<html><body>not found</body></html>",
		"robots.txt":       Robots,
		"sitemap.xml": `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
			`<url><loc>` + Base + `/</loc></url><url><loc>` + Base + `/about</loc></url></urlset>`,
	}
}

// Write fills root with dist/, public/robots.txt and findable.yaml, and
// returns the config's path.
func Write(t testing.TB, root string) string {
	t.Helper()
	files := map[string]string{"public/robots.txt": Robots, "findable.yaml": Config}
	for name, body := range Files() {
		files["dist/"+name] = body
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(root, "findable.yaml")
}
