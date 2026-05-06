#!/bin/bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." >/dev/null && pwd)"
font_dir="$repo_root/internal/render/fonts"
cache_dir="${TWEE_FONT_SOURCE_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/twee/font-sources}"

noto_sans_mono_unicodes="U+25B0"
noto_sans_symbols_unicodes="U+23BF"
noto_sans_symbols2_unicodes="U+23F5,U+23FA,U+2722,U+2733,U+273B,U+273D"

usage() {
	cat <<'EOF'
Usage: scripts/subset-render-fonts.sh [options]

Regenerate the embedded Noto fallback TTFs as tiny pyftsubset outputs.
The primary JetBrains Mono font is not modified.

Defaults:
  NotoSansMono-Regular.ttf      U+25B0
  NotoSansSymbols-Regular.ttf   U+23BF
  NotoSansSymbols2-Regular.ttf  U+23F5,U+23FA,U+2722,U+2733,U+273B,U+273D

Options:
  --mono <list>         Replace Noto Sans Mono code points.
  --symbols <list>      Replace Noto Sans Symbols code points.
  --symbols2 <list>     Replace Noto Sans Symbols 2 code points.
  --add-mono <list>     Append code points to Noto Sans Mono.
  --add-symbols <list>  Append code points to Noto Sans Symbols.
  --add-symbols2 <list> Append code points to Noto Sans Symbols 2.
  --cache-dir <path>    Cache full upstream font files in this directory.
  -h, --help            Show this help.

Lists are comma-separated Unicode values accepted by pyftsubset, for example:
  U+23BF,U+273D

Use --add-* when preserving the current subset and adding newly observed
missing glyphs. The script uses --no-ignore-missing-unicodes, so it fails if a
requested code point is assigned to a font that does not contain it.
EOF
}

append_unicodes() {
	local current="$1"
	local added="$2"
	if [[ -z "$current" ]]; then
		printf '%s' "$added"
	else
		printf '%s,%s' "$current" "$added"
	fi
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--mono)
		noto_sans_mono_unicodes="${2:?--mono requires a value}"
		shift 2
		;;
	--symbols)
		noto_sans_symbols_unicodes="${2:?--symbols requires a value}"
		shift 2
		;;
	--symbols2)
		noto_sans_symbols2_unicodes="${2:?--symbols2 requires a value}"
		shift 2
		;;
	--add-mono)
		noto_sans_mono_unicodes="$(append_unicodes "$noto_sans_mono_unicodes" "${2:?--add-mono requires a value}")"
		shift 2
		;;
	--add-symbols)
		noto_sans_symbols_unicodes="$(append_unicodes "$noto_sans_symbols_unicodes" "${2:?--add-symbols requires a value}")"
		shift 2
		;;
	--add-symbols2)
		noto_sans_symbols2_unicodes="$(append_unicodes "$noto_sans_symbols2_unicodes" "${2:?--add-symbols2 requires a value}")"
		shift 2
		;;
	--cache-dir)
		cache_dir="${2:?--cache-dir requires a value}"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown argument: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

mkdir -p "$cache_dir"

download_font() {
	local url="$1"
	local out="$2"
	if [[ -s "$out" ]]; then
		return
	fi
	echo "Downloading $url" >&2
	curl -fsSL "$url" -o "$out"
}

subset_font() {
	local source="$1"
	local unicodes="$2"
	local output="$3"
	echo "Subsetting $(basename "$output"): $unicodes" >&2
	uvx --from fonttools pyftsubset "$source" \
		--unicodes="$unicodes" \
		--no-ignore-missing-unicodes \
		--output-file="$output" \
		--layout-features='*' \
		--glyph-names \
		--symbol-cmap \
		--notdef-glyph \
		--notdef-outline
}

mono_source="$cache_dir/NotoSansMono-Regular.full.ttf"
symbols_source="$cache_dir/NotoSansSymbols-Regular.full.ttf"
symbols2_source="$cache_dir/NotoSansSymbols2-Regular.full.ttf"

download_font \
	"https://raw.githubusercontent.com/notofonts/noto-fonts/main/hinted/ttf/NotoSansMono/NotoSansMono-Regular.ttf" \
	"$mono_source"
download_font \
	"https://notofonts.github.io/symbols/fonts/NotoSansSymbols/hinted/ttf/NotoSansSymbols-Regular.ttf" \
	"$symbols_source"
download_font \
	"https://notofonts.github.io/symbols/fonts/NotoSansSymbols2/hinted/ttf/NotoSansSymbols2-Regular.ttf" \
	"$symbols2_source"

subset_font "$mono_source" "$noto_sans_mono_unicodes" "$font_dir/NotoSansMono-Regular.ttf"
subset_font "$symbols_source" "$noto_sans_symbols_unicodes" "$font_dir/NotoSansSymbols-Regular.ttf"
subset_font "$symbols2_source" "$noto_sans_symbols2_unicodes" "$font_dir/NotoSansSymbols2-Regular.ttf"

echo "Updated subset fonts in $font_dir" >&2
