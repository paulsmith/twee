#!/bin/bash
set -euo pipefail

session=twee-$$
out=$session.twee

export TWEE_SESSION=$session

# Typists average five characters per word, so 60 WPM is about one
# character every 200 milliseconds.
type_at_60_wpm() {
	local text=$1
	local character
	local i

	for ((i = 0; i < ${#text}; i++)); do
		character=${text:i:1}
		twee type -- "$character"
		sleep 0.2
	done
}

exec 3>&1 >/dev/null
twee start --trace "$out" -- vim
trap 'twee stop >/dev/null 2>&1 || true' EXIT
twee wait text --pattern VIM
sleep 1
type_at_60_wpm "iHello, world"
sleep 0.5
twee key Enter
type_at_60_wpm "The quick brown fox jumps over the lazy dog."
sleep 1
twee key Escape
sleep 0.5
type_at_60_wpm ":q!"
sleep 0.5
twee key Enter
twee wait exit
exec >&3 3>&-
echo "Recording is $out"
