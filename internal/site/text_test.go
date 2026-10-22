package site

import (
	"strings"
	"testing"
)

func TestParseText(t *testing.T) {
	const doc = `<html><head><title>T</title><style>p{}</style></head><body>
<header><p>Site banner</p></header><nav><a href="/">Home</a></nav>
<main>
  <h1>Welcome   to  <em>Acme</em></h1>
  <p>First
     paragraph.</p>
  <script>var x = 1</script><svg><text>icon</text></svg>
  <ul><li>One</li><li>Two <b>bold</b></li></ul>
  <h3>Deep</h3>
  <div>Last<br>line</div>
</main>
<article><p>Second article</p></article>
<footer><p>Copyright</p></footer></body></html>`
	p, err := Parse(strings.NewReader(doc), "/")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Welcome to Acme\n\nFirst paragraph.\n\n- One\n- Two bold\n\n### Deep\n\nLast line"
	if p.Text != want {
		t.Fatalf("text:\n%q\nwant:\n%q", p.Text, want)
	}
}

func TestParseTextWithoutMainSkipsBoilerplate(t *testing.T) {
	p, err := Parse(strings.NewReader(`<body><nav>Menu</nav><aside>Ad</aside><p>Hello</p><noscript>no js</noscript><footer>Foot</footer></body>`), "/")
	if err != nil {
		t.Fatal(err)
	}
	if p.Text != "Hello" {
		t.Fatalf("text %q", p.Text)
	}
	if p.Words != 4 {
		t.Fatalf("words %d: boilerplate still counts toward content.thin", p.Words)
	}
}

func TestParseTextFencesPre(t *testing.T) {
	const doc = "<body><main><p>Run:</p><pre><code>go test\n  ./...</code></pre><pre>   </pre><pre>a ```b``` c</pre></main></body>"
	p, err := Parse(strings.NewReader(doc), "/")
	if err != nil {
		t.Fatal(err)
	}
	want := "Run:\n\n```\ngo test\n  ./...\n```\n\n````\na ```b``` c\n````"
	if p.Text != want {
		t.Fatalf("text:\n%q\nwant:\n%q", p.Text, want)
	}
}

func TestParseTextPrefersMainOverChromeArticle(t *testing.T) {
	for _, doc := range []string{
		"<body><aside><article>side</article></aside><main><p>real</p></main></body>",
		"<body><header><article>head</article></header><main><p>real</p></main></body>",
	} {
		p, err := Parse(strings.NewReader(doc), "/")
		if err != nil {
			t.Fatal(err)
		}
		if p.Text != "real" {
			t.Errorf("%s: text %q", doc, p.Text)
		}
	}
	p, _ := Parse(strings.NewReader("<body><aside><article>side</article></aside><article>post</article></body>"), "/")
	if p.Text != "post" {
		t.Errorf("article outside chrome: %q", p.Text)
	}
}

func TestParseTextEscapesLeadingHash(t *testing.T) {
	p, err := Parse(strings.NewReader("<body><main><h2>Title</h2><p>#1 in Leeds</p></main></body>"), "/")
	if err != nil {
		t.Fatal(err)
	}
	if want := "## Title\n\n\\#1 in Leeds"; p.Text != want {
		t.Fatalf("text %q want %q", p.Text, want)
	}
}

func TestParseTextKeepsHeaderInsideArticle(t *testing.T) {
	p, _ := Parse(strings.NewReader("<body><article><header><h1>Post</h1></header><p>body</p><footer>end</footer><aside>ad</aside></article></body>"), "/")
	if want := "# Post\n\nbody\n\nend"; p.Text != want {
		t.Errorf("article header: %q want %q", p.Text, want)
	}
	p, _ = Parse(strings.NewReader("<body><header>Banner</header><p>x</p></body>"), "/")
	if p.Text != "x" {
		t.Errorf("body banner: %q", p.Text)
	}
}

func TestParseTextRendersEveryArticle(t *testing.T) {
	p, _ := Parse(strings.NewReader("<body><article><h2>A</h2><p>one</p></article><article><h2>B</h2><p>two</p></article></body>"), "/")
	if want := "## A\n\none\n\n## B\n\ntwo"; p.Text != want {
		t.Errorf("text %q want %q", p.Text, want)
	}
}

func TestParseTextEscapesBlockStarters(t *testing.T) {
	for in, want := range map[string]string{
		"```":        "\\```",
		"~~~":        "\\~~~",
		"- x":        "\\- x",
		"> quote":    "\\> quote",
		"1. first":   "1\\. first",
		"2) second":  "2\\) second",
		"-5 degrees": "-5 degrees",
		"*note":      "*note",
	} {
		p, _ := Parse(strings.NewReader("<body><main><p>"+in+"</p></main></body>"), "/")
		if p.Text != want {
			t.Errorf("%q: got %q want %q", in, p.Text, want)
		}
	}
}

func TestParseTextParagraphDoesNotJoinFollowingList(t *testing.T) {
	p, _ := Parse(strings.NewReader("<body><main><p>- x</p><ul><li>b</li><li>c</li></ul></main></body>"), "/")
	if want := "\\- x\n\n- b\n- c"; p.Text != want {
		t.Errorf("text %q want %q", p.Text, want)
	}
}
