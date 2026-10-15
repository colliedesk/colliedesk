#!/bin/sh
# Decision tests for tests/pinned-actions-fetchable-test.sh with a stand-in curl: a pin GitHub
# says does not exist fails the check; throttling or a network error is retried and, if it never
# clears, reported without failing (it says nothing about the pin).
set -eu

HERE=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/repo/.github/workflows" "$work/bin"
cat >"$work/repo/.github/workflows/ci.yml" <<'EOF'
jobs:
  a:
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7
EOF

# The stand-in prints the next status code from $CODES (space-separated) on each call.
cat >"$work/bin/curl" <<'EOF'
#!/bin/sh
count_file=$STATE/calls
n=$(cat "$count_file" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" >"$count_file"
set -- $CODES
i=1
code=${1:-000}
for c in "$@"; do
  if [ "$i" -eq "$n" ]; then code=$c; fi
  last=$c
  i=$((i + 1))
done
[ "$n" -ge "$i" ] && code=$last
printf '%s' "$code"
EOF
chmod +x "$work/bin/curl"

fail=0
run_case() {
  name=$1 codes=$2 want_exit=$3 want_calls=$4 want_text=$5
  rm -f "$work/calls"
  set +e
  out=$(STATE=$work CODES=$codes CURL="$work/bin/curl" SLEEP=true sh "$HERE/pinned-actions-fetchable-test.sh" "$work/repo" 2>&1)
  got=$?
  set -e
  calls=$(cat "$work/calls" 2>/dev/null || echo 0)
  if [ "$got" -ne "$want_exit" ] || [ "$calls" -ne "$want_calls" ] || ! printf '%s' "$out" | grep -q "$want_text"; then
    echo "FAIL $name: exit=$got (want $want_exit) calls=$calls (want $want_calls) output: $out" >&2
    fail=1
  else
    echo "ok   $name"
  fi
}

run_case "fetchable pin passes"                    "200"             0 1 "every pinned action archive is fetchable"
run_case "missing pin fails at once"               "404"             1 1 "HTTP 404"
run_case "throttled then served passes"            "429 429 200"     0 3 "every pinned action archive is fetchable"
run_case "network error then served passes"        "000 200"         0 2 "every pinned action archive is fetchable"
run_case "always throttled is reported, not failed" "429"            0 5 "could not be checked"
run_case "throttled then missing fails"            "429 404"         1 2 "HTTP 404"

exit "$fail"
