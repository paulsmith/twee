#!/bin/bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." >/dev/null && pwd)"
font_dir="$repo_root/internal/render/fonts"
cache_dir="${TWEE_FONT_SOURCE_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/twee/font-sources}"

noto_sans_mono_unicodes="U+2190-2195,U+219C-219E,U+21A0,U+21A2-21A4,U+21A6,U+21D0-21D4,U+21DA-21DB,U+21E6,U+21E8,U+2308-230B,U+2310,U+2319,U+2320-2321,U+2336-237A,U+2395,U+239B-23AE,U+23B0-23BD,U+23DC-23E1,U+2500-259F"
noto_sans_symbols_unicodes="U+2190-2199,U+2300-230F,U+2311-2315,U+2317,U+231C-231F,U+2322-2323,U+2329-232A,U+232C-2335,U+237C,U+2380-2394,U+2396-239A,U+23AF,U+23BE-23CD,U+23D0-23DB,U+23E2-23E8,U+260A-260D,U+2613,U+2624-262F,U+2638-263B,U+263D-2653,U+2669-267E,U+2690-269D,U+26A2-26A9,U+26AD-26BC,U+26CE,U+26E2-26FF,U+271D-2721,U+2776-2793"
noto_sans_symbols2_unicodes="U+21AF,U+21E6-21F0,U+21F3,U+2316,U+2318,U+231A-231B,U+2324-2328,U+232B,U+237D-237F,U+23CE-23CF,U+23F4-23F6,U+23FA,U+25A0-2609,U+260E-2612,U+2614-2623,U+2630-2637,U+263C,U+2654-2668,U+267F-268F,U+269E-26A1,U+26AA-26AC,U+26BD-26CD,U+26CF-26E1,U+2700-2704,U+2706-2709,U+270B-271C,U+2722-2727,U+2729-274B,U+274D,U+274F-2753,U+2756-2775,U+2794,U+2798-27AF,U+27B1-27BE,U+2800-28FF"

usage() {
	cat <<'EOF'
Usage: scripts/subset-render-fonts.sh [options]

Regenerate the embedded Noto fallback TTFs as tiny pyftsubset outputs.
The primary JetBrains Mono font is not modified.

Defaults:
  NotoSansMono-Regular.ttf      common arrows and technical symbols; U+2500-U+259F
  NotoSansSymbols-Regular.ttf   common arrows, technical, and miscellaneous symbols
  NotoSansSymbols2-Regular.ttf  supplemental symbols; U+25A0-U+28FF

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
