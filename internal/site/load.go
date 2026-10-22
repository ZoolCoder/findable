package site

import (
	"bytes"
	"cmp"
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
// and dir/a/index.html are both served as /a. When a site holds both, the file
// a.html is the page and the directory index is listed in Site.Shadowed.
func LoadDir(dir string, base *url.URL) (Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Site{}, fmt.Errorf("read %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	s := Site{Base: base, Failed: map[string]string{}, Shadowed: map[string]string{}}
	s.Robots = readFile(root, "robots.txt")
	s.LLMs = readFile(root, "llms.txt")
	s.LLMsFull = readFile(root, "llms-full.txt")
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
			s.Failed[servedPath(name)] = "could not be parsed: " + err.Error()
			return nil
		}
		page.Served = servedPath(name)
		s.addFilePage(page, name)
		return nil
	})
	slices.SortFunc(s.Pages, func(a, b Page) int { return strings.Compare(a.Path, b.Path) })
	return s, err
}

// addFilePage adds the page read from file name, keeping only one page per
// path: a.html wins over a/index.html.
func (s *Site) addFilePage(page Page, name string) {
	i := slices.IndexFunc(s.Pages, func(p Page) bool { return p.Path == page.Path })
	if i < 0 {
		s.Pages = append(s.Pages, page)
		return
	}
	if strings.HasSuffix(name, "/index.html") {
		s.Shadowed[page.Path] = page.Served
		return
	}
	s.Shadowed[page.Path] = s.Pages[i].Served
	s.Pages[i] = page
}

