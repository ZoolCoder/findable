#!/bin/sh
# Runs guard-apply.sh over sample Bash tool inputs and checks it asks or stays
# silent. Each command is written as it arrives in the hook's JSON, so \n, \t,
# \" and \\ are the literal escape sequences.
here=$(dirname "$0")
failures=0

check() {
	want=$1
	command=$2
	# The session fields carry the word findable, as they do in this repository.
	got=$(printf '{"session_id":"a1","transcript_path":"/Users/me/.claude/projects/-Users-me-Tools-findable/a1.jsonl","cwd":"/Users/me/Tools/findable","tool_name":"Bash","tool_input":{"command":"%s","description":"apply"}}' "$command" | sh "$here/guard-apply.sh")
	case $got in
	*'"permissionDecision":"ask"'*) have=ask ;;
	'') have=silent ;;
	*) have="unexpected output: $got" ;;
	esac
	if [ "$have" != "$want" ]; then
		printf 'FAIL want %s, got %s: %s\n' "$want" "$have" "$command"
		failures=$((failures + 1))
	fi
}

check ask 'findable apply -yes\necho done'
check ask 'findable apply\nls'
check ask 'findable apply\t-yes'
check ask 'findable apply -out x.json'
check ask 'findable apply -out=x.json'
check silent 'findable apply plan.json'
check ask 'findable apply'
check silent 'findable audit'
check ask 'findable apply > out'
check ask 'findable apply >/dev/null'
check ask 'findable apply 2>&1'
check ask 'findable apply \"-yes\"'
check ask 'findable apply plan.json -yes'
check ask 'findable apply \\\n-yes'
check ask 'findable \\\napply --yes'
check ask '(findable apply)'
check ask 'sh -c '"'"'findable apply'"'"
check ask 'echo $(findable apply -yes)'
check ask 'findable apply && ls'
check silent 'findable apply -target a plan.json'
check silent 'findable apply plan.json > out'
check ask 'findable apply; findable apply plan.json'
check ask 'findable apply plan.json && findable apply'
check silent 'go test ./... apply'

if [ "$failures" -ne 0 ]; then
	printf '%s case(s) failed\n' "$failures"
	exit 1
fi
echo ok
