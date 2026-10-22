<p align="center">
<picture>
<source media="(prefers-color-scheme: dark)" srcset=".github/mark-dark.png">
<img src=".github/mark.png" width="96" alt="findable">
</picture>
</p>

<h1 align="center">findable</h1>

<p align="center">
<a href="https://github.com/ZoolCoder/findable/releases/latest"><img src="https://img.shields.io/github/v/release/ZoolCoder/findable?label=release" alt="Latest release"></a>
<a href="https://github.com/ZoolCoder/findable/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/ZoolCoder/findable/ci.yml?branch=main&label=ci" alt="CI"></a>
<a href="https://zoolcoder.github.io/findable/"><img src="https://img.shields.io/badge/docs-manual-7C3AED" alt="Manual"></a>
<a href="LICENSE"><img src="https://img.shields.io/github/license/ZoolCoder/findable" alt="MIT"></a>
<a href="https://github.com/ZoolCoder/findable/issues"><img src="https://img.shields.io/github/issues/ZoolCoder/findable" alt="Issues"></a>
</p>

Make a website easy to find, in search engines and in AI answers.

`findable audit` reads a built site or crawls a live one and reports what keeps
it from ranking or being cited: titles, canonicals, noindex, the sitemap,
robots.txt, hreflang, structured data, AI crawler rules and llms.txt. Every
finding names its pages and the fix. The audit is a deterministic, tested Go
binary, so CI can gate a deploy on it.

The judgement work belongs to agents. As a Claude Code plugin, findable adds
skills and subagents that trace each finding to the source file that causes it,
fix it there with your approval, and plan reach beyond the code: schema, local,
off-site, content and conversion.

```console
$ findable audit
== site: 3 findings

error  sitemap.url.status  the sitemap lists a URL that answers 404
       fix: List only URLs that answer 200 with the page itself: drop it, or list where it redirects.
       /jobs/welder-2024
warn   meta.title.length  the title is over 60 characters and is cut short in results
       fix: Put the words a searcher types first; keep it under 60 characters.
       /services/recruitment, /about
info   schema.localbusiness.fields  LocalBusiness has no telephone
       fix: Add telephone to the LocalBusiness block.
       /contact

$ findable plan -out plan.json
CREATE  site  llms.txt    generated from the site's titles and descriptions, 1840 bytes; rebuild and deploy to publish it
UPDATE  site  robots.txt  AI crawler block, from ai_crawlers, 354 bytes; rebuild and deploy to publish it
MANUAL  site  sitemap.url.status  [error] /jobs/welder-2024: ...

$ findable apply plan.json
applied and verified
```

**Documentation: <https://zoolcoder.github.io/findable/>**

## What it checks

| Area | Checks |
|---|---|
| Pages | title and description (missing, length, duplicates), one `<h1>`, `lang`, canonical, Open Graph card, image `alt`, thin content, titles that do not name the brand (`brand:`), a page's target query missing from title, h1 or description (`queries:`) |
| Indexing | noindex by meta or `X-Robots-Tag`, noindex pages in the sitemap, broken internal links, orphan pages |
| Sitemap and robots.txt | missing or invalid (`.xml.gz` too), URLs on another host or not canonical, URLs that 404 or redirect (a missing trailing slash), redirect chains of two or more hops, two files serving one path, canonical, hreflang and internal links that only reach their page through a redirect, a canonical pointing at a page that does not exist or does not answer 200, temporary 302/303/307 redirects, Googlebot or Bingbot blocked |
| International | hreflang codes, self links, reciprocity, `x-default` |
| Structured data | JSON-LD that parses; Organization, LocalBusiness, JobPosting and BreadcrumbList fields; expired jobs; the types each kind of page must carry; Organization without `sameAs`, home page without a WebSite block named for the brand; articles without a named author or ISO 8601 dates; an about page that names nobody |
| Local | the business name and phones (NAP) in the JSON-LD match the config |
| AI search | robots.txt against your policy for AI search, user and training crawlers; llms.txt present, well formed, linking real pages |

Each check has a stable id, such as `meta.title.length`. The
[check list](https://zoolcoder.github.io/findable/checks.html) follows Google
Search Central, RFC 9309, sitemaps.org, llmstxt.org and each AI vendor's
crawler documentation.

## What it writes

Up to three files, and only into the `write_dir` the config names:

- the AI crawler block in robots.txt, between `# findable:start` and
  `# findable:end`, from the `ai_crawlers` policy you set;
- llms.txt, generated from the site's indexable pages;
- llms-full.txt, the text of every page llms.txt lists, when `llms_txt.full` is set.

Everything else is a `MANUAL` action: a fix for a person or an agent to make in
the site's source. `plan` shows every write before `apply` makes it, and
`apply` reads each file back to verify it.

## Agents

| Surface | What it adds |
|---|---|
| Skills | setup, plan and apply, SEO audit, schema, international, AI search, local SEO, off-site, content plan, conversion, measurement, launch |
| Subagents | `technical-seo`, `ai-search`, `local-offsite`, `content-cro`: read-only, each returns a report |
| Commands | `/findable:audit` works through the findings; `/findable:plan` and `/findable:apply` preview and make guarded writes; `/findable:doctor` checks the install; `/findable:reach` runs the four subagents and merges one plan |
| MCP | `findable mcp` serves audit, status, validate, plan, apply and doctor as tools |
| Adapters | the same rules for Codex, Cursor, Gemini CLI, Copilot and cloud agents |

The skills ask before editing a file and never publish, deploy or contact
anyone on their own. They never fake reviews, cloak pages or invent facts.
[Eval cases](evals/) hold them to those rules.

## Install

```console
$ brew tap zoolcoder/findable https://github.com/zoolcoder/findable
$ brew install --cask findable
```

Or `go install github.com/zoolcoder/findable/cmd/findable@latest`, or a binary,
deb, rpm or apk from the [latest release](https://github.com/ZoolCoder/findable/releases/latest).

The Claude Code plugin:

```console
/plugin marketplace add zoolcoder/findable
/plugin install findable@findable
```

## Use

```console
$ cp findable.example.yaml findable.yaml   # a built directory, or url: for a live crawl
$ findable validate
$ findable audit
$ findable plan -out plan.json
$ findable apply plan.json
```

In CI, `findable audit -exit-code` exits 2 when it finds an error. `findable ui`
opens a local admin page, and `findable doctor` checks the install.

findable runs on your machine and sends nothing anywhere but the site it
crawls. The crawl stays on that site's host, honours robots.txt for the
`findable` token and stops at `max_pages`.

## Develop

```console
$ make lint test build
$ make evals          # the plugin eval suite, needs the claude CLI
```

See [CONTRIBUTING.md](CONTRIBUTING.md). Report security problems as
[SECURITY.md](SECURITY.md) describes. [NOTICE.md](NOTICE.md) credits the
projects the skills learned from.

MIT licensed.
