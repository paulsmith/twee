#!/usr/bin/env bash
# Record a human-paced htop inspection.
set -euo pipefail

APP_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(CDPATH='' cd -- "$APP_DIR/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$REPO_ROOT/bin/twee"}
TRACE="$APP_DIR/htop.twee"
SESSION="twee-htop-$$"
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/twee-htop.XXXXXX")
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

mkdir -p "$HOME_DIR"
rm -f "$TRACE"
"$TWEE_BIN" start --name "$SESSION" --cols 100 --rows 28 --dir "$WORK_DIR" \
  --env "HOME=$HOME_DIR" --trace "$TRACE" -- htop -d 20 >/dev/null
"$TWEE_BIN" wait stable --name "$SESSION" --quiet 500ms >/dev/null
sleep 3

type_slow 'P'
sleep 4
type_slow '/'
"$TWEE_BIN" wait stable --name "$SESSION" --quiet 250ms >/dev/null
type_slow 'htop'
"$TWEE_BIN" wait stable --name "$SESSION" --quiet 250ms >/dev/null
sleep 3
"$TWEE_BIN" key Enter --name "$SESSION" >/dev/null
sleep 3
type_slow 'M'
sleep 5
type_slow 'q'
"$TWEE_BIN" wait exit --name "$SESSION" --timeout 15s >/dev/null
"$TWEE_BIN" bundle validate "$TRACE"
"$TWEE_BIN" bundle info "$TRACE"
