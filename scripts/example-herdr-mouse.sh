#!/bin/bash
set -euo pipefail

# Recreate the Herdr mouse session used by the mouse-input integration demo.
#
# Usage:
#   scripts/example-herdr-mouse.sh [trace.twee] [project-directory]
#
# TWEE_BIN and HERDR_BIN may point at non-PATH builds. The script uses an
# isolated HOME/config directory so Herdr's first-run tutorial is deterministic
# and no persistent Herdr server is created.

TWEE_BIN=${TWEE_BIN:-twee}
HERDR_BIN=${HERDR_BIN:-herdr}
trace=${1:-herdr-mouse-demo.twee}
project_dir=${2:-$PWD}
session=herdr-mouse-demo-$$
demo_root=$(mktemp -d)

mkdir -p "$demo_root/home"
export TWEE_SESSION=$session

cleanup() {
	"$TWEE_BIN" stop >/dev/null 2>&1 || true
	rm -rf "$demo_root"
}
trap cleanup EXIT

command -v "$TWEE_BIN" >/dev/null
command -v "$HERDR_BIN" >/dev/null
command -v jq >/dev/null

find_cell() {
	local label=$1
	local match

	match=$("$TWEE_BIN" find --pattern "$label")
	jq -er \
		'if (.data | length) > 0 then .data[0] | "\(.x) \(.y)" else error("label not found") end' \
		<<<"$match"
}

click_label() {
	local label=$1
	local x
	local y

	read -r x y < <(find_cell "$label")
	"$TWEE_BIN" click --x "$x" --y "$y" --button left
}

"$TWEE_BIN" start \
	--cols 120 \
	--rows 36 \
	--dir "$project_dir" \
	--trace "$trace" \
	--env "HOME=$demo_root/home" \
	--env "HERDR_CONFIG_PATH=$demo_root/config.toml" \
	-- "$HERDR_BIN" --no-session >/dev/null

"$TWEE_BIN" wait text --pattern continue >/dev/null
click_label continue

# Exercise settings controls and a concrete theme option.
"$TWEE_BIN" wait text --pattern theme >/dev/null
click_label theme
"$TWEE_BIN" wait text --pattern tokyo-night >/dev/null
click_label tokyo-night
click_label apply
"$TWEE_BIN" wait no-text --pattern tokyo-night >/dev/null
"$TWEE_BIN" wait stable --quiet 100ms >/dev/null

# Verify right-click context menus, then invoke Split right from that menu.
"$TWEE_BIN" click --x 60 --y 12 --button right
"$TWEE_BIN" wait text --pattern "Split right" >/dev/null
click_label "Split right"
"$TWEE_BIN" wait no-text --pattern "Split right" >/dev/null
"$TWEE_BIN" wait cursor --x 90 --y 6 >/dev/null

# Focus the left pane, the Agents sidebar, and finally the right pane. The
# cursor waits prove both pane transitions were consumed; making the right
# pane last also orders the otherwise non-rendering sidebar click before an
# observable UI response and before session teardown.
"$TWEE_BIN" click --x 50 --y 20 --button left
"$TWEE_BIN" wait cursor --x 43 --y 6 >/dev/null
"$TWEE_BIN" click --x 4 --y 19 --button left
"$TWEE_BIN" click --x 95 --y 20 --button left
"$TWEE_BIN" wait cursor --x 90 --y 6 >/dev/null

"$TWEE_BIN" stop >/dev/null
trap - EXIT
rm -rf "$demo_root"

"$TWEE_BIN" inspect "$trace" >/dev/null
echo "Recording is $trace"
