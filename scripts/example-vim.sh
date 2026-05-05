#!/bin/bash
set -euo pipefail

session=twee-$$

exec >/dev/null
twee start -name $session vim
trap 'twee stop -name $session >/dev/null' EXIT
twee wait text -name $session VIM
twee trace start -name $session -out $session.twee
twee type -name $session "iHello, world"
twee key -name $session Enter
twee type -name $session "The quick brown fox jumps over the lazy dog."
twee key -name $session Escape
twee type -name $session ":q!"
twee trace stop -name $session
exec >/dev/tty
echo "Recording is $session.twee"
