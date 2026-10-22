#!/bin/sh
# PreToolUse guard: asks the user before a Bash call runs `findable
# apply` with -yes, or without a saved plan file. It never blocks on its own;
# the user decides.
# Only the command field is read, so the session's paths cannot trip the match.
# The command arrives JSON-escaped, so a newline is the two characters
# backslash-n. A backslash-newline continuation is a space, any other newline
# a command separator and a tab a space. Escaped double quotes, single quotes
# and the remaining backslashes are dropped, so a quoted "-yes" reads as the
# bare flag the shell will pass on.
command=$(cat | sed -n 's/.*"command":"\(\([^"\\]*\\.\)*[^"\\]*\)".*/\1/p' |
	sed 's/\\\\\\n/ /g; s/\\n/;/g; s/\\[rt]/ /g; s/\\"//g; s/\\//g' | tr -d "'")

printf '%s' "$command" | grep -Eq 'findable[^|;&]*[[:space:]]apply([^A-Za-z0-9_./-]|$)' || exit 0

reason=""
if printf '%s' "$command" | grep -Eq '[[:space:]]-{1,2}yes([^A-Za-z0-9_-]|$)'; then
	reason="findable apply with -yes deletes without a prompt."
else
	# Each command between operators is read on its own, so a bare apply cannot
	# hide behind a later one. What follows apply, with redirections, comments,
	# value flags and their values dropped: a saved plan file is the one word
	# left.
	printf '%s\n' "$command" | tr ';&|' '\n\n\n' | while IFS= read -r segment; do
		rest=$(printf '%s' "$segment" | sed -nE 's/.*findable.*[[:space:]]apply([^"`)}]*).*/\1/p' |
			sed -E 's/#.*//; s/[0-9]*[<>]+[[:space:]]*[^[:space:]]*//g; s/-{1,2}(target|config|out)(=|[[:space:]]+)[^[:space:]]+//g; s/-{1,2}[a-z-]+//g')
		if printf '%s' "$segment" | grep -Eq 'findable.*[[:space:]]apply([^A-Za-z0-9_./-]|$)' && ! printf '%s' "$rest" | grep -Eq '[^[:space:]]'; then
			echo bare
		fi
	done | grep -q bare && reason="findable apply without a saved plan file applies whatever the live state is now, not a plan the user reviewed."
fi
[ -n "$reason" ] || exit 0

printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"%s"}}\n' "$reason"
