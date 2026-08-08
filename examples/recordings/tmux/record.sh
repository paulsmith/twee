#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$repo_root/bin/twee"}
record_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
trace="$record_dir/tmux.twee"
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/twee-tmux.XXXXXX")
session="record-tmux-$$"

cleanup() {
  "$TWEE_BIN" --name "$session" stop --grace 0 >/dev/null 2>&1 || true
  tmux -L t kill-server >/dev/null 2>&1 || true
  rm -rf "$work_dir"
}
trap cleanup EXIT

mkdir -p "$work_dir/home" "$work_dir/state"
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

type_slow "tmux -L t new -s ops"
key Enter
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null

type_slow "echo API:5xx"
key Enter
key Ctrl+B
type_slow "%"
sleep 1

type_slow "echo Queue:312"
key Enter
key Ctrl+B
key Left
type_slow "echo Pause:retry"
key Enter
key Ctrl+B
key Right
type_slow "echo Owner:oncall"
key Enter
sleep 2

type_slow "exit"
key Enter
sleep 1
type_slow "exit"
key Enter
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null
type_slow "exit"
key Enter
"$TWEE_BIN" --name "$session" wait exit >/dev/null
"$TWEE_BIN" inspect "$trace"
