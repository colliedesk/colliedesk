#!/bin/sh
# Boots a built server image the way install.sh runs it (Postgres, migrate, serve) and fails
# unless the container reports healthy and stamps the expected version. CI runs this before
# any image tag a self-hoster can pull is moved.
# Usage: tests/image-boot-test.sh IMAGE EXPECTED_VERSION
set -eu

IMAGE=${1:?usage: image-boot-test.sh IMAGE EXPECTED_VERSION}
EXPECTED_VERSION=${2:?usage: image-boot-test.sh IMAGE EXPECTED_VERSION}
DOCKER=${DOCKER:-docker}
NAME=colliedesk-boot-$$
PASSWORD=boot-test-password

cleanup() {
  status=$?
  if [ "$status" != 0 ]; then
    echo "--- server logs" >&2
    "$DOCKER" logs "$NAME-server" >&2 2>&1 || :
  fi
  "$DOCKER" rm -f "$NAME-server" "$NAME-postgres" >/dev/null 2>&1 || :
  "$DOCKER" network rm "$NAME" >/dev/null 2>&1 || :
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

actual_version=$("$DOCKER" run --rm "$IMAGE" version)
if [ "$actual_version" != "$EXPECTED_VERSION" ]; then
  echo "image reports version '$actual_version', want '$EXPECTED_VERSION'" >&2
  exit 1
fi

"$DOCKER" network create "$NAME" >/dev/null
"$DOCKER" run -d --name "$NAME-postgres" --network "$NAME" --network-alias postgres \
  -e POSTGRES_DB=colliedesk -e POSTGRES_USER=colliedesk -e POSTGRES_PASSWORD="$PASSWORD" \
  postgres:17-bookworm >/dev/null

# Over TCP: during init the entrypoint runs a socket-only server that is about to restart.
attempt=0
until "$DOCKER" exec "$NAME-postgres" pg_isready -h 127.0.0.1 -U colliedesk -d colliedesk >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    echo "postgres did not become ready" >&2
    exit 1
  fi
  sleep 1
done

# The request login exercises the row-level security path that serve verifies at startup.
"$DOCKER" run -d --name "$NAME-server" --network "$NAME" \
  -e DATABASE_URL="postgres://colliedesk:$PASSWORD@postgres:5432/colliedesk?sslmode=disable" \
  -e DATABASE_REQUEST_URL="postgres://colliedesk_request:$PASSWORD@postgres:5432/colliedesk?sslmode=disable" \
  -e APP_ORIGIN=http://localhost:8080 -e DISABLE_TELEMETRY=true \
  --entrypoint /bin/sh "$IMAGE" -c '/app/collie migrate && exec /app/collie serve' >/dev/null

attempt=0
until "$DOCKER" exec "$NAME-server" /app/collie healthcheck >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$("$DOCKER" inspect -f '{{.State.Running}}' "$NAME-server" 2>/dev/null)" != true ] || [ "$attempt" -ge 60 ]; then
    echo "server did not become healthy" >&2
    exit 1
  fi
  sleep 1
done
# Installs made before the binary was renamed still call /app/colliedesk.
if ! "$DOCKER" exec "$NAME-server" /app/colliedesk healthcheck >/dev/null 2>&1; then
  echo "/app/colliedesk no longer runs; installs made before the rename would break" >&2
  exit 1
fi
echo "image $IMAGE booted healthy at version $actual_version"
