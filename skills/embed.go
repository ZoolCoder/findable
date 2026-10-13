// Package skills embeds the skill texts, so the MCP server can serve them as
// prompts from a binary installed without this repository.
package skills

import "embed"

//go:embed */SKILL.md
var FS embed.FS
