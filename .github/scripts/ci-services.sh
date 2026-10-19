#!/bin/sh
# Starts the test services of a CI job in the background and waits for them later, so pulling and
# booting the containers overlaps the toolchain setup instead of delaying the job's first step
# (GitHub's services: block runs before any step).
# Usage: ci-services.sh start postgres [mailpit] ... ; ci-services.sh wait postgres [mailpit]
set -eu

DOCKER=${DOCKER:-docker}
TIMEOUT=${CI_SERVICES_TIMEOUT:-60}
INTERVAL=${CI_SERVICES_INTERVAL:-1}

start() {
  case "$1" in
    postgres)
      "$DOCKER" run -d --name ci-postgres -p 5432:5432 \
        --tmpfs /var/lib/postgresql/data:rw,size=2g \
        -e POSTGRES_DB=colliedesk_test -e POSTGRES_USER=colliedesk -e POSTGRES_PASSWORD=colliedesk \
        postgres:17-bookworm >/dev/null ;;
    mailpit)
      "$DOCKER" run -d --name ci-mailpit -p 1025:1025 -p 8025:8025 axllent/mailpit:v1.27 >/dev/null ;;
    *) echo "ci-services: unknown service $1" >&2; return 1 ;;
  esac
}

ready() {
  case "$1" in
    # -h 127.0.0.1: the TCP listener, which comes up after the image's init socket-only phase.
    postgres) "$DOCKER" exec ci-postgres pg_isready -q -h 127.0.0.1 -U colliedesk -d colliedesk_test ;;
    mailpit) "$DOCKER" exec ci-mailpit /mailpit readyz >/dev/null 2>&1 ;;
    *) echo "ci-services: unknown service $1" >&2; return 1 ;;
  esac
}

await() {
  waited=0
  until ready "$1"; do
    if [ "$(awk -v w="$waited" -v t="$TIMEOUT" 'BEGIN { print (w >= t) }')" = 1 ]; then
      echo "ci-services: $1 did not become ready within ${TIMEOUT}s" >&2
      "$DOCKER" logs "ci-$1" >&2 2>&1 || true
      return 1
    fi
    sleep "$INTERVAL"
    waited=$(awk -v w="$waited" -v i="$INTERVAL" 'BEGIN { print w + i }')
  done
  echo "ci-services: $1 ready"
}

command=${1:-}
[ $# -ge 2 ] || { echo "usage: ci-services.sh start|wait SERVICE..." >&2; exit 2; }
shift
for service in "$@"; do
  case "$command" in
    start) start "$service" ;;
    wait) await "$service" ;;
    *) echo "usage: ci-services.sh start|wait SERVICE..." >&2; exit 2 ;;
  esac
done
