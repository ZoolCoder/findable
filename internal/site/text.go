package site

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// bodyText renders the readable content of a parsed document: the first
// <main> when there is one, else every <article> outside the page chrome,
// otherwise the body.
func bodyText(doc *html.Node) string {
	roots := []*html.Node{firstElement(doc, func(n *html.Node) bool { return n.DataAtom == atom.Main })}
	if roots[0] == nil {
		roots = articles(doc)
	}
	if len(roots) == 0 {
		roots = []*html.Node{firstElement(doc, func(n *html.Node) bool { return n.DataAtom == atom.Body })}
	}
	var r textRenderer
	for _, root := range roots {
		if root == nil {
			return ""
		}
		r.render(root)
		r.flush()
	}
	return strings.Join(r.paras, "\n\n")
}

// articles lists the outermost <article> elements outside the page chrome.
func articles(n *html.Node) []*html.Node {
	if n.Type == html.ElementNode && n.DataAtom == atom.Article && !inChrome(n) {
		return []*html.Node{n}
	}
	var found []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		found = append(found, articles(c)...)
	}
	return found
}

func inChrome(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		switch p.DataAtom {
		case atom.Nav, atom.Header, atom.Footer, atom.Aside:
			return true
		}
	}
	return false
}

func firstElement(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n.Type == html.ElementNode && match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := firstElement(c, match); found != nil {
			return found
		}
	}
	return nil
}

type textRenderer struct {
	paras  []string
	cur    strings.Builder
	prefix string
	// sectionDepth is how many main, article or section elements enclose the
	// node being rendered; a header or footer inside one belongs to its content.
	sectionDepth int
	lastWasList  bool
}

func (r *textRenderer) render(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		r.cur.WriteString(n.Data)
		return
	case html.ElementNode:
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Svg, atom.Nav, atom.Aside:
			return
		case atom.Header, atom.Footer:
			if r.sectionDepth == 0 {
				return
			}
		case atom.Pre:
			r.flush()
			r.addCode(n)
			return
		case atom.Br, atom.Td, atom.Th:
			defer r.cur.WriteByte(' ')
		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			r.flush()
			r.prefix = strings.Repeat("#", int(n.Data[1]-'0')) + " "
			defer r.endBlock()
		case atom.Li:
			r.flush()
			r.prefix = "- "
			defer r.endBlock()
		case atom.Main, atom.Section, atom.Article:
			r.sectionDepth++
			defer func() { r.sectionDepth-- }()
			r.flush()
			defer r.endBlock()
		case atom.P, atom.Div, atom.Blockquote, atom.Tr, atom.Dt, atom.Dd:
			r.flush()
			defer r.endBlock()
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.render(c)
	}
}

// addCode keeps the line breaks and indentation of a <pre> as a fenced block.
func (r *textRenderer) addCode(pre *html.Node) {
	code := strings.Trim(preText(pre), "\n")
	if strings.TrimSpace(code) == "" {
		return
	}
	fence := strings.Repeat("`", max(3, longestBacktickRun(code)+1))
	r.paras = append(r.paras, fence+"\n"+code+"\n"+fence)
	r.lastWasList = false
}

func preText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(preText(c))
	}
	return b.String()
}

func longestBacktickRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

func (r *textRenderer) endBlock() {
	r.flush()
	r.prefix = ""
}

// blockStart matches text that Markdown would read as a heading, quote,
// fence or list item at the start of a paragraph.
var blockStart = regexp.MustCompile("^(#|>|`{3,}|~{3,}|[-+*] )")

var orderedMarker = regexp.MustCompile(`^(\d+)([.)] )`)

// escapeBlockStart keeps page text from forging Markdown structure.
func escapeBlockStart(line string) string {
	if orderedMarker.MatchString(line) {
		return orderedMarker.ReplaceAllString(line, `$1\$2`)
	}
	if blockStart.MatchString(line) {
		return `\` + line
	}
	return line
}

// flush ends the current paragraph, collapsing its whitespace.
func (r *textRenderer) flush() {
	line := strings.Join(strings.Fields(r.cur.String()), " ")
	r.cur.Reset()
	if line == "" {
		return
	}
	if r.prefix == "" {
		line = escapeBlockStart(line)
	}
	// Consecutive list items form one list, not one paragraph each.
	if isItem := r.prefix == "- "; isItem && r.lastWasList {
		r.paras[len(r.paras)-1] += "\n" + r.prefix + line
	} else {
		r.paras = append(r.paras, r.prefix+line)
		r.lastWasList = isItem
	}
	r.prefix = ""
}
