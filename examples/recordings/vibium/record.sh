#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$repo_root/bin/twee"}
VIBIUM_BIN=${VIBIUM_BIN:-/bin/vibium}
trace="$script_dir/vibium.twee"
session="vibium-renewal-$$"
demo_root=$(mktemp -d)
port=$((18000 + RANDOM % 1000))

export TWEE_SESSION=$session

cleanup() {
	"$TWEE_BIN" stop >/dev/null 2>&1 || true
	if [[ -n ${server_pid:-} ]]; then
		kill "$server_pid" >/dev/null 2>&1 || true
		wait "$server_pid" 2>/dev/null || true
	fi
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
command -v "$VIBIUM_BIN" >/dev/null
mkdir -p "$demo_root/site" "$demo_root/work"
printf '%s\n' 'PS1="vibium$ "' 'clear' >"$demo_root/bashrc"
# shellcheck disable=SC2016 # JavaScript template literals must reach the browser unchanged.
printf '%s\n' \
	'<!doctype html><title>Renewal handoff</title><main><h1>Renewal handoff</h1><p>Northwind annual renewal</p><label>Owner <input id="owner" placeholder="Account owner"></label><label>Plan <select id="tier"><option>Standard</option><option>Expansion</option></select></label><label><input id="notify" type="checkbox"> Notify account team</label><button id="save" onclick="save()">Save handoff</button><p id="status" aria-live="polite">Draft not saved</p></main><script>function save() { const owner = document.querySelector("#owner").value; const tier = document.querySelector("#tier").value; const notify = document.querySelector("#notify").checked ? "on" : "off"; document.querySelector("#status").textContent = "Saved: " + owner + " / " + tier + " / notifications " + notify; }</script>' \
	>"$demo_root/site/index.html"

/bin/busybox httpd -f -p "$port" -h "$demo_root/site" >"$demo_root/http.log" 2>&1 &
server_pid=$!
for _ in {1..20}; do
	if /bin/nc -z 127.0.0.1 "$port"; then
		break
	fi
	sleep 0.1
done

rm -f "$trace"
"$TWEE_BIN" start --cols 108 --rows 30 --dir "$demo_root/work" --trace "$trace" -- \
	bash --noprofile --rcfile "$demo_root/bashrc" -i >/dev/null
"$TWEE_BIN" wait text --pattern vibium >/dev/null

type_human 'vibium --headless start'
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait stable --quiet 400ms >/dev/null
type_human "vibium go http://127.0.0.1:$port"
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait stable --quiet 400ms >/dev/null
type_human 'vibium map'
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait text --pattern 'Save handoff' >/dev/null

# One browser character per command keeps the page input as deliberate as the CLI input.
for letter in A n a; do
	type_human "vibium type '#owner' $letter"
	"$TWEE_BIN" key Enter >/dev/null
	"$TWEE_BIN" wait stable --quiet 150ms >/dev/null
done
type_human "vibium select '#tier' Expansion"
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait stable --quiet 150ms >/dev/null
type_human "vibium check '#notify'"
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait stable --quiet 150ms >/dev/null
type_human "vibium click '#save'"
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait stable --quiet 150ms >/dev/null
type_human "vibium text '#status'"
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait text --pattern 'Saved: Ana / Expansion / notifications on' >/dev/null
"$TWEE_BIN" sleep 1s >/dev/null
type_human 'vibium stop'
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait text --pattern 'Browser session closed' >/dev/null
type_human exit
"$TWEE_BIN" key Enter >/dev/null
"$TWEE_BIN" wait exit >/dev/null
trap - EXIT
cleanup
"$TWEE_BIN" bundle validate "$trace" >/dev/null
"$TWEE_BIN" bundle info "$trace"
