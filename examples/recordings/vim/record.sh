#!/usr/bin/env bash
# Record a human-paced Vim handoff edit.
set -euo pipefail

APP_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(CDPATH='' cd -- "$APP_DIR/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$REPO_ROOT/bin/twee"}
TRACE="$APP_DIR/vim.twee"
SESSION="twee-vim-$$"
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/twee-vim.XXXXXX")
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

mkdir -p "$HOME_DIR"
printf '%s\n' \
  '# Incident handoff' \
  '- [ ] Verify alert routing' \
  '- [ ] Review error budget' \
  '- [ ] Post status update' \
  > "$WORK_DIR/ops-checklist.md"

rm -f "$TRACE"
"$TWEE_BIN" start --name "$SESSION" --cols 86 --rows 24 --dir "$WORK_DIR" \
  --env "HOME=$HOME_DIR" --trace "$TRACE" -- \
  vim -Nu NONE -n ops-checklist.md >/dev/null
"$TWEE_BIN" wait text --name "$SESSION" --pattern 'Incident handoff' >/dev/null
sleep 2

type_slow 'G'
type_slow 'o'
type_slow '- [ ] Capture rollback owner: Mina'
key Escape
type_slow 'o'
type_slow '- [ ] Confirm database snapshot'
key Escape
type_slow '/error'
key Enter
type_slow 'A'
type_slow ' (reviewed)'
key Escape
type_slow ':w'
key Enter
"$TWEE_BIN" wait stable --name "$SESSION" --quiet 500ms >/dev/null
sleep 2
type_slow ':q'
key Enter 0.3
"$TWEE_BIN" wait exit --name "$SESSION" --timeout 15s >/dev/null
"$TWEE_BIN" inspect "$TRACE"