// servedPath is the path a static server answers for the file name:
// blog/post/index.html is the directory /blog/post/.
func servedPath(name string) string {
	if name == "index.html" {
		return "/"
	}
	if dir, ok := strings.CutSuffix(name, "/index.html"); ok {
		return "/" + dir + "/"
	}
	return "/" + name
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
	return collectSitemap([]string{"sitemap.xml"}, true, func(loc string) ([]byte, error) {
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

const (
	maxSitemapFiles = 50
	maxSitemapURLs  = 50000
)

// collectSitemap flattens the sitemaps at roots and their children. With
// optional, a root that does not exist is Found false and no error: the
// default /sitemap.xml may simply be absent. Every other failure, and any
// limit that cut the read short, is reported in Err.
func collectSitemap(roots []string, optional bool, read func(string) ([]byte, error)) Sitemap {
	var (
		sm        Sitemap
		errs      []error
		truncated bool
	)
	var queue []string
	queued := map[string]bool{}
	enqueue := func(locs ...string) {
		for _, loc := range locs {
			if !queued[loc] {
				queued[loc] = true
				queue = append(queue, loc)
			}
		}
	}
	enqueue(roots...)
	filesRead := 0
	for len(queue) > 0 && filesRead < maxSitemapFiles && len(sm.URLs) < maxSitemapURLs {
		loc := queue[0]
		queue = queue[1:]
		filesRead++
		b, err := read(loc)
		if err != nil {
			missing := errors.Is(err, errNotFound) || errors.Is(err, fs.ErrNotExist)
			if !optional || !missing || !slices.Contains(roots, loc) {
				errs = append(errs, fmt.Errorf("%s: %w", loc, err))
			}
			continue
		}
		sm.Found = true
		urls, children, err := parseSitemap(b)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", loc, err))
			continue
		}
		if room := maxSitemapURLs - len(sm.URLs); len(urls) > room {
			urls, truncated = urls[:room], true
		}
		sm.URLs = append(sm.URLs, urls...)
		enqueue(children...)
	}
	switch {
	case truncated:
		errs = append(errs, fmt.Errorf("more than %d URLs; the rest are ignored", maxSitemapURLs))
	case len(queue) > 0:
		errs = append(errs, fmt.Errorf("stopped after %d sitemaps; %d left unread", filesRead, len(queue)))
	}
	if len(errs) > 0 {
		sm.Err = errors.Join(errs...).Error()
	}
	return sm
}

// maxFollowPaths bounds the paths a link-following crawl remembers, so a
// page of endless distinct links cannot exhaust memory. A variable so a test
// can lower it.
var maxFollowPaths = 50000

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
	// attemptsPerPage times Max, plus the sitemap's entries, bounds the
	// requests a crawl makes for pages.
	attemptsPerPage = 4
	// robotsToken is the product token findable obeys in robots.txt.
	robotsToken = "findable"
)

// Crawl reads robots.txt, llms.txt and the sitemap at base, then each page
// the sitemap lists on this host. With no sitemap it follows links from the
// home page instead. Pages robots.txt keeps findable out of are skipped.
func (c Crawler) Crawl(ctx context.Context, base *url.URL) (Site, error) {
	client := c.Client
	// robots.txt and the sitemaps are read before any rule exists.
	c.Client = sameHostClient(client, base, Robots{})
	c.Max = cmpOr(c.Max, 300)
	c.Workers = cmpOr(c.Workers, 4)
	s := Site{Base: base, Live: true, Failed: map[string]string{}, Hops: map[string][]Hop{}}

	robots, err := c.text(ctx, s.URL("/robots.txt"), maxBody)
	if err != nil && !errors.Is(err, errNotFound) {
		return Site{}, fmt.Errorf("reach %s: %w", base, err)
	}
	s.Robots = robots
	s.LLMs, _ = c.text(ctx, s.URL("/llms.txt"), maxBody)
	s.LLMsFull, _ = c.text(ctx, s.URL("/llms-full.txt"), maxBody)
	rules := ParseRobots(robots.Body)
	c.Client = sameHostClient(client, base, rules)
	roots, optional := rules.Sitemaps, false
	if len(roots) == 0 {
		roots, optional = []string{s.URL("/sitemap.xml")}, true
	}
	s.Sitemap = collectSitemap(roots, optional, func(loc string) ([]byte, error) {
		u, err := url.Parse(loc)
		if err != nil || !sameHost(u.Host, base.Host) {
			return nil, fmt.Errorf("%s is not on %s; findable reads sitemaps on the site's own host only", loc, base.Host)
		}
		f, err := c.text(ctx, loc, maxSitemap)
		return []byte(f.Body), err
	})

	var queue []fetch
	seen := map[string]bool{}
	for _, u := range s.Sitemap.URLs {
		abs := s.resolveURL("/", u)
		if abs == nil {
			continue
		}
		// Files (PDFs, images) are valid sitemap entries, not pages to read.
		if p := CleanPath(abs.Path); !seen[p] && !LooksLikeFile(p) {
			seen[p] = true
			queue = append(queue, fetch{path: p, url: u, raw: requestPath(abs)})
		}
	}
	if len(queue) == 0 {
		queue = []fetch{{path: "/", url: s.URL("/")}}
	}
	c.pages(ctx, &s, rules, queue, len(s.Sitemap.URLs) == 0)
	if err := ctx.Err(); err != nil {
		return Site{}, fmt.Errorf("crawl %s: %w", base, err)
	}
	slices.SortFunc(s.Pages, func(a, b Page) int { return strings.Compare(a.Path, b.Path) })
	slices.Sort(s.Skipped)
	return s, nil
}

// sameHostClient copies client (a default one when nil) so that it follows
// at most maxRedirects redirects and never one to another host: the
// redirect response comes back instead, and the crawl reports it. A redirect
// to a path rules disallow is not followed either.
func sameHostClient(client *http.Client, base *url.URL, rules Robots) *http.Client {
	c := http.Client{Timeout: 20 * time.Second}
	if client != nil {
		c = *client
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects || !sameHost(req.URL.Host, base.Host) || !rules.Allowed(robotsToken, requestPath(req.URL)) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &c
}

// fetch is a page to read: its path on the site, the URL as listed and the
// path and query as requested, which robots.txt rules are tested against.
// raw is empty for the home page queued by default.
type fetch struct{ path, url, raw string }

// pages fetches the queue, and with follow also every same-site link the
// pages carry, until Max. A page that does not answer 200 with HTML goes in
// s.Failed with the reason.
func (c Crawler) pages(ctx context.Context, s *Site, rules Robots, queue []fetch, follow bool) {
	var mu sync.Mutex
	seen := map[string]bool{}
	capped := false
	for _, f := range queue {
		seen[f.path] = true
	}
	// Pages that fail never count towards Max, so the requests are budgeted
	// separately: a site of dead links or endless redirects cannot hold the
	// crawl.
	attempts, budget := 0, c.Max*attemptsPerPage+len(queue)
	for len(queue) > 0 && len(s.Pages) < c.Max && attempts < budget && ctx.Err() == nil {
		var batch []fetch
		for len(queue) > 0 && len(batch) < min(c.Workers, c.Max-len(s.Pages), budget-attempts) {
			f := queue[0]
			queue = queue[1:]
			if rules.Allowed(robotsToken, cmp.Or(f.raw, f.path)) {
				batch = append(batch, f)
			} else {
				s.Skipped = append(s.Skipped, f.path)
			}
		}
		attempts += len(batch)
		var wg sync.WaitGroup
		for _, f := range batch {
			wg.Go(func() {
				r := c.page(ctx, f, rules)
				mu.Lock()
				defer mu.Unlock()
				if len(r.hops) > 1 {
					s.Hops[f.path] = r.hops
				}
				if r.disallowed {
					s.Skipped = append(s.Skipped, f.path)
					return
				}
				if r.why != "" {
					s.Failed[f.path] = r.why
					// A same-host redirect to another page: read that page.
					if r.final != "" && !seen[r.final] {
						if len(seen) >= maxFollowPaths {
							capped = true
							return
						}
						seen[r.final] = true
						queue = append(queue, fetch{path: r.final, url: s.URL(r.final), raw: r.finalRaw})
					}
					return
				}
				page := r.page
				s.Pages = append(s.Pages, page)
				// The canonical is the URL search engines index, so its
				// status matters even when nothing else leads there.
				if abs := s.resolveURL(cmp.Or(page.Served, page.Path), page.Canonical); abs != nil {
					if lp := CleanPath(abs.Path); !seen[lp] && !LooksLikeFile(lp) {
						if len(seen) >= maxFollowPaths {
							capped = true
						} else {
							seen[lp] = true
							queue = append(queue, fetch{path: lp, url: s.URL(lp), raw: requestPath(abs)})
						}
					}
				}
				if !follow {
					return
				}
				for _, href := range page.Links {
					abs := s.resolveURL(cmp.Or(page.Served, page.Path), href)
					if abs == nil {
						continue
					}
					lp := CleanPath(abs.Path)
					if seen[lp] || LooksLikeFile(lp) {
						continue
					}
					if len(seen) >= maxFollowPaths {
						capped = true
						continue
					}
					seen[lp] = true
					// The fetch drops the query, so robots.txt is tested on the same URL.
					noQuery := *abs
					noQuery.RawQuery = ""
					queue = append(queue, fetch{path: lp, url: s.URL(lp), raw: requestPath(&noQuery)})
				}
			})
		}
		wg.Wait()
	}
	s.Truncated = capped || len(queue) > 0
}

// fetched is what reading one URL gave: the page on success, else why it
// could not be read and, for a same-host redirect to another page, the path
// it led to. hops is the redirect chain, when the client followed one.
type fetched struct {
	page  Page
	final string
	// finalRaw is the path as the final response was requested, which
	// robots.txt rules are tested against when that page is fetched.
	finalRaw string
	hops     []Hop
	why      string
	// disallowed is set when the page redirects to a path robots.txt keeps
	// findable out of: the page is skipped, not failed.
	disallowed bool
}

func (c Crawler) page(ctx context.Context, f fetch, rules Robots) fetched {
	resp, err := c.get(ctx, f.url)
	if err != nil {
		return fetched{why: "did not answer: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	r := fetched{hops: hops(resp)}
	if loc := resp.Header.Get("Location"); resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if next, err := resp.Request.URL.Parse(loc); err == nil && sameHost(next.Host, resp.Request.URL.Host) && !rules.Allowed(robotsToken, requestPath(next)) {
			r.why, r.disallowed = fmt.Sprintf("redirects (%d) to %s, which robots.txt disallows", resp.StatusCode, loc), true
			return r
		}
		r.why = fmt.Sprintf("redirects (%d) to %s, off the site or past %d hops", resp.StatusCode, loc, maxRedirects)
		return r
	}
	if resp.StatusCode != http.StatusOK {
		r.why = fmt.Sprintf("answers %d", resp.StatusCode)
		return r
	}
	if got := CleanPath(resp.Request.URL.Path); got != f.path {
		r.final, r.finalRaw, r.why = got, requestPath(resp.Request.URL), "redirects to "+got
		return r
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "html") {
		r.why = "is not HTML (" + resp.Header.Get("Content-Type") + ")"
		return r
	}
	page, err := Parse(io.LimitReader(resp.Body, maxBody), f.path)
	if err != nil {
		r.why = "does not parse: " + err.Error()
		return r
	}
	page.Served = escapeControls(resp.Request.URL.Path)
	page.XRobots = resp.Header.Get("X-Robots-Tag")
	served := *resp.Request.URL
	served.Fragment = ""
	page.URL = served.String()
	page.Redirects = redirectCodes(r.hops)
	r.page = page
	return r
}

// hops lists the responses from the URL requested to resp, first first: every
// redirect the client followed and the response that ended the chain.
// CleanPath hides a redirect that only adds or drops a trailing slash, so
// this is the only record of it.
func hops(resp *http.Response) []Hop {
	chain := []Hop{{URL: resp.Request.URL.String(), Code: resp.StatusCode}}
	for r := resp.Request.Response; r != nil; r = r.Request.Response {
		chain = append([]Hop{{URL: r.Request.URL.String(), Code: r.StatusCode}}, chain...)
	}
	return chain
}

// redirectCodes are the statuses of the redirects in a chain, the final
// response left out.
func redirectCodes(chain []Hop) []int {
	var codes []int
	for _, h := range chain[:len(chain)-1] {
		codes = append(codes, h.Code)
	}
	return codes
}

var errNotFound = errors.New("not found")

// text reads a text file of at most limit bytes. A 5xx or 429 answer is an
// error, not a missing file: RFC 9309 treats an unreachable robots.txt as
// disallowing everything, so the crawl must not go on as if there were none.
func (c Crawler) text(ctx context.Context, u string, limit int64) (File, error) {
	resp, err := c.get(ctx, u)
	if err != nil {
		return File{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return File{}, fmt.Errorf("%s: unreachable (%d)", u, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return File{}, fmt.Errorf("%s: %w (%d)", u, errNotFound, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return File{}, err
	}
	if int64(len(b)) > limit {
		return File{}, fmt.Errorf("%s: larger than %d MB", u, limit>>20)
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

// assetExtensions are the file types a site links to that are not pages. A
// dotted page name such as /docs/v2.0 is not among them.
var assetExtensions = []string{
	".pdf", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg", ".ico",
	".css", ".js", ".json", ".xml", ".txt", ".zip", ".gz", ".mp4", ".webm",
	".mp3", ".woff", ".woff2", ".csv", ".map", ".wasm",
}

// LooksLikeFile reports whether a path names a file (an image, a PDF) rather
// than a page.
func LooksLikeFile(p string) bool {
	return slices.Contains(assetExtensions, strings.ToLower(path.Ext(p)))
}

func cmpOr(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}
