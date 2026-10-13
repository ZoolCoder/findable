package site

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

// Skip lists the files a static build writes that are not pages of their own:
// the not-found page and the SPA fallback.
var Skip = []string{"404.html", "200.html"}

// LoadDir reads a built site from dir. Every .html file is a page; dir/a.html
// and dir/a/index.html are both served as /a.
func LoadDir(dir string, base *url.URL) (Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Site{}, fmt.Errorf("read %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	s := Site{Base: base}
	s.Robots = readFile(root, "robots.txt")
	s.LLMs = readFile(root, "llms.txt")
	s.Sitemap = dirSitemap(root, base)

	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !strings.HasSuffix(name, ".html") || slices.Contains(Skip, path.Base(name)) {
			return err
		}
		b, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		page, err := Parse(bytes.NewReader(b), CleanPath("/"+name))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		s.Pages = append(s.Pages, page)
		return nil
	})
	slices.SortFunc(s.Pages, func(a, b Page) int { return strings.Compare(a.Path, b.Path) })
	return s, err
}

func readFile(root *os.Root, name string) File {
	b, err := root.ReadFile(name)
	if err != nil {
		return File{}
	}
	return File{Found: true, Body: string(b)}
}

// dirSitemap reads sitemap.xml and, for an index, each child sitemap the
// build wrote next to it.
func dirSitemap(root *os.Root, base *url.URL) Sitemap {
	return collectSitemap("sitemap.xml", func(loc string) ([]byte, error) {
		name := strings.TrimPrefix(loc, "/")
		if u, err := url.Parse(loc); err == nil && u.Host != "" {
			if !sameHost(u.Host, base.Host) {
				return nil, fmt.Errorf("%s is on another host", loc)
			}
			name = strings.TrimPrefix(u.Path, "/")
		}
		return root.ReadFile(name)
	})
}

// collectSitemap flattens a sitemap and its children. A missing first file is
// Found false; a broken child is reported in Err.
func collectSitemap(first string, read func(string) ([]byte, error)) Sitemap {
	var sm Sitemap
	queue, seen := []string{first}, map[string]bool{}
	for len(queue) > 0 && len(seen) < 50 {
		loc := queue[0]
		queue = queue[1:]
		if seen[loc] {
			continue
		}
		seen[loc] = true
		b, err := read(loc)
		if err != nil {
			if loc != first {
				sm.Err = fmt.Sprintf("%s: %v", loc, err)
			}
			continue
		}
		sm.Found = true
		urls, children, err := parseSitemap(b)
		if err != nil {
			sm.Err = fmt.Sprintf("%s: %v", loc, err)
			continue
		}
		sm.URLs = append(sm.URLs, urls...)
		queue = append(queue, children...)
	}
	return sm
}

func parseSitemap(b []byte) (urls, children []string, err error) {
	var doc struct {
		XMLName xml.Name
		URLs    []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
		Maps []struct {
			Loc string `xml:"loc"`
		} `xml:"sitemap"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, nil, err
	}
	switch doc.XMLName.Local {
	case "urlset", "sitemapindex":
	default:
		return nil, nil, fmt.Errorf("root element is <%s>, not <urlset> or <sitemapindex>", doc.XMLName.Local)
	}
	for _, u := range doc.URLs {
		urls = append(urls, strings.TrimSpace(u.Loc))
	}
	for _, m := range doc.Maps {
		children = append(children, strings.TrimSpace(m.Loc))
	}
	return urls, children, nil
}

// Crawler reads a live site over HTTP.
type Crawler struct {
	Client    *http.Client
	UserAgent string
	// Max caps the pages fetched.
	Max int
	// Workers is how many pages are fetched at once.
	Workers int
}

const maxBody = 5 << 20

// Crawl reads robots.txt, llms.txt and the sitemap at base, then each page
// the sitemap lists on this host. With no sitemap it follows links from the
// home page instead.
func (c Crawler) Crawl(ctx context.Context, base *url.URL) (Site, error) {
	if c.Client == nil {
		c.Client = &http.Client{Timeout: 20 * time.Second}
	}
	c.Max = cmpOr(c.Max, 300)
	c.Workers = cmpOr(c.Workers, 4)
	s := Site{Base: base, Live: true}

	robots, err := c.text(ctx, s.URL("/robots.txt"))
	if err != nil && !errors.Is(err, errNotFound) {
		return Site{}, fmt.Errorf("reach %s: %w", base, err)
	}
	s.Robots = robots
	s.LLMs, _ = c.text(ctx, s.URL("/llms.txt"))
	first := s.URL("/sitemap.xml")
	if maps := ParseRobots(robots.Body).Sitemaps; len(maps) > 0 {
		first = maps[0]
	}
	s.Sitemap = collectSitemap(first, func(loc string) ([]byte, error) {
		f, err := c.text(ctx, loc)
		return []byte(f.Body), err
	})

	var paths []string
	for _, u := range s.Sitemap.URLs {
		if p := s.LocalPath("/", u); p != "" && !slices.Contains(paths, p) {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		paths = []string{"/"}
	}
	s.Pages = c.pages(ctx, s, paths, len(s.Sitemap.URLs) == 0)
	slices.SortFunc(s.Pages, func(a, b Page) int { return strings.Compare(a.Path, b.Path) })
	return s, nil
}

// pages fetches paths, and with follow also every same-site link the pages
// carry, until Max.
func (c Crawler) pages(ctx context.Context, s Site, paths []string, follow bool) []Page {
	var (
		mu    sync.Mutex
		out   []Page
		seen  = map[string]bool{}
		queue = slices.Clone(paths)
	)
	for _, p := range paths {
		seen[p] = true
	}
	for len(queue) > 0 && len(out) < c.Max {
		batch := queue[:min(len(queue), c.Workers, c.Max-len(out))]
		queue = queue[len(batch):]
		var wg sync.WaitGroup
		for _, p := range batch {
			wg.Go(func() {
				page, ok := c.page(ctx, s, p)
				if !ok {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				out = append(out, page)
				if !follow {
					return
				}
				for _, href := range page.Links {
					if lp := s.LocalPath(p, href); lp != "" && !seen[lp] && !LooksLikeFile(lp) {
						seen[lp] = true
						queue = append(queue, lp)
					}
				}
			})
		}
		wg.Wait()
	}
	return out
}

func (c Crawler) page(ctx context.Context, s Site, p string) (Page, bool) {
	resp, err := c.get(ctx, s.URL(p))
	if err != nil {
		return Page{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "html") {
		return Page{}, false
	}
	page, err := Parse(io.LimitReader(resp.Body, maxBody), p)
	if err != nil {
		return Page{}, false
	}
	page.XRobots = resp.Header.Get("X-Robots-Tag")
	return page, true
}

var errNotFound = errors.New("not found")

func (c Crawler) text(ctx context.Context, u string) (File, error) {
	resp, err := c.get(ctx, u)
	if err != nil {
		return File{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return File{}, fmt.Errorf("%s: %w (%d)", u, errNotFound, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return File{}, err
	}
	// A single-page app answers 200 with its HTML shell for any path: that
	// is not a robots.txt or a sitemap.
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") && !strings.HasSuffix(u, ".html") {
		return File{}, fmt.Errorf("%s: %w (an HTML page answered)", u, errNotFound)
	}
	return File{Found: true, Body: string(b)}, nil
}

func (c Crawler) get(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	return c.Client.Do(req)
}

// LooksLikeFile reports whether a path names a file (an image, a PDF) rather
// than a page.
func LooksLikeFile(p string) bool {
	ext := path.Ext(p)
	return ext != "" && ext != ".html"
}

func cmpOr(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}
