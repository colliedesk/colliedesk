#!/bin/sh
set -eu

DOCKER=${DOCKER:-docker}
OPENSSL=${OPENSSL:-openssl}
SLEEP=${SLEEP:-sleep}
TARGET_DIR=./colliedesk
WORKSPACE_NAME=
ADMIN_EMAIL=

usage() {
  printf '%s\n' 'Usage: install.sh [--dir PATH] [--name NAME] [--email EMAIL]'
}

fail() {
  printf 'colliedesk installer: %s\n' "$1" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case $1 in
    --dir) [ "$#" -ge 2 ] || fail '--dir requires a value'; TARGET_DIR=$2; shift 2 ;;
    --name) [ "$#" -ge 2 ] || fail '--name requires a value'; WORKSPACE_NAME=$2; shift 2 ;;
    --email) [ "$#" -ge 2 ] || fail '--email requires a value'; ADMIN_EMAIL=$2; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done

command -v "$DOCKER" >/dev/null 2>&1 || fail 'Docker is required. Install Docker before running this script.'
"$DOCKER" compose version >/dev/null 2>&1 || fail 'Docker Compose v2 is required. Install the docker compose plugin.'
command -v "$OPENSSL" >/dev/null 2>&1 || fail 'OpenSSL is required to generate installation secrets.'

if [ -z "$WORKSPACE_NAME" ] || [ -z "$ADMIN_EMAIL" ]; then
  [ -t 0 ] || fail 'Non-interactive install requires --name and --email.'
  if [ -z "$WORKSPACE_NAME" ]; then
    printf 'Workspace name: ' >&2
    IFS= read -r WORKSPACE_NAME
  fi
  if [ -z "$ADMIN_EMAIL" ]; then
    printf 'Admin email: ' >&2
    IFS= read -r ADMIN_EMAIL
  fi
fi
[ -n "$WORKSPACE_NAME" ] || fail 'Workspace name cannot be empty.'
[ -n "$ADMIN_EMAIL" ] || fail 'Admin email cannot be empty.'

mkdir -p "$TARGET_DIR"

if [ ! -f "$TARGET_DIR/.env" ]; then
  POSTGRES_PASSWORD=$("$OPENSSL" rand -hex 32)
  APP_SECRET=$("$OPENSSL" rand -hex 32)
  [ -n "$POSTGRES_PASSWORD" ] && [ -n "$APP_SECRET" ] || fail 'Secret generation failed.'
  (
    umask 077
    {
      printf 'POSTGRES_PASSWORD=%s\n' "$POSTGRES_PASSWORD"
      printf 'APP_SECRET=%s\n' "$APP_SECRET"
      printf 'COLLIE_DESK_IMAGE=%s\n' "${COLLIE_DESK_IMAGE:-ghcr.io/colliedesk/colliedesk:edge}"
      printf 'COLLIE_DESK_PORT=%s\n' "${COLLIE_DESK_PORT:-8080}"
      printf 'APP_ORIGIN=%s\n' "${APP_ORIGIN:-http://localhost:${COLLIE_DESK_PORT:-8080}}"
    } >"$TARGET_DIR/.env"
  )
fi

cat >"$TARGET_DIR/compose.yaml" <<'COMPOSE'
name: colliedesk

services:
  postgres:
    image: postgres:17-bookworm
    environment:
      POSTGRES_DB: colliedesk
      POSTGRES_USER: colliedesk
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U colliedesk -d colliedesk"]
      interval: 3s
      timeout: 3s
      retries: 20
  server:
    image: ${COLLIE_DESK_IMAGE}
    environment:
      DATABASE_URL: postgres://colliedesk:${POSTGRES_PASSWORD}@postgres:5432/colliedesk?sslmode=disable
      APP_ORIGIN: ${APP_ORIGIN}
      APP_SECRET: ${APP_SECRET}
      LISTEN_ADDR: :8080
      ATTACHMENTS_DIR: /data/attachments
    volumes:
      - attachments-data:/data/attachments
    ports:
      - "127.0.0.1:${COLLIE_DESK_PORT}:8080"
    depends_on:
      postgres:
        condition: service_healthy
    entrypoint: ["/bin/sh", "-c"]
    command: ["/app/colliedesk migrate && exec /app/colliedesk serve"]
    healthcheck:
      test: ["CMD", "/app/colliedesk", "healthcheck"]
      interval: 2s
      timeout: 4s
      retries: 30

volumes:
  postgres-data:
  attachments-data:
COMPOSE

cd "$TARGET_DIR"
"$DOCKER" compose up -d

attempt=0
while [ "$attempt" -lt 60 ]; do
  SERVER_ID=$("$DOCKER" compose ps --quiet server 2>/dev/null || :)
  HEALTH_STATUS=
  if [ -n "$SERVER_ID" ]; then
    HEALTH_STATUS=$("$DOCKER" inspect --format '{{.State.Health.Status}}' "$SERVER_ID" 2>/dev/null || :)
  fi
  if [ "$HEALTH_STATUS" = healthy ]; then
    break
  fi
  attempt=$((attempt + 1))
  "$SLEEP" 2
done
[ "$attempt" -lt 60 ] || fail 'Server did not become healthy within 120 seconds. Run docker compose ps for details.'

if [ -f .initialized ]; then
  printf '%s\n' 'CollieDesk is running. Existing workspace initialization was kept.'
  exit 0
fi

PAIRING_URL=$("$DOCKER" compose exec -T server /app/colliedesk create-workspace \
  --name "$WORKSPACE_NAME" --admin-email "$ADMIN_EMAIL")
[ -n "$PAIRING_URL" ] || fail 'Workspace creation did not return a pairing URL.'
touch .initialized
printf 'CollieDesk is ready. Open this one-time pairing URL:\n%s\n' "$PAIRING_URL"
