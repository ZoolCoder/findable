---
name: setup
description: Install findable, write a first findable.yaml for a website and check it with doctor. Use when the user asks to "set up", "install" or "get started with" findable, wants to audit a site for the first time, or when doctor fails.
license: MIT
compatibility: Needs a shell with Homebrew or Go.
---

# Set up

1. Install: `brew install --cask zoolcoder/findable/findable`, or `go install github.com/zoolcoder/findable/cmd/findable@latest`.
2. Find out how the site is built. Read the build config (`package.json` scripts, `vite.config.*`, `astro.config.*`, `next.config.*`, `hugo.toml`, ...) to learn:
   - the output directory (`dist`, `build`, `out`, `_site`);
   - the static directory copied into it unchanged (`public/`, `static/`): that is `write_dir`;
   - the public origin, from the canonical tags or the sitemap: that is `base_url`;
   - the locales and which one has no URL prefix.
3. Write `findable.yaml` next to the site, from `findable.example.yaml`. One target per site:
   - `path` (a built directory, checked offline) or `url` (a live site, crawled). Prefer `path`: it is fast, repeatable and catches problems before they ship.
   - `noindex`: the paths meant to stay out of search (admin, account, checkout). Ask the user; never guess that a page is meant to be hidden.
   - `schema`: which structured data each kind of page must carry.
   - `ai_crawlers`: leave it out until the user decides. The `ai-search` skill covers that conversation.
   - `llms_txt`: only when the site has no hand-written `llms.txt`. Apply would overwrite one.
4. Build the site, then run `findable validate` and `findable doctor`. Fix every `fail` line.
5. Run `findable audit` and hand over to the `seo-audit` skill.

Never point `write_dir` at the build output: apply writes there, and the next build would wipe it. It is the source directory the build copies.
