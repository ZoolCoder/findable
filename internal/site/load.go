package site

import (
	"bytes"
	"compress/gzip"
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

// maxSitemap caps a gzipped sitemap once unpacked: the protocol allows 50 MB.
const maxSitemap = 50 << 20

func parseSitemap(b []byte) (urls, children []string, err error) {
	if bytes.HasPrefix(b, []byte{0x1f, 0x8b}) {
		if b, err = gunzip(b); err != nil {
			return nil, nil, err
		}
	}
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

func gunzip(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	out, err := io.ReadAll(io.LimitReader(zr, maxSitemap+1))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	if len(out) > maxSitemap {
		return nil, fmt.Errorf("unpacks to more than %d MB", maxSitemap>>20)
	}
	return out, nil
}

// Crawler reads a live site over HTTP. It stays on the site's host: a
// redirect or a sitemap that points elsewhere is reported, never followed.
type Crawler struct {
	Client    *http.Client
	UserAgent string
	// Max caps the pages fetched.
	Max int
	// Workers is how many pages are fetched at once.
	Workers int
}

const (
	maxBody      = 5 << 20
	maxRedirects = 5
	// robotsToken is the product token findable obeys in robots.txt.
	robotsToken = "findable"
)

// Crawl reads robots.txt, llms.txt and the sitemap at base, then each page
// the sitemap lists on this host. With no sitemap it follows links from the
// home page instead. Pages robots.txt keeps findable out of are skipped.
func (c Crawler) Crawl(ctx context.Context, base *url.URL) (Site, error) {
	c.Client = sameHostClient(c.Client, base)
	c.Max = cmpOr(c.Max, 300)
	c.Workers = cmpOr(c.Workers, 4)
	s := Site{Base: base, Live: true, Failed: map[string]string{}}

	robots, err := c.text(ctx, s.URL("/robots.txt"))
	if err != nil && !errors.Is(err, errNotFound) {
		return Site{}, fmt.Errorf("reach %s: %w", base, err)
	}
	s.Robots = robots
	s.LLMs, _ = c.text(ctx, s.URL("/llms.txt"))
	rules := ParseRobots(robots.Body)
	first := s.URL("/sitemap.xml")
	if len(rules.Sitemaps) > 0 {
		first = rules.Sitemaps[0]
	}
	s.Sitemap = collectSitemap(first, func(loc string) ([]byte, error) {
		u, err := url.Parse(loc)
		if err != nil || !sameHost(u.Host, base.Host) {
			return nil, fmt.Errorf("%s is not on %s; findable reads sitemaps on the site's own host only", loc, base.Host)
		}
		f, err := c.text(ctx, loc)
		return []byte(f.Body), err
	})

	var queue []fetch
	seen := map[string]bool{}
	for _, u := range s.Sitemap.URLs {
		if p := s.LocalPath("/", u); p != "" && !seen[p] {
			seen[p] = true
			queue = append(queue, fetch{path: p, url: u})
		}
	}
	if len(queue) == 0 {
		queue = []fetch{{path: "/", url: s.URL("/")}}
	}
	c.pages(ctx, &s, rules, queue, len(s.Sitemap.URLs) == 0)
	slices.SortFunc(s.Pages, func(a, b Page) int { return strings.Compare(a.Path, b.Path) })
	slices.Sort(s.Skipped)
	return s, nil
}

// sameHostClient copies client (a default one when nil) so that it follows
// at most maxRedirects redirects and never one to another host: the
// redirect response comes back instead, and the crawl reports it.
func sameHostClient(client *http.Client, base *url.URL) *http.Client {
	c := http.Client{Timeout: 20 * time.Second}
	if client != nil {
		c = *client
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects || !sameHost(req.URL.Host, base.Host) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &c
}

// fetch is a page to read: its path on the site and the URL as listed.
type fetch struct{ path, url string }

// pages fetches the queue, and with follow also every same-site link the
// pages carry, until Max. A page that does not answer 200 with HTML goes in
// s.Failed with the reason.
func (c Crawler) pages(ctx context.Context, s *Site, rules Robots, queue []fetch, follow bool) {
	var mu sync.Mutex
	seen := map[string]bool{}
	for _, f := range queue {
		seen[f.path] = true
	}
	for len(queue) > 0 && len(s.Pages) < c.Max {
		var batch []fetch
		for len(queue) > 0 && len(batch) < min(c.Workers, c.Max-len(s.Pages)) {
			f := queue[0]
			queue = queue[1:]
			if rules.Allowed(robotsToken, f.path) {
				batch = append(batch, f)
			} else {
				s.Skipped = append(s.Skipped, f.path)
			}
		}
		var wg sync.WaitGroup
		for _, f := range batch {
			wg.Go(func() {
				page, final, why := c.page(ctx, f)
				mu.Lock()
				defer mu.Unlock()
				if why != "" {
					s.Failed[f.path] = why
					// A same-host redirect to another page: read that page.
					if final != "" && !seen[final] {
						seen[final] = true
						queue = append(queue, fetch{path: final, url: s.URL(final)})
					}
					return
				}
				s.Pages = append(s.Pages, page)
				if !follow {
					return
				}
				for _, href := range page.Links {
					if lp := s.LocalPath(f.path, href); lp != "" && !seen[lp] && !LooksLikeFile(lp) {
						seen[lp] = true
						queue = append(queue, fetch{path: lp, url: s.URL(lp)})
					}
				}
			})
		}
		wg.Wait()
	}
	s.Truncated = len(queue) > 0
}

// page reads one page. why is empty on success; otherwise it says what the
// URL answered, and final is the same-host path a redirect led to, if any.
func (c Crawler) page(ctx context.Context, f fetch) (page Page, final, why string) {
	resp, err := c.get(ctx, f.url)
	if err != nil {
		return Page{}, "", "did not answer: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	if loc := resp.Header.Get("Location"); resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return Page{}, "", fmt.Sprintf("redirects (%d) to %s, off the site or past %d hops", resp.StatusCode, loc, maxRedirects)
	}
	if resp.StatusCode != http.StatusOK {
		return Page{}, "", fmt.Sprintf("answers %d", resp.StatusCode)
	}
	if got := CleanPath(resp.Request.URL.Path); got != f.path {
		return Page{}, got, "redirects to " + got
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "html") {
		return Page{}, "", "is not HTML (" + resp.Header.Get("Content-Type") + ")"
	}
	page, err = Parse(io.LimitReader(resp.Body, maxBody), f.path)
	if err != nil {
		return Page{}, "", "does not parse: " + err.Error()
	}
	page.XRobots = resp.Header.Get("X-Robots-Tag")
	return page, "", ""
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
