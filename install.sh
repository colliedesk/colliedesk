#!/bin/sh
set -eu

# The whole installer lives in main() and runs only from the last line, so `curl | sh`
# parses the complete script before any command can read from the same stdin.
main() {

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
  # The superuser only creates the database. Colliedesk connects as the non-superuser owner
  # (DATABASE_URL) and serves requests as a separate login that can only take the app role
  # (DATABASE_REQUEST_URL), so SQL that leaves the row-level security role gains nothing.
  POSTGRES_PASSWORD=$("$OPENSSL" rand -hex 32)
  OWNER_PASSWORD=$("$OPENSSL" rand -hex 32)
  REQUEST_PASSWORD=$("$OPENSSL" rand -hex 32)
  APP_SECRET=$("$OPENSSL" rand -hex 32)
  [ -n "$POSTGRES_PASSWORD" ] && [ -n "$OWNER_PASSWORD" ] && [ -n "$REQUEST_PASSWORD" ] && [ -n "$APP_SECRET" ] ||
    fail 'Secret generation failed.'
  (
    umask 077
    {
      printf 'POSTGRES_USER=postgres\n'
      printf 'POSTGRES_PASSWORD=%s\n' "$POSTGRES_PASSWORD"
      printf 'COLLIEDESK_OWNER_PASSWORD=%s\n' "$OWNER_PASSWORD"
      printf 'DATABASE_URL=postgres://colliedesk:%s@postgres:5432/colliedesk?sslmode=disable\n' "$OWNER_PASSWORD"
      printf 'DATABASE_REQUEST_URL=postgres://colliedesk_request:%s@postgres:5432/colliedesk?sslmode=disable\n' "$REQUEST_PASSWORD"
      printf 'APP_SECRET=%s\n' "$APP_SECRET"
      printf 'COLLIE_DESK_IMAGE=%s\n' "${COLLIE_DESK_IMAGE:-ghcr.io/colliedesk/colliedesk:edge}"
      printf 'COLLIE_DESK_PORT=%s\n' "${COLLIE_DESK_PORT:-8080}"
      printf 'APP_ORIGIN=%s\n' "${APP_ORIGIN:-http://localhost:${COLLIE_DESK_PORT:-8080}}"
    } >"$TARGET_DIR/.env"
  )
fi

mkdir -p "$TARGET_DIR/initdb"
# Runs once, when PostgreSQL creates its volume. Installs from before the request login have no
# COLLIEDESK_OWNER_PASSWORD and keep their existing volume, so nothing changes for them.
cat >"$TARGET_DIR/initdb/10-colliedesk-owner.sh" <<'INITDB'
#!/bin/sh
set -e
if [ -n "${COLLIEDESK_OWNER_PASSWORD:-}" ]; then
  # psql reads the password from its environment, so it never appears in a process list.
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<'SQL'
\getenv owner_password COLLIEDESK_OWNER_PASSWORD
CREATE ROLE colliedesk LOGIN CREATEROLE PASSWORD :'owner_password';
ALTER DATABASE colliedesk OWNER TO colliedesk;
SQL
fi
INITDB
chmod 755 "$TARGET_DIR/initdb/10-colliedesk-owner.sh"

cat >"$TARGET_DIR/compose.yaml" <<'COMPOSE'
name: colliedesk

services:
  postgres:
    image: postgres:17-bookworm
    environment:
      POSTGRES_DB: colliedesk
      POSTGRES_USER: ${POSTGRES_USER:-colliedesk}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      COLLIEDESK_OWNER_PASSWORD: ${COLLIEDESK_OWNER_PASSWORD:-}
    volumes:
      - postgres-data:/var/lib/postgresql/data
      - ./initdb:/docker-entrypoint-initdb.d:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -h 127.0.0.1 -U colliedesk -d colliedesk"]
      interval: 3s
      timeout: 3s
      retries: 20
  server:
    image: ${COLLIE_DESK_IMAGE}
    environment:
      DATABASE_URL: ${DATABASE_URL:-postgres://colliedesk:${POSTGRES_PASSWORD}@postgres:5432/colliedesk?sslmode=disable}
      DATABASE_REQUEST_URL: ${DATABASE_REQUEST_URL:-}
      APP_ORIGIN: ${APP_ORIGIN}
      APP_SECRET: ${APP_SECRET}
      LISTEN_ADDR: :8080
      SMTP_HOST: ${SMTP_HOST:-}
      SMTP_PORT: ${SMTP_PORT:-587}
      SMTP_USERNAME: ${SMTP_USERNAME:-}
      SMTP_PASSWORD: ${SMTP_PASSWORD:-}
      SMTP_STARTTLS: ${SMTP_STARTTLS:-true}
      EMAIL_FROM_POLICY: ${EMAIL_FROM_POLICY:-verified}
      MAIL_FROM_DOMAIN: ${MAIL_FROM_DOMAIN:-}
      MAIL_FROM_INBOUND_SECRET: ${MAIL_FROM_INBOUND_SECRET:-}
      EMAIL_TRUSTED_AUTHSERV_ID: ${EMAIL_TRUSTED_AUTHSERV_ID:-}
      TRUSTED_PROXIES: ${TRUSTED_PROXIES-127.0.0.0/8,::1/128,172.16.0.0/12}
      ATTACHMENTS_DIR: /data/attachments
      DISABLE_TELEMETRY: ${DISABLE_TELEMETRY:-}
      DO_NOT_TRACK: ${DO_NOT_TRACK:-}
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
  printf '%s\n' 'Colliedesk is running. Existing workspace initialization was kept.'
  exit 0
fi

PAIRING_URL=$("$DOCKER" compose exec -T server /app/colliedesk create-workspace \
  --name "$WORKSPACE_NAME" --admin-email "$ADMIN_EMAIL" </dev/null)
[ -n "$PAIRING_URL" ] || fail 'Workspace creation did not return a pairing URL.'
touch .initialized
printf 'Colliedesk is ready. Open this one-time pairing URL:\n%s\n' "$PAIRING_URL"
}

main "$@"
