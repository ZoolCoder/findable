---
name: schema
description: Add and fix schema.org structured data (JSON-LD) so pages qualify for Google rich results and read clearly to AI search. Covers Organization, LocalBusiness, JobPosting, BreadcrumbList, Article, Product, Event and FAQ, and Google's policies on each. Use when the user asks about "schema", "structured data", "JSON-LD", "rich results", "rich snippets", or a findable schema.* finding.
license: MIT
compatibility: findable checks the result; Google's Rich Results Test confirms eligibility.
---

# Structured data

## Choose the types

| Page | Type | Notes |
|---|---|---|
| Home | `Organization` (or the most specific subtype) | name, url, logo, sameAs (official profiles), contactPoint |
| Each office or shop | `LocalBusiness` subtype, one per location | name, address, telephone, url, openingHours, geo |
| A job's own page | `JobPosting` | one per page; never on a list of jobs |
| Every page below home | `BreadcrumbList` | positions 1, 2, 3; every item but the last has a URL |
| Blog and news | `Article` / `NewsArticle` | headline, datePublished, dateModified, author, image |
| Products, events | `Product`, `Event` | price and availability, or date and place |

Write the matching rules into `schema:` in findable.yaml so every future page is checked.

## JobPosting (Google job search)

- Required: title, description (HTML allowed), datePosted, hiringOrganization with a name, jobLocation (or applicantLocationRequirements with `jobLocationType: TELECOMMUTE`).
- Recommended: baseSalary, employmentType, validThrough. Job search filters on them.
- When a job closes, expire it one of the three ways Google accepts: a `validThrough` in the past, removing the page (404/410), or removing the `JobPosting` markup. Say on the page that the role is filled and link to open ones.
- **Never** keep a filled job marked up as open (a future or missing `validThrough` on a closed role). Expired listings left open are a policy violation and can cost the whole site its job listings.
- Ask whether a flagged job is really closed before fixing it: an open role needs a real `validThrough`, not less markup.
- Whenever you set a future `validThrough`, tell the owner in the same reply how to expire the posting the day the role is filled (one of the three ways above). A date alone is not a fix.
- Use the Indexing API only for JobPosting and livestream pages. Google limits it to those.

## Rules Google enforces

- Mark up only what the page shows. Every value in the JSON-LD must be visible to the reader.
- No self-serving reviews: an `AggregateRating` a business gives itself is not eligible for stars. **Never** write ratings or reviews that real customers did not give.
- FAQ rich results show only for well-known government and health sites. FAQPage markup still helps AI answers, but promise no stars or dropdowns.
- HowTo rich results are gone. Skip HowTo markup.
- One entity, one name: the Organization name matches the logo, the site and the Google Business Profile. findable's `local.nap.mismatch` checks this when `business:` is set.

## Check

Rebuild, then `findable audit`, then Google's Rich Results Test on one page of each type. Fix in the JSON-LD builder, not page by page.
