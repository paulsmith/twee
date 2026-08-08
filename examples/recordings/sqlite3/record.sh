#!/usr/bin/env bash
set -euo pipefail

if [[ ${TWEE_RECORD_RLWRAP:-} != 1 ]]; then
  exec nix shell nixpkgs#rlwrap --command env TWEE_RECORD_RLWRAP=1 bash "$0"
fi

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$repo_root/bin/twee"}
record_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
trace="$record_dir/sqlite3.twee"
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/twee-sqlite3.XXXXXX")
session="record-sqlite3-$$"

cleanup() {
  "$TWEE_BIN" --name "$session" stop --grace 0 >/dev/null 2>&1 || true
  rm -rf "$work_dir"
}
trap cleanup EXIT

mkdir -p "$work_dir/home" "$work_dir/state"
sqlite3 "$work_dir/inventory.db" \
  "CREATE TABLE stock (sku TEXT PRIMARY KEY, qty INTEGER, reorder INTEGER); INSERT INTO stock VALUES ('usb', 6, 12), ('hdmi', 21, 10), ('dock', 2, 5);"
export HOME="$work_dir/home"
export XDG_STATE_HOME="$work_dir/state"

type_slow() {
  local value=$1
  local index
  for ((index = 0; index < ${#value}; index++)); do
    "$TWEE_BIN" --name "$session" type -- "${value:index:1}" >/dev/null
    sleep 0.2
  done
}

key() {
  "$TWEE_BIN" --name "$session" key "$1" >/dev/null
  sleep 0.4
}

rm -f "$trace"
"$TWEE_BIN" --name "$session" start --cols 80 --rows 18 --dir "$work_dir" \
  --trace "$trace" -- bash --noprofile --norc -i >/dev/null
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null

type_slow "rlwrap sqlite3 -box -header inventory.db"
key Enter
"$TWEE_BIN" --name "$session" wait text --pattern 'sqlite>' >/dev/null
type_slow "select *from stock;"
key Enter
"$TWEE_BIN" --name "$session" wait text --pattern 'usb' >/dev/null
sleep 1
type_slow "update stock set qty=18;"
key Enter
type_slow "select *from stock;"
key Enter
"$TWEE_BIN" --name "$session" wait text --pattern '18' >/dev/null
sleep 1
key Ctrl+D
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null
type_slow "exit"
key Enter
"$TWEE_BIN" --name "$session" wait exit >/dev/null
"$TWEE_BIN" inspect "$trace"
