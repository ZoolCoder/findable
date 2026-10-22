package site

import (
	"encoding/json"
	"io"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Parse reads one HTML document, served at path, into a Page.
func Parse(r io.Reader, path string) (Page, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return Page{}, err
	}
	p := Page{Path: path, Hreflang: map[string]string{}, OG: map[string]string{}}
	w := walker{page: &p}
	w.walk(doc, false, false)
	p.Text = bodyText(doc)
	return p, nil
}

type walker struct {
	page *Page
}

func (w *walker) walk(n *html.Node, inHead, inBody bool) {
	if n.Type == html.ElementNode {
		switch n.DataAtom {
		case atom.Html:
			w.page.Lang = attr(n, "lang")
		case atom.Head:
			inHead = true
		case atom.Body:
			inBody = true
		case atom.Title:
			if inHead && w.page.Title == "" {
				w.page.Title = strings.TrimSpace(text(n))
			}
		case atom.Meta:
			w.meta(n)
		case atom.Link:
			w.link(n)
		case atom.Script:
			if strings.EqualFold(attr(n, "type"), "application/ld+json") {
				w.jsonld(n)
			}
			return
		case atom.Style, atom.Noscript, atom.Template:
			return
		case atom.H1:
			w.page.H1 = append(w.page.H1, text(n))
		case atom.Img:
			if _, ok := attrOK(n, "alt"); !ok && attr(n, "role") != "presentation" && attr(n, "aria-hidden") != "true" {
				w.page.ImagesNoAlt++
			}
		case atom.A:
			if href := attr(n, "href"); href != "" {
				w.page.Links = append(w.page.Links, href)
			}
		}
	}
	if n.Type == html.TextNode && inBody {
		w.page.Words += len(strings.Fields(n.Data))
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.walk(c, inHead, inBody)
	}
}

func (w *walker) meta(n *html.Node) {
	content := strings.TrimSpace(attr(n, "content"))
	name := strings.ToLower(attr(n, "name"))
	prop := strings.ToLower(attr(n, "property"))
	switch {
	case name == "description":
		w.page.Description = content
	case name == "robots" || name == "googlebot":
		w.page.MetaRobots = strings.Trim(w.page.MetaRobots+","+content, ",")
	case strings.HasPrefix(prop, "og:"):
		w.page.OG[prop] = content
	}
}

func (w *walker) link(n *html.Node) {
	href := strings.TrimSpace(attr(n, "href"))
	for _, rel := range strings.Fields(strings.ToLower(attr(n, "rel"))) {
		switch rel {
		case "canonical":
			w.page.Canonical = href
		case "alternate":
			if lang := attr(n, "hreflang"); lang != "" {
				w.page.Hreflang[strings.ToLower(lang)] = href
			}
		}
	}
}

// jsonld keeps the block's raw text: collapsing whitespace would change
// string values.
func (w *walker) jsonld(n *html.Node) {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(c.Data)
	}
	raw := strings.TrimSpace(b.String())
	if raw == "" {
		return
	}
	var v json.RawMessage
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		w.page.JSONLDErrs = append(w.page.JSONLDErrs, err.Error())
		return
	}
	w.page.JSONLD = append(w.page.JSONLD, v)
}

func attr(n *html.Node, key string) string {
	v, _ := attrOK(n, key)
	return v
}

func attrOK(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val, true
		}
	}
	return "", false
}

// text is the node's text content with its whitespace collapsed.
func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
