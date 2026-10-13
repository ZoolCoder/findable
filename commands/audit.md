---
description: Audit the site and work through the findings
argument-hint: "[target ...]"
---

Use the seo-audit skill on $ARGUMENTS (every target when empty): build the site if the output is stale, run `findable audit -json` (or the MCP `audit` tool), group the findings by severity, and for each error say where in the source it comes from and the fix. Ask before editing files, and ask the owner about every finding that is a decision rather than a bug.
