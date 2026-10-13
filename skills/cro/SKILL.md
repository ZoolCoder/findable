---
name: cro
description: Turn more visitors into leads, applications or sales (conversion rate optimisation): calls to action, forms, trust, speed and page structure, measured with real data and without dark patterns. Use when the user asks about "conversions", "CRO", "more leads", "more applications", "the form doesn't convert", "landing page", or "A/B test".
license: MIT
compatibility: Needs the site source; results need analytics (the measure skill).
---

# Conversion

## 1. Measure first

Know the current rate per page and per step before changing anything: visits, starts, submits (the `measure` skill). Without a baseline no change can be judged.

## 2. Review each key page

| Question | Look for |
|---|---|
| Does the first screen say what this is, for whom, and what to do next? | one headline, one primary button, no carousel |
| Is the next step obvious everywhere? | the same primary action in the header, after each section, at the end |
| Is the form as short as it can be? | only the fields needed now; labels above fields; errors beside the field; works with autofill and on a phone |
| Is there proof? | real numbers, named clients with consent, real photos, licences, reviews from real customers |
| Does it answer the objections? | price or how pricing works, timelines, what happens after submit |
| Is it fast and stable on a phone? | Lighthouse mobile: LCP under 2.5 s, INP under 200 ms, CLS under 0.1 |
| Does every visitor get it? | contrast, keyboard, screen reader, every site language |

Write findings as a list: page, problem, change, expected effect, how it will be measured.

## 3. Test

- Change one thing at a time on high-traffic pages, or run an A/B test when traffic allows. Low traffic: make the change, compare four weeks before and after, say the result is indicative.
- Do not call a test before it has enough conversions per variant.

## Never

- Dark patterns: fake countdowns, fake scarcity ("2 left"), pre-ticked consent, confirmshaming, hidden costs, hard-to-cancel flows.
- Invented testimonials, numbers or logos.
- Tracking before consent where the law requires consent (EU, UK).
