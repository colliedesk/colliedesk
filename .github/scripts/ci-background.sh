#!/bin/sh
# Runs a long CI check in the background so the job's quick checks go on meanwhile; a later step
# waits for it, prints its output and fails with it.
# Usage: ci-background.sh start NAME COMMAND [ARG]... ; ci-background.sh wait NAME
set -eu

STATE=${CI_BACKGROUND_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/ci-background}
TIMEOUT=${CI_BACKGROUND_TIMEOUT:-110}
INTERVAL=${CI_BACKGROUND_INTERVAL:-1}

usage() { echo "usage: ci-background.sh start NAME COMMAND... | wait NAME" >&2; exit 2; }
[ $# -ge 2 ] || usage
command=$1 name=$2
shift 2

case "$command" in
  start)
    [ $# -ge 1 ] || usage
    mkdir -p "$STATE"
    rm -f "$STATE/$name.status"
    : >"$STATE/$name.log"
    # Detached from the step's output streams, which the runner would otherwise wait on.
    ( if "$@" >"$STATE/$name.log" 2>&1; then echo 0; else echo $?; fi >"$STATE/$name.status" ) \
      </dev/null >/dev/null 2>&1 &
    echo "ci-background: started $name"
    ;;
  wait)
    [ -f "$STATE/$name.log" ] || { echo "ci-background: $name was never started" >&2; exit 1; }
    waited=0
    until [ -s "$STATE/$name.status" ]; do
      if [ "$(awk -v w="$waited" -v t="$TIMEOUT" 'BEGIN { print (w >= t) }')" = 1 ]; then
        cat "$STATE/$name.log"
        echo "ci-background: $name did not finish within ${TIMEOUT}s" >&2
        exit 1
      fi
      sleep "$INTERVAL"
      waited=$(awk -v w="$waited" -v i="$INTERVAL" 'BEGIN { print w + i }')
    done
    cat "$STATE/$name.log"
    status=$(cat "$STATE/$name.status")
    [ "$status" = 0 ] || { echo "ci-background: $name exited $status" >&2; exit 1; }
    ;;
  *) usage ;;
esac
