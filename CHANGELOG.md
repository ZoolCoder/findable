# Changelog

Notable changes, newest first. Versions follow [semver](https://semver.org);
before 1.0 a minor bump may change the config format, and any that does says so
here with what to edit.

## Unreleased

- A live crawl records the URL each page answers 200 at and the redirects on
  the way. New checks use it: `sitemap.url.redirect`, `meta.canonical.redirect`,
  `hreflang.redirect`, `links.redirect` and `redirect.temporary` catch a site
  that names its pages without the trailing slash it serves them at.
- `brand:` (defaults to `business.name`) checks that titles name the brand
  (`meta.title.brand`) and that the home page carries a WebSite block with it
  (`schema.website.name`).
- `queries:` names the search each page is meant to rank for; `content.query`
  checks its words are in the title, h1 and description.
- `schema.organization.sameas`: an Organization block without `sameAs`.
- `redirect.chain`: a URL that redirects more than once before a page
  answers, with every hop and its status. `meta.canonical.target`: a canonical
  naming a page that does not exist, or on a live crawl one that answers an
  error or a redirect; the crawl now reads every canonical a page names, so
  that status is known even when nothing links there.
- `llms_txt.full: true` writes `llms-full.txt`, the text of every page
  llms.txt lists, next to it; `ai.llms.full.missing` notes an llms.txt
  without one.
- Experience and authorship signals: `schema.article.author` and
  `schema.article.dates` check every Article, BlogPosting or NewsArticle for
  a named author, an ISO 8601 `datePublished` and a `dateModified`;
  `about.missing` and `about.people.missing` note a site with no about page,
  or one whose structured data names no person.
- `crawl.failed`: pages a live crawl reached by a link or redirect but could
  not read, beyond what `sitemap.url.status` already reports.
- A sitemap robots.txt names but that cannot be read is `sitemap.invalid`
  with the URL and status, not "there is no sitemap.xml"; a built site with a
  page that does not parse reports it as `crawl.failed` and audits the rest,
  instead of aborting; a live site whose robots.txt shuts findable out
  reports `crawl.skipped` instead of failing the target.
- Page text keeps `<pre>` blocks as fenced code, comes from `<main>` before
  any `<article>` in a sidebar, and escapes lines that start with `#`, so
  llms-full.txt does not turn "#1 rated" into a heading; headings in it stop
  at six levels.
- The crawler tests robots.txt against the URL it fetches, so `Disallow: /*?`
  no longer skips `/search`; link following stops at 50,000 paths and marks
  the crawl truncated; repeated child sitemaps are read once and the URL cap
  is reported only when it cut something.
- Report messages quote type names and phone numbers taken from the page, and
  plan details drop control characters, so a hostile page cannot forge rows
  or send terminal escapes. The local admin page refuses a second apply on a
  target while one runs.
- The plugin's apply guard reads a command with embedded newlines or tabs as
  separate commands and treats `-out` as a value flag, so `findable apply
  -yes` can no longer slip past it on a second line. It also asks for a
  quoted `-yes`, a backslash-newline continuation, a redirect, a subshell or
  an `sh -c` wrapper, reads only the command (not the session's paths) and
  checks every command between `;`, `&&` and `|`, so a bare apply cannot hide
  behind a planned one; `hooks/guard-apply_test.sh` runs every case in CI.
- `findable -config f.yaml apply` is refused with "put the command first"
  instead of silently running a plan and exiting 0; `status` exits 1 after
  printing every row when a target cannot be read.
- The crawl refuses to follow a redirect into a path robots.txt disallows and
  lists the redirecting URL under `crawl.skipped`; canonical and redirect
  targets are tested against robots.txt as requested, trailing slash kept; it stops after a bounded
  number of requests (four per page of `max_pages`) and marks the crawl
  truncated, so dead links or endless redirects cannot hold it; and it follows
  exactly the five redirects the message names.
- Pages whose last path segment has a dot, such as `/docs/v2.0`, are crawled
  and checked: only known asset extensions (`.pdf`, `.png`, `.js`, ...) count
  as files. robots.txt rules written in non-ASCII characters match the
  percent-encoded request path, and paths ending in several slashes are one
  page.
- Page paths keep control characters percent-encoded, and the audit text
  report, `status` and the admin page drop them from page-derived text, so a
  hostile link cannot forge report rows or send terminal escapes anywhere
  they are shown.
- `crawl.shadowed`: a built directory with both `a.html` and `a/index.html`
  yields one page for `/a` and names the file that was not checked, instead
  of two pages with the same path.
- llms-full.txt leaves `#` lines inside fenced code blocks alone; page text
  keeps a `<header>` or `<footer>` inside `<main>`, `<article>` or
  `<section>`, renders every `<article>` when there is no `<main>`, and
  escapes a paragraph that begins with a code fence, quote or list marker, so
  one page cannot swallow the pages after it. llms.txt descriptions can no
  longer add links.
- `plan` and `apply` refuse a robots.txt with a `findable:start` or
  `findable:end` line without its pair and name the file to fix, instead of
  deleting the rules between a stray marker and the next block.
- Article checks accept an author given as a plain name or an `@id` reference
  to a Person in the same structured data, and dates without seconds;
  `about.people.missing` accepts a Person named as `mainEntity`, `about`,
  `author` or `creator`; a one-object BreadcrumbList `itemListElement` is not
  "empty"; LocalBusiness types get the Organization `logo` and `sameAs`
  checks; a canonical on the wrong scheme or host is reported once; merged
  redirect findings present their URLs as an example, not as the fix for
  every listed page.
- Relative links such as `other` or `../x` on a page served from a directory
  (`blog/post/index.html`) resolve against the URL the page is served at, so
  valid links are no longer reported broken or orphaned, canonical and
  hreflang checks agree with the browser, and the crawler queues the right
  URLs.
- The crawl tests robots.txt against the path as requested (trailing slash,
  `.html` and query kept), so `Disallow: /private/` is honoured; reads every
  `Sitemap:` line, not the first; ignores a UTF-8 BOM; and no longer fetches
  PDFs, images and other files a sitemap lists, nor reports them as errors.
- A robots.txt answering 5xx or 429 stops the crawl instead of counting as
  absent; a cancelled crawl returns an error instead of a partial site; a
  crawl that reads no page fails with the reason instead of a clean audit.
- Sitemaps up to 50 MB are read in full and larger ones reported as too
  large; reading stops at 50,000 URLs or 50 files and says so; every failed
  child sitemap is reported, not only the last.
- `meta.title.duplicate` and `meta.description.duplicate` also fire when the
  shared text is too short or too long; `schema.jobposting.expired` no longer
  fires on the last day of a date-only `validThrough`.
- `audit -exit-code` exits 1 when any target is unreadable, whatever the
  order; the text report shows that target as an error, not `0 findings`.
  `-h` exits 0.
- A saved plan pins the write directory and a SHA-256 of each generated file,
  so `apply` refuses a plan whose files or destination changed after review.
- llms.txt descriptions are collapsed to one line, so a meta description
  cannot add headings or links to it.
- Config: `url:` with `base_url:` is rejected (the crawled origin is the
  judged one; before, `url:` was silently never fetched); a config with no
  targets is rejected.
- CI pins the plugin validator and goreleaser to exact versions; the release
  checkout no longer keeps the token in git config.

## 0.1.0 - 2026-10-13

- `audit`: technical SEO, structured data, hreflang, local NAP and AI search
  checks over a built directory or a live crawl, every finding with its pages
  and a fix; `-exit-code` gates CI.
- The `site` provider: `plan` lists findings as MANUAL and the files it can
  write; `apply` writes the robots.txt AI crawler block and llms.txt, then
  verifies.
- Skills for setup, plan and apply, SEO audit, schema, international, AI
  search, local SEO, off-site, content planning, conversion, measurement and
  launch; subagents per area; the `audit` and `reach` commands; evals.
- `ui`: the admin page. `mcp`: the MCP server with an `audit` tool.
- A live crawl stays on the site's host: redirects and sitemaps elsewhere are
  reported, not followed. It honours robots.txt for the `findable` token, reads
  `.xml.gz` sitemaps, fetches each URL as the sitemap lists it, and reports
  sitemap URLs that fail (`sitemap.url.status`) and what it could not read
  (`crawl.skipped`, `crawl.truncated`).
