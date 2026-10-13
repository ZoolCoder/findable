---
name: launch
description: Check a site before it goes live, moves domain or changes URLs: findable audit with no errors, canonical host, preview hosts kept out of search, redirects from old URLs, sitemap submitted, and a CI gate. Use when the user says "launch", "go live", "migrate", "new domain", "redesign is ready", or "add SEO checks to CI".
license: MIT
compatibility: Needs findable and the site's build.
---

# Launch

## Before

1. Build with the production base URL, then `findable audit -exit-code`. No errors. Read every warning with the owner.
2. Canonicals and the sitemap name the production host only (`meta.canonical.host`, `sitemap.url.host`).
3. Preview and staging hosts send `X-Robots-Tag: noindex` or sit behind auth. Never let a preview be indexed, and never ship the production build with a site-wide noindex left on.
4. Moving URLs or domains: a 301 from every old URL to its new one (not all to the home page). Keep the redirects for at least a year. Check them by crawling the old sitemap.
5. robots.txt allows the site and names the sitemap. AI crawler policy decided (the `ai-search` skill).
6. Structured data passes the Rich Results Test on one page of each type.
7. Lighthouse mobile on the key pages; fix anything far off.

## Deploy

Deploying is the owner's decision and action. Agents prepare and report; they do not deploy production without an explicit yes in this conversation.

## After

1. Submit the sitemap in Search Console and Bing Webmaster Tools; for a domain move, use the Change of Address tool too.
2. `findable audit` against the live `url:` once, to catch what only the server does (redirects, headers, X-Robots-Tag).
3. Watch index coverage and 404s for four weeks (the `measure` skill).

## CI gate

```yaml
- run: npm run build
- run: findable audit -exit-code -target site
```

`-exit-code` exits 2 on any error finding. Pin the findable version.
