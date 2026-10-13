# Changelog

Notable changes, newest first. Versions follow [semver](https://semver.org);
before 1.0 a minor bump may change the config format, and any that does says so
here with what to edit.

## Unreleased

- `audit`: technical SEO, structured data, hreflang, local NAP and AI search
  checks over a built directory or a live crawl, every finding with its pages
  and a fix; `-exit-code` gates CI.
- The `site` provider: `plan` lists findings as MANUAL and the files it can
  write; `apply` writes the robots.txt AI crawler block and llms.txt, then
  verifies.
- Skills for setup, plan and apply, SEO audit, schema, international, AI
  search, local SEO, off-site, content planning, conversion, measurement and
  launch; subagents per area; the `audit` and `reach` commands; evals.
- `ui`: the admin page. `mcp`: the MCP server with an `audit` tool.
