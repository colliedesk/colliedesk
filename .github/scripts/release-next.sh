#!/bin/sh
# Decisions for the manual Release button (release.yml, workflow_dispatch).
#   cloud-commit <version>   version from GET /api/v1/config -> the git ref Cloud runs:
#                            edge-<hex> (a main build) -> <hex>; a release tag -> the tag, which the
#                            workflow then refuses as already released, with a clear message.
#   last-tag [tag...]        the highest release tag given (by version); nothing when none.
#   next-tag <part> [tag...] the release after the highest one given, for part major, minor or
#                            patch (Semantic Versioning). After the alpha releases (v0.0.N), or with
#                            none, the next release is v1.0.0 whatever the part.
# A release tag is vMAJOR.MINOR.PATCH with MAJOR >= 1, or one of the alpha releases v0.0.N.
set -eu

RELEASE='^v(0\.0\.(0|[1-9][0-9]*)|[1-9][0-9]*\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$'

last_tag() {
  printf '%s\n' "$@" | grep -E "$RELEASE" | sort -V | tail -n 1
}

case ${1-} in
  cloud-commit)
    version=${2-}
    # grep matches line by line, so refuse anything but one line of [a-z0-9.-] first.
    case $version in
      '' | *[!a-z0-9.-]*)
        echo "Cloud reports a version that is not a main build (edge-<commit>) or a release." >&2
        exit 1
        ;;
    esac
    if printf '%s' "$version" | grep -Eq '^edge-[0-9a-f]{7,40}$'; then
      printf '%s\n' "${version#edge-}"
    elif printf '%s' "$version" | grep -Eq "$RELEASE"; then
      printf '%s\n' "$version"
    else
      echo "Cloud reports version '$version', which is not a main build (edge-<commit>) or a release." >&2
      exit 1
    fi
    ;;
  last-tag)
    shift
    last_tag "$@"
    ;;
  next-tag)
    part=${2-}
    case $part in
      major | minor | patch) ;;
      *)
        echo "next-tag needs the part to raise: major, minor or patch (got '$part')." >&2
        exit 2
        ;;
    esac
    shift 2
    last=$(last_tag "$@")
    case $last in
      '' | v0.0.*) echo v1.0.0; exit 0 ;;
    esac
    IFS=. read -r major minor patch <<EOF
${last#v}
EOF
    case $part in
      major) printf 'v%s.0.0\n' "$((major + 1))" ;;
      minor) printf 'v%s.%s.0\n' "$major" "$((minor + 1))" ;;
      patch) printf 'v%s.%s.%s\n' "$major" "$minor" "$((patch + 1))" ;;
    esac
    ;;
  *)
    echo "usage: release-next.sh cloud-commit <version> | last-tag [tag...] | next-tag major|minor|patch [tag...]" >&2
    exit 2
    ;;
esac
