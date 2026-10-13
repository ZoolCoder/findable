---
name: local-seo
description: Grow local search and Google Maps visibility for a business with offices or a service area: Google Business Profile, Bing Places, Apple Business Connect, NAP consistency, location pages, LocalBusiness schema and reviews. Use when the user asks about "local SEO", "Google Maps", "Google Business Profile", "GBP", "near me", "reviews", or a findable local.* finding.
license: MIT
compatibility: findable checks NAP in structured data when business is set in findable.yaml.
---

# Local SEO

## 1. Profiles

One profile per real location the public can visit or that serves an area:

- Google Business Profile: primary category as specific as it exists (for a recruiter, "Employment agency"), services, hours, photos of the real place and team, the site URL with the location page, not the home page.
- Bing Places (it can import from Google) and Apple Business Connect.
- The owner creates and verifies these. Agents prepare the content; never sign in, verify or post on the owner's behalf without their explicit yes.

## 2. NAP: one name, address and phone

The same business name, address and phone number on the site, every profile and every directory. Set it in findable.yaml:

```yaml
business:
  name: Example Recruiting
  phones: ["+40 21 000 0000"]
```

findable then flags `local.nap.mismatch` when the JSON-LD names the business differently or lists another phone. A branch may add its place after the name ("Example Recruiting — Cluj").

## 3. Location pages

One page per location with: the address, a map, hours, the phone, who works there, what this office does, directions, and local proof (clients, jobs, events in that city). Mark it up as a LocalBusiness subtype (the `schema` skill). Never make city pages for places with no office or real service: doorway pages are a spam policy violation.

## 4. Reviews

- Ask every real customer for a review, with a direct link. Reply to every review, the bad ones calmly and fast.
- **Never** write fake reviews, buy them, offer incentives for them, or gate the ask so only happy customers get it. All of these break Google's policy and consumer-protection law (the US FTC rule on fake reviews, the EU unfair commercial practices rules).
- Never put self-written ratings in the site's JSON-LD.

## 5. Citations

List the business in the directories that matter in its country and industry, with the same NAP. Quality over count (see the `offsite` skill).
