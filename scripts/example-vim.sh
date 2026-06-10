#!/bin/bash
set -euo pipefail

session=twee-$$
out=$session.twee

export TWEE_SESSION=$session

exec 3>&1 >/dev/null
twee start --trace "$out" -- vim
trap 'twee stop >/dev/null 2>&1 || true' EXIT
twee wait text --pattern VIM
twee type -- "iHello, world"
twee key Enter
twee type -- "The quick brown fox jumps over the lazy dog."
twee key Escape
twee type -- ":q!"
twee key Enter
twee wait exit
exec >&3 3>&-
echo "Recording is $out"
