#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$repo_root/bin/twee"}
CLAUDE_BIN=${CLAUDE_BIN:-/bin/claude}
trace="$script_dir/claude.twee"
session="claude-release-$$"
demo_root=$(mktemp -d)

export TWEE_SESSION=$session

cleanup() {
	"$TWEE_BIN" stop >/dev/null 2>&1 || true
	rm -rf "$demo_root"
}
trap cleanup EXIT

type_human() {
	local value=$1
	local i
	for ((i = 0; i < ${#value}; i++)); do
		"$TWEE_BIN" type -- "${value:i:1}" >/dev/null
		sleep 0.2
	done
}

command -v "$TWEE_BIN" >/dev/null
command -v "$CLAUDE_BIN" >/dev/null
mkdir -p "$demo_root/work" "$demo_root/runtime"
printf '%s\n' 'PS1="claude$ "' 'clear' >"$demo_root/bashrc"
printf '%s\n' \
	'Release candidate notes:' \
	'- Export uses a 30-second cache.' \
	'- Audit events are retained for 7 days.' \
	'- The support runbook lacks an owner.' \
	>"$demo_root/work/brief.txt"

rm -f "$trace"
"$TWEE_BIN" start --cols 112 --rows 32 --dir "$demo_root/work" --trace "$trace" \
	--env "XDG_CONFIG_HOME=$demo_root/runtime" --env "XDG_CACHE_HOME=$demo_root/runtime/cache" \
	-- bash --noprofile --rcfile "$demo_root/bashrc" -i >/dev/null
"$TWEE_BIN" wait text --pattern claude >/dev/null

# Safe mode disables local customizations; print mode and plan permissions prevent edits and persistence.
type_human 'claude -p --safe-mode --no-chrome --no-session-persistence --permission-mode plan --allowedTools Read -- "Read only brief.txt. Do not edit files. Return three concise release-risk recommendations based only on that file."'
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait text --timeout 60s --pattern 'Export cache staleness' >/dev/null
"$TWEE_BIN" wait stable --quiet 1s --timeout 60s >/dev/null
"$TWEE_BIN" sleep 2s >/dev/null
type_human exit
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait exit >/dev/null
trap - EXIT
cleanup
"$TWEE_BIN" bundle validate "$trace" >/dev/null
"$TWEE_BIN" bundle info "$trace"
