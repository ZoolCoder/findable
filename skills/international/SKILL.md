---
name: international
description: Set up and fix a multi-language or multi-region site for search: URL structure, hreflang, x-default, canonicals per locale, translated metadata and right-to-left languages. Use when the user asks about "hreflang", "translations and SEO", "multilingual SEO", "international SEO", or a findable hreflang.* finding.
license: MIT
compatibility: findable checks hreflang when the target lists two or more locales.
---

# International sites

## Structure

- One URL per language: `/` for the default, `/ro/`, `/ar/` for the rest (subfolders). Subfolders keep one domain's authority; ccTLDs are only worth it for a real local business per country.
- Never pick the language from the IP address or Accept-Language and redirect. Crawlers mostly come from the US and would never see the other languages. Offer a language switcher.
- Translate the title, description, h1, slug-facing text, image alt and JSON-LD. A translated body under an English title ranks as neither.

## hreflang

Every page lists every language version of itself, itself included, plus `x-default`:

```html
<link rel="alternate" hreflang="en" href="https://example.com/jobs">
<link rel="alternate" hreflang="ro" href="https://example.com/ro/jobs">
<link rel="alternate" hreflang="x-default" href="https://example.com/jobs">
```

- Absolute URLs, on the canonical host.
- Pairs must be reciprocal. A one-way link is ignored.
- Codes are ISO 639-1, optionally with an ISO 3166-1 region: `en`, `pt-br`. `uk` is Ukrainian, not the United Kingdom (`en-gb`).
- Each locale's canonical points at itself, never at the default language.

Set `locales:` in findable.yaml with the default first. findable then checks `hreflang.missing`, `self`, `xdefault`, `invalid` and `reciprocal`, and matches `noindex:` and `schema:` paths across locale prefixes.

## Right-to-left

Arabic and Hebrew pages need `<html lang="ar" dir="rtl">`. Check that the layout mirrors and that numbers and Latin brand names still read correctly.

## Local search per country

Each country with an office gets its own LocalBusiness entry and Google Business Profile (see the `local-seo` skill). Keep the business name the same everywhere.
