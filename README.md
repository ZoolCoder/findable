<p align="center">
<img src=".github/mark.png" width="96" alt="findable">
</p>

<h1 align="center">findable</h1>

<p align="center">
<a href="https://github.com/ZoolCoder/findable/releases/latest"><img src="https://img.shields.io/github/v/release/ZoolCoder/findable?label=release" alt="Latest release"></a>
<a href="https://github.com/ZoolCoder/findable/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/ZoolCoder/findable/ci.yml?label=ci" alt="CI"></a>
<a href="https://zoolcoder.github.io/findable/"><img src="https://img.shields.io/badge/docs-manual-7C3AED" alt="Manual"></a>
<a href="LICENSE"><img src="https://img.shields.io/github/license/ZoolCoder/findable" alt="MIT"></a>
<a href="https://github.com/ZoolCoder/findable/issues"><img src="https://img.shields.io/github/issues/ZoolCoder/findable" alt="Issues"></a>
</p>

An SEO and AI-search audit for websites, with agent skills that turn the
findings into fixes and a reach plan. The Go tool is deterministic and tested,
so CI can gate a deploy on it; the skills and subagents do the judgement work.

```console
$ findable audit
== site: 2 findings

warn  meta.h1.missing  the page has no <h1>
      fix: Give the page one visible <h1> that states its subject.
      /
$ findable plan -out plan.json
CREATE  site  llms.txt    generated from the site's titles and descriptions, 180 bytes; rebuild and deploy to publish it
UPDATE  site  robots.txt  AI crawler block, from ai_crawlers, 354 bytes; rebuild and deploy to publish it
$ findable apply plan.json
applied and verified
```

It checks titles, descriptions, canonicals, noindex, links, the sitemap,
robots.txt, hreflang, JSON-LD (Organization, LocalBusiness, JobPosting,
BreadcrumbList), AI crawler rules and llms.txt, on a built directory or a live
crawl. It writes only two files: the AI crawler block in robots.txt and
llms.txt.

As a Claude Code plugin it adds skills for SEO audits, schema, international,
AI search, local, off-site, content, conversion, measurement and launch, four
subagents, and `/findable:audit` and `/findable:reach`. Adapters carry the same
rules to Codex, Cursor, Gemini CLI and Copilot.

## Install

```console
$ brew tap zoolcoder/findable https://github.com/zoolcoder/findable
$ brew install --cask findable
```

or `go install github.com/zoolcoder/findable/cmd/findable@latest`.

## Use

```console
$ cp findable.example.yaml findable.yaml
$ findable validate
$ findable audit
$ findable plan -out plan.json
$ findable apply plan.json
$ findable doctor
```

The [manual](https://zoolcoder.github.io/findable) covers the config,
every check and command, the admin page and the agent surfaces.
[NOTICE.md](NOTICE.md) credits the projects the skills learned from.

## Develop

```console
$ make lint test build
```

See [CONTRIBUTING.md](CONTRIBUTING.md). Security reports go through
[SECURITY.md](SECURITY.md).
