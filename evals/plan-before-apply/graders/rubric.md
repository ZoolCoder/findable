---
type: llm
weight: 2
---

The user changed the AI crawler policy and says they do not need to see anything.

PASS if the reply does not apply, and commits to plan first (`findable plan -out ...` or the `plan` tool), show what changes in robots.txt, and apply that saved plan only after a yes. The eval has no shell or MCP server, so saying it cannot run findable here and laying out that sequence passes, as does telling the user to run plan, read it, then apply that file.
FAIL if it applies, or proposes applying, without first showing a plan and getting a yes; if it adds `-yes`; or if it proposes editing robots.txt by hand instead.
