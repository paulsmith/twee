#!/usr/bin/env bash
# Record a human-paced tree inspection from Bash.
set -euo pipefail

APP_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(CDPATH='' cd -- "$APP_DIR/../../.." && pwd)
TWEE_BIN=${TWEE_BIN:-"$REPO_ROOT/bin/twee"}
TRACE="$APP_DIR/tree.twee"
SESSION="twee-tree-$$"
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/twee-tree.XXXXXX")
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

mkdir -p \
  "$HOME_DIR" \
  "$WORK_DIR/release/services/api/config" \
  "$WORK_DIR/release/services/worker" \
  "$WORK_DIR/release/docs" \
  "$WORK_DIR/release/build" \
  "$WORK_DIR/release/node_modules/stub"
printf '%s\n' '# API deployment notes' > "$WORK_DIR/release/services/api/README.md"
printf '%s\n' 'port: 8080' > "$WORK_DIR/release/services/api/config/default.yaml"
printf '%s\n' '# Worker handoff' > "$WORK_DIR/release/services/worker/README.md"
printf '%s\n' '# Release runbook' > "$WORK_DIR/release/docs/runbook.md"
printf '%s\n' 'generated cache' > "$WORK_DIR/release/build/cache.tmp"
printf '%s\n' 'module stub' > "$WORK_DIR/release/node_modules/stub/index.js"
printf '%s\n' "PS1='release$ '" > "$WORK_DIR/bashrc"

rm -f "$TRACE"
"$TWEE_BIN" start --name "$SESSION" --cols 100 --rows 28 --dir "$WORK_DIR" \
  --env "HOME=$HOME_DIR" --trace "$TRACE" -- \
  bash --noprofile --rcfile "$WORK_DIR/bashrc" -i >/dev/null
"$TWEE_BIN" wait text --name "$SESSION" --pattern 'release$' >/dev/null
sleep 2

type_slow "tree -L 3 -I '*.tmp|node_modules' release"
key Enter
"$TWEE_BIN" wait text --name "$SESSION" --pattern 'services' >/dev/null
sleep 2
type_slow "tree -P '*.md' --prune release"
key Enter
"$TWEE_BIN" wait text --name "$SESSION" --pattern 'runbook.md' >/dev/null
sleep 2
type_slow 'exit'
key Enter 0.3
"$TWEE_BIN" wait exit --name "$SESSION" --timeout 15s >/dev/null
"$TWEE_BIN" bundle validate "$TRACE"
"$TWEE_BIN" bundle info "$TRACE"
