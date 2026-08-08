#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$repo_root/bin/twee"}
record_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
trace="$record_dir/fzf.twee"
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/twee-fzf.XXXXXX")
session="record-fzf-$$"

cleanup() {
  "$TWEE_BIN" --name "$session" stop --grace 0 >/dev/null 2>&1 || true
  rm -rf "$work_dir"
}
trap cleanup EXIT

mkdir -p "$work_dir/home" "$work_dir/state"
printf '%s\n' \
  'cache/flush-production - Clear stale edge content' \
  'deploy/rollback-web - Revert the failed web release' \
  'database/restore-readonly - Restore reporting replica access' \
  'incident/compose-update - Draft a customer status update' >"$work_dir/runbooks"
printf '%s\n' \
  "pick() { choice=\$(fzf --prompt='Runbook> ' < runbooks); }" \
  'PS1="runbooks$ "' >"$work_dir/init.sh"
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
  sleep 0.7
}

rm -f "$trace"
"$TWEE_BIN" --name "$session" start --cols 80 --rows 18 --dir "$work_dir" \
  --trace "$trace" -- bash --noprofile --rcfile "$work_dir/init.sh" -i >/dev/null
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null

type_slow "pick"
key Enter
"$TWEE_BIN" --name "$session" wait text --pattern 'Runbook>' >/dev/null
type_slow "deploy"
sleep 1
key Enter
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null

type_slow "echo \"\$choice\""
key Enter
sleep 4
type_slow "echo rollback-now"
key Enter
sleep 2
type_slow "exit"
key Enter
"$TWEE_BIN" --name "$session" wait exit >/dev/null
"$TWEE_BIN" inspect "$trace"
