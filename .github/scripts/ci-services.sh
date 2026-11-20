#!/bin/sh
# Starts the test services of a CI job in the background and waits for them later, so pulling and
# booting the containers overlaps the toolchain setup instead of delaying the job's first step
# (GitHub's services: block runs before any step).
# Usage: ci-services.sh start postgres [mailpit] ... ; ci-services.sh wait postgres [mailpit]
set -eu

DOCKER=${DOCKER:-docker}
TIMEOUT=${CI_SERVICES_TIMEOUT:-60}
INTERVAL=${CI_SERVICES_INTERVAL:-1}
# docker run's output and exit status per service, read by wait.
STATE=${CI_SERVICES_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/ci-services}

# Postgres is pinned to the minor version Colliedesk Cloud runs, so the latency gates measure what
# production does: 17.12 made AI retrieval about six times slower than 17.11 on the same rows.
# PGDATA stays on the tmpfs (the image's default moved to /var/lib/postgresql/19/docker). PostgreSQL 19
# changed how the lock table is sized: its release notes double a setting to keep the same capacity
# (on 16 and 17 the same value only doubles the table). CI_POSTGRES_IMAGE picks another image, by
# digest, for the job that runs the migrations on the oldest supported majors.
POSTGRES_IMAGE=${CI_POSTGRES_IMAGE:-postgres:19.0-bookworm@sha256:f4859caf96de494e38efa69ed703b08a5bbd9605e152e604f2daec8841c87c41}

run_service() {
  case "$1" in
    postgres)
      "$DOCKER" run -d --name ci-postgres -p 5432:5432 \
        --tmpfs /var/lib/postgresql/data:rw,size=2g -e PGDATA=/var/lib/postgresql/data \
        -e POSTGRES_DB=colliedesk_test -e POSTGRES_USER=colliedesk -e POSTGRES_PASSWORD=colliedesk \
        "$POSTGRES_IMAGE" \
        postgres -c max_locks_per_transaction=512 ;;
    mailpit) "$DOCKER" run -d --name ci-mailpit -p 1025:1025 -p 8025:8025 axllent/mailpit:v1.27 ;;
  esac
}

# docker run pulls the image before it returns, so it runs in the background: the job's setup steps
# go on while the image downloads.
start() {
  case "$1" in
    postgres | mailpit) ;;
    *) echo "ci-services: unknown service $1" >&2; return 1 ;;
  esac
  image=
  if [ "$1" = postgres ]; then
    case "$POSTGRES_IMAGE" in
      *@sha256:*) image=" ($POSTGRES_IMAGE)" ;;
      *) echo "ci-services: pin CI_POSTGRES_IMAGE by digest (name@sha256:...): $POSTGRES_IMAGE" >&2; return 1 ;;
    esac
  fi
  mkdir -p "$STATE"
  rm -f "$STATE/$1.status"
  # Detached from the step's output streams, which the runner would otherwise wait on.
  ( if run_service "$1" >"$STATE/$1.log" 2>&1; then echo 0; else echo 1; fi >"$STATE/$1.status" ) </dev/null >/dev/null 2>&1 &
  echo "ci-services: starting $1$image"
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
    if [ "$(cat "$STATE/$1.status" 2>/dev/null)" = 1 ]; then
      echo "ci-services: $1 could not start:" >&2
      cat "$STATE/$1.log" >&2
      return 1
    fi
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
