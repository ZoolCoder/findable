---
name: measure
description: Set up and read the measurements that show whether reach is growing: Google Search Console, Bing Webmaster Tools, IndexNow, privacy-respecting analytics, conversion events and AI referrals. Use when the user asks "is our SEO working", "Search Console", "Bing Webmaster", "analytics", "track conversions", "how many visitors", or "traffic from ChatGPT".
license: MIT
compatibility: The owner creates and verifies the accounts; agents read exports or APIs the owner grants.
---

# Measure

## Accounts the owner sets up

| Tool | Why |
|---|---|
| Google Search Console (domain property) | queries, clicks, index coverage, rich result status, Core Web Vitals from real users |
| Bing Webmaster Tools | Bing and Copilot, and part of ChatGPT search; import from Search Console |
| Analytics | visits and conversions; prefer a privacy-first tool or server-side counts where consent rules apply |

Verification is the owner's step (a DNS record or a meta tag). Never sign in for them. Submit the sitemap in both consoles.

## IndexNow

Bing, Yandex and others accept IndexNow pings when a page changes. A static site pings from the deploy step with the changed URLs. Google does not use IndexNow; it reads the sitemap's `lastmod`.

## What to track

- Search Console: clicks, impressions, average position by page and query, monthly. Pages with many impressions and few clicks need a better title and description.
- Index coverage: pages "crawled, not indexed" usually need more value or more internal links.
- Conversions: each form submit and key click as an event, per page and per language.
- AI referrals: visits whose referrer is chatgpt.com, perplexity.ai, copilot.microsoft.com, gemini.google.com or claude.ai.
- findable: `findable audit -json` in CI after every build; fail the deploy on errors with `-exit-code`.

## Report

Monthly, one page: clicks and conversions against last month and last year, top movers, findings fixed, what was shipped, what is next. Numbers from the tools only; never estimate figures the data does not show.
