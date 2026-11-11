#!/bin/sh
# Lists a release in the official MCP Registry (registry.modelcontextprotocol.io) as
# io.github.colliedesk/colliedesk, from server.json with its version set to the tag. release.yml runs
# it with GitHub OIDC (id-token: write), which proves the io.github.colliedesk namespace; nobody logs
# in by hand. A run that finishes an earlier one finds the version listed and stops there.
# Try it without publishing: sh .github/scripts/mcp-registry-publish.sh v1.2.3 --dry-run
set -eu
tag=$1
mode=${2:-publish}
case "$tag" in v[0-9]*) ;; *) echo "mcp-registry: the tag must look like v1.2.3, got $tag" >&2; exit 2 ;; esac
version=${tag#v}
registry=https://registry.modelcontextprotocol.io
name=io.github.colliedesk/colliedesk
# mcp-publisher, pinned by version and by the SHA-256 the registry publishes in its checksums file.
publisher_version=v1.8.7
publisher_sha256=00b21004149a4ce66f0782508ae4a2bce67d1bea395a8da0bd8028159fb59579
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

listed=$(curl -s -o /dev/null -w '%{http_code}' "$registry/v0/servers/$(printf %s "$name" | sed 's,/,%2F,')/versions/$version")
if [ "$listed" = 200 ]; then
  echo "mcp-registry: $name $version is already listed"
  exit 0
fi

python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); d["version"]=sys.argv[2]; json.dump(d, open(sys.argv[3], "w"), indent=2, ensure_ascii=False)' \
  "$root/server.json" "$version" "$work/server.json"

archive=mcp-publisher_linux_amd64.tar.gz
curl -fsSL -o "$work/$archive" "https://github.com/modelcontextprotocol/registry/releases/download/$publisher_version/$archive"
echo "$publisher_sha256  $work/$archive" | sha256sum -c - >/dev/null
tar -xzf "$work/$archive" -C "$work" mcp-publisher

cd "$work"
./mcp-publisher validate server.json
if [ "$mode" = --dry-run ]; then
  echo "mcp-registry: $name $version is valid; not published (--dry-run)"
  exit 0
fi
./mcp-publisher login github-oidc
./mcp-publisher publish server.json
echo "mcp-registry: published $name $version"
