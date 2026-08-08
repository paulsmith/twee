#!/usr/bin/env bash
# Record a human-paced Bash log investigation.
set -euo pipefail

APP_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(CDPATH='' cd -- "$APP_DIR/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$REPO_ROOT/bin/twee"}
TRACE="$APP_DIR/bash.twee"
SESSION="twee-bash-$$"
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/twee-bash.XXXXXX")
HOME_DIR="$WORK_DIR/home"

cleanup() {
  "$TWEE_BIN" stop --name "$SESSION" >/dev/null 2>&1 || true
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

if [[ ! -x "$TWEE_BIN" ]]; then
  printf 'twee binary is not executable: %s\n' "$TWEE_BIN" >&2
  exit 1
fi

type_slow() {
  local text=$1 character
  while [[ -n "$text" ]]; do
    character=${text:0:1}
    "$TWEE_BIN" type --name "$SESSION" -- "$character" >/dev/null
    sleep 0.2
    text=${text:1}
  done
}

key() {
  "$TWEE_BIN" key "$1" --name "$SESSION" >/dev/null
  sleep "${2:-0.7}"
}

mkdir -p "$HOME_DIR" "$WORK_DIR/logs"
printf '%s\n' \
  '2026-07-31T09:02:11Z INFO request_id=req-841 route=/v1/invoices status=200' \
  '2026-07-31T09:04:18Z ERROR request_id=req-842 route=/v1/charges status=502 upstream=ledger' \
  '2026-07-31T09:05:02Z ERROR request_id=req-844 route=/v1/charges status=502 upstream=ledger' \
  > "$WORK_DIR/logs/api.log"
printf '%s\n' \
  'job_id,queue,status' \
  'job-842,billing,failed' \
  'job-843,email,complete' \
  'job-844,billing,failed' \
  > "$WORK_DIR/jobs.csv"
printf '%s\n' "PS1='ops$ '" > "$WORK_DIR/bashrc"

rm -f "$TRACE"
"$TWEE_BIN" start --name "$SESSION" --cols 100 --rows 28 --dir "$WORK_DIR" \
  --env "HOME=$HOME_DIR" --trace "$TRACE" -- \
  bash --noprofile --rcfile "$WORK_DIR/bashrc" -i >/dev/null
"$TWEE_BIN" wait text --name "$SESSION" --pattern 'ops$' >/dev/null
sleep 2

type_slow "grep 'ERROR' logs/api.log"
key Enter
"$TWEE_BIN" wait text --name "$SESSION" --pattern 'request_id=req-844' >/dev/null
sleep 2
type_slow "awk -F, '/failed/{print \$1,\$2}' jobs.csv"
key Enter
"$TWEE_BIN" wait text --name "$SESSION" --pattern 'job-844 billing' >/dev/null
sleep 2
type_slow 'exit'
key Enter 0.3
"$TWEE_BIN" wait exit --name "$SESSION" --timeout 15s >/dev/null
"$TWEE_BIN" inspect "$TRACE"
