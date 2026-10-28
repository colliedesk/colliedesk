#!/bin/sh
# Builds the files a release attaches besides the image (client-assets.txt): the client collie for
# each system, the Claude Desktop extension and SHA256SUMS. release.yml runs it; it also runs on a
# Mac to try a build before a release: sh .github/scripts/client-release.sh v0.0.0 /tmp/client
set -eu
tag=$1
out=$2
case "$tag" in v*) ;; *) echo "client-release: the tag must start with v, got $tag" >&2; exit 2 ;; esac
root=$(cd "$(dirname "$0")/../.." && pwd)
assets="$root/.github/scripts/client-assets.txt"
mkdir -p "$out"
out=$(cd "$out" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export COPYFILE_DISABLE=1 # no ._ files in archives made on a Mac
export CGO_ENABLED=0

build() { # GOOS GOARCH DIR
  mkdir -p "$3"
  exe=collie
  [ "$1" = windows ] && exe=collie.exe
  (cd "$root/server" && GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "-s -w -X main.version=$tag" -o "$3/$exe" ./cmd/collie-client)
  cp "$root/LICENSE" "$3/LICENSE"
}

grep -v '^#' "$assets" | while read -r goos goarch asset; do
  case "$goos" in bundle | checksums | '') continue ;; esac
  dir="$work/$goos-$goarch"
  build "$goos" "$goarch" "$dir"
  case "$asset" in
    *.zip) (cd "$dir" && zip -q -X "$out/$asset" collie.exe LICENSE) ;;
    *.tar.gz) tar -czf "$out/$asset" -C "$dir" collie LICENSE ;;
    *) echo "client-release: unknown archive $asset" >&2; exit 1 ;;
  esac
done

# Claude Desktop picks a command per platform, not per CPU: one universal macOS binary, and the
# x64 Windows one (Windows on Arm runs it too).
bundle=$(awk '$1 == "bundle" { print $3 }' "$assets")
mcpb="$work/mcpb"
mkdir -p "$mcpb/server"
(cd "$root/server" && go run ./internal/release/universal -o "$mcpb/server/collie" "$work/darwin-arm64/collie" "$work/darwin-amd64/collie")
cp "$work/windows-amd64/collie.exe" "$mcpb/server/collie.exe"
cp "$root/LICENSE" "$mcpb/LICENSE"
jq --arg version "${tag#v}" '.version = $version' "$root/server/cmd/collie-client/mcpb-manifest.json" > "$mcpb/manifest.json"
(cd "$mcpb" && zip -q -X -r "$out/$bundle" manifest.json LICENSE server)

checksums=$(awk '$1 == "checksums" { print $3 }' "$assets")
(
  cd "$out"
  rm -f "$checksums"
  files=$(grep -v '^#' "$assets" | awk '$1 != "checksums" { print $3 }')
  # shellcheck disable=SC2086 # one word per asset name
  if command -v sha256sum >/dev/null; then sha256sum $files; else shasum -a 256 $files; fi > "$checksums"
)
echo "client-release: $(grep -vc '^#' "$assets") files in $out"
