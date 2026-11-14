#!/bin/sh
# Rebuilds the help center's serif font from Noto Serif (SIL Open Font License 1.1, OFL.txt here).
# Run from this directory: sh subset.sh /path/to/NotoSerif[wdth,wght].ttf
# Source: https://github.com/google/fonts/raw/main/ofl/notoserif/NotoSerif%5Bwdth%2Cwght%5D.ttf
# (sha256 4d8e6761424656867019081a1a01336f3cb086982682698714054fc33f782713 when this was written).
# Needs uv (https://docs.astral.sh/uv/); fontTools runs in a throwaway environment.
set -eu
source=${1:?usage: sh subset.sh NotoSerif-VF.ttf}
# Latin with its diacritics (Latin-1, Extended-A and -B, Extended Additional, which holds Vietnamese),
# the combining marks they use, punctuation, and the currency and arrow signs articles use.
unicodes='U+0020-007E,U+00A0-00FF,U+0100-024F,U+0259,U+02BB-02BC,U+02C6-02C7,U+02D8-02DD,U+0300-0309,U+0323,U+1E00-1EFF,U+2000-206F,U+20AB-20AC,U+2122,U+2190-2193,U+2212,U+FFFD'
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
for weight in 400 600; do
	uvx --from 'fonttools[woff]==4.60.1' fonttools varLib.instancer "$source" "wght=$weight" wdth=100 -o "$work/$weight.ttf" -q
	uvx --from 'fonttools[woff]==4.60.1' pyftsubset "$work/$weight.ttf" --unicodes="$unicodes" \
		--layout-features='kern,liga,mark,mkmk,ccmp,locl' --flavor=woff2 --output-file="noto-serif-$weight.woff2"
done
