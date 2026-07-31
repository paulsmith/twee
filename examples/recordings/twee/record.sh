#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$repo_root/bin/twee"}
record_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
source_trace="$repo_root/examples/recordings/bash/bash.twee"
trace="$record_dir/twee.twee"
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/twee-artifact-triage.XXXXXX")
session="record-twee-$$"

cleanup() {
  "$TWEE_BIN" --name "$session" stop --grace 0 >/dev/null 2>&1 || true
  rm -rf "$work_dir"
}
trap cleanup EXIT

mkdir -p "$work_dir/home" "$work_dir/state" "$work_dir/inner-state" "$work_dir/bin"
cp "$source_trace" "$work_dir/deploy-check.twee"
cp "$TWEE_BIN" "$work_dir/bin/twee"
printf '%s\n' \
  'unset TWEE_SESSION TWEE_DAEMON_MODE TWEE_DAEMON_NAME TWEE_DAEMON_READY_FD' \
  'unset TWEE_DAEMON_LOCK_FD TWEE_DAEMON_CMD TWEE_DAEMON_COLS TWEE_DAEMON_ROWS' \
  'unset TWEE_DAEMON_DIR TWEE_DAEMON_ENV TWEE_DAEMON_TRACE' \
  'PS1="triage$ "' >"$work_dir/init.sh"
export HOME="$work_dir/home"
export XDG_STATE_HOME="$work_dir/state"
export PATH="$work_dir/bin:$PATH"

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
  sleep 0.6
}

rm -f "$trace"
"$TWEE_BIN" --name "$session" start --cols 100 --rows 24 --dir "$work_dir" \
  --env TWEE_DAEMON_MODE= --env XDG_STATE_HOME="$work_dir/inner-state" \
  --trace "$trace" -- \
  bash --noprofile --rcfile "$work_dir/init.sh" -i >/dev/null
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null

type_slow "twee bundle validate deploy-check.twee"
key Enter
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null
sleep 0.8
type_slow "twee bundle info deploy-check.twee | jq '.data | {command,duration_ms,events}'"
key Enter
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null
sleep 0.8
type_slow "twee export deploy-check.twee -o deploy-check.html --max-idle 2s"
key Enter
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null
sleep 0.8
type_slow "test -s deploy-check.html && echo 'HTML REPLAY READY'"
key Enter
"$TWEE_BIN" --name "$session" wait stable --quiet 300ms >/dev/null
sleep 1
type_slow "exit"
key Enter
"$TWEE_BIN" --name "$session" wait exit >/dev/null
"$TWEE_BIN" bundle validate "$trace"
"$TWEE_BIN" bundle info "$trace"
