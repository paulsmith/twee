#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$repo_root/bin/twee"}
HERDR_BIN=${HERDR_BIN:-herdr}
trace="$script_dir/herdr.twee"
session="herdr-triage-$$"
demo_root=$(mktemp -d)

export TWEE_SESSION=$session

cleanup() {
	"$TWEE_BIN" stop >/dev/null 2>&1 || true
	rm -rf "$demo_root"
}
trap cleanup EXIT

command -v "$TWEE_BIN" >/dev/null
command -v "$HERDR_BIN" >/dev/null
command -v jq >/dev/null
mkdir -p "$demo_root/home" "$demo_root/work"

type_human() {
	local value=$1
	local i
	for ((i = 0; i < ${#value}; i++)); do
		"$TWEE_BIN" type -- "${value:i:1}" >/dev/null
		sleep 0.2
	done
}

find_cell() {
	local label=$1
	local match

	match=$("$TWEE_BIN" find --pattern "$label")
	jq -er 'if (.data | length) > 0 then .data[0] | "\(.x) \(.y)" else error("label not found") end' <<<"$match"
}

click_label() {
	local label=$1
	local x
	local y

	read -r x y < <(find_cell "$label")
	"$TWEE_BIN" click --x "$x" --y "$y" --button left >/dev/null
}

rm -f "$trace"
"$TWEE_BIN" start --cols 120 --rows 36 --dir "$demo_root/work" --trace "$trace" \
	--env "HOME=$demo_root/home" --env "HERDR_CONFIG_PATH=$demo_root/config.toml" \
	-- "$HERDR_BIN" --no-session >/dev/null

"$TWEE_BIN" wait text --pattern continue >/dev/null
"$TWEE_BIN" sleep 1s >/dev/null
click_label continue
"$TWEE_BIN" wait text --pattern theme >/dev/null
"$TWEE_BIN" sleep 1s >/dev/null
click_label theme
"$TWEE_BIN" wait text --pattern tokyo-night >/dev/null
"$TWEE_BIN" sleep 1s >/dev/null
click_label tokyo-night
"$TWEE_BIN" sleep 1s >/dev/null
click_label apply
"$TWEE_BIN" wait no-text --pattern tokyo-night >/dev/null
"$TWEE_BIN" wait stable --quiet 400ms >/dev/null

# The context menu makes the topology change explicit before the split is chosen.
"$TWEE_BIN" click --x 60 --y 12 --button right >/dev/null
"$TWEE_BIN" wait text --pattern 'Split right' >/dev/null
"$TWEE_BIN" sleep 1s >/dev/null
click_label 'Split right'
"$TWEE_BIN" wait no-text --pattern 'Split right' >/dev/null
"$TWEE_BIN" wait cursor --x 90 --y 6 >/dev/null
"$TWEE_BIN" sleep 1s >/dev/null

# Record the concrete handoff in the left pane, then the next diagnostic in the right pane.
"$TWEE_BIN" click --x 50 --y 20 --button left >/dev/null
"$TWEE_BIN" wait cursor --x 43 --y 6 >/dev/null
type_human 'echo "INCIDENT: checkout latency; owner: on-call"'
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait text --pattern 'checkout latency' >/dev/null
"$TWEE_BIN" sleep 1s >/dev/null
"$TWEE_BIN" click --x 95 --y 20 --button left >/dev/null
"$TWEE_BIN" wait cursor --x 90 --y 6 >/dev/null
type_human 'echo "DIAGNOSTIC: compare p95 and error rate"'
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait text --pattern 'compare p95' >/dev/null
"$TWEE_BIN" sleep 1s >/dev/null
"$TWEE_BIN" click --x 4 --y 19 --button left >/dev/null
"$TWEE_BIN" sleep 1s >/dev/null
"$TWEE_BIN" click --x 95 --y 20 --button left >/dev/null
"$TWEE_BIN" wait cursor --x 90 --y 6 >/dev/null
"$TWEE_BIN" sleep 2s >/dev/null

# Herdr's prefix detach is the app's clean single-process exit route.
"$TWEE_BIN" key Ctrl+B >/dev/null
"$TWEE_BIN" sleep 300ms >/dev/null
type_human q
"$TWEE_BIN" wait exit >/dev/null
trap - EXIT
rm -rf "$demo_root"
"$TWEE_BIN" bundle validate "$trace" >/dev/null
"$TWEE_BIN" bundle info "$trace"
