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
# The operator console on its own host, e.g. https://ops.example.com; empty serves it at APP_ORIGIN/operator/.
INSTALL_OPERATOR_ORIGIN=${OPERATOR_ORIGIN:-}

usage() {
  printf '%s\n' 'Usage: install.sh [--dir PATH] [--name NAME] [--email EMAIL] [--operator-origin URL]'
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
    --operator-origin) [ "$#" -ge 2 ] || fail '--operator-origin requires a value'; INSTALL_OPERATOR_ORIGIN=$2; shift 2 ;;
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

# The settings the server keeps in its database (collie config, the operator console) instead of the
# environment. An older .env may still set them: they are imported once, before the server starts.
# server/cmd/collie/config_test.go keeps this list equal to the server's.
MOVED_SETTINGS='AI_SERVER_PROVIDER AI_SERVER_API_KEY AI_SERVER_BASE_URL AI_SERVER_MODELS AI_SERVER_LABEL
AI_SERVER_PRICES AI_SERVER_MONTHLY_ALLOWANCE SMTP_HOST SMTP_PORT SMTP_USERNAME SMTP_PASSWORD
SMTP_STARTTLS EMAIL_FROM_POLICY MAIL_FROM_DOMAIN MAIL_FROM_INBOUND_SECRET
EMAIL_TRUSTED_AUTHSERV_ID HOSTED_MAIL_DOMAIN RESEND_WEBHOOK_SECRET RESEND_API_KEY
GOOGLE_OAUTH_CLIENT_ID GOOGLE_OAUTH_CLIENT_SECRET MICROSOFT_OAUTH_CLIENT_ID
MICROSOFT_OAUTH_CLIENT_SECRET MICROSOFT_OAUTH_TENANT GITHUB_APP_ID GITHUB_APP_SLUG
GITHUB_APP_CLIENT_ID GITHUB_APP_PRIVATE_KEY GITHUB_APP_CLIENT_SECRET GITHUB_APP_WEBHOOK_SECRET
POLAR_ACCESS_TOKEN POLAR_WEBHOOK_SECRET POLAR_API_URL BILLING_ALERT_EMAIL RETENTION_DELETE
RETENTION_MAX_DELETIONS_PER_DAY INTERNAL_SIGNUP_EMAILS INTERNAL_SIGNUP_NETWORKS TRUSTED_PROXIES
CLOUD_AUTH_ISSUER CLOUD_AUTH_JWKS_URL CLOUD_AUTH_AUTHORIZED_PARTIES CLOUD_AUTH_EMAIL_CLAIM
CLERK_PUBLISHABLE_KEY HELP_CENTER_DOMAIN AI_ALLOW_PRIVATE_BASE_URLS MAIL_IMAGE_CACHE_BYTES
KB_VIDEO_MAX_BYTES PROBLEM_REPORT_WORKSPACE POLAR_PRODUCT_TEAM_MONTHLY POLAR_PRODUCT_TEAM_YEARLY
POLAR_PRODUCT_FOUNDING POLAR_PRODUCT_SOLO_MONTHLY POLAR_PRODUCT_SOLO_YEARLY
POLAR_PRODUCT_SMALL_MONTHLY POLAR_PRODUCT_SMALL_YEARLY POLAR_PRODUCT_TEAM25_MONTHLY
POLAR_PRODUCT_TEAM25_YEARLY SIGNUP_HASH_KEY'

mkdir -p "$TARGET_DIR"

if [ ! -f "$TARGET_DIR/.env" ]; then
  # The superuser only creates the database. Colliedesk connects as the non-superuser owner
  # (DATABASE_URL) and serves requests as a separate login that can only take the app role
  # (DATABASE_REQUEST_URL), so SQL that leaves the row-level security role gains nothing.
  POSTGRES_PASSWORD=$("$OPENSSL" rand -hex 32)
  OWNER_PASSWORD=$("$OPENSSL" rand -hex 32)
  REQUEST_PASSWORD=$("$OPENSSL" rand -hex 32)
  # Seals Gmail grants and the Freshdesk API key of an in-app import. Written once: a replaced ring
  # cannot open what the old one sealed.
  TOKEN_KEY=$("$OPENSSL" rand -base64 32)
  [ -n "$POSTGRES_PASSWORD" ] && [ -n "$OWNER_PASSWORD" ] && [ -n "$REQUEST_PASSWORD" ] && [ -n "$TOKEN_KEY" ] ||
    fail 'Secret generation failed.'
  (
    umask 077
    {
      printf 'POSTGRES_USER=postgres\n'
      printf 'POSTGRES_PASSWORD=%s\n' "$POSTGRES_PASSWORD"
      printf 'COLLIEDESK_OWNER_PASSWORD=%s\n' "$OWNER_PASSWORD"
      printf 'DATABASE_URL=postgres://colliedesk:%s@postgres:5432/colliedesk?sslmode=disable\n' "$OWNER_PASSWORD"
      printf 'DATABASE_REQUEST_URL=postgres://colliedesk_request:%s@postgres:5432/colliedesk?sslmode=disable\n' "$REQUEST_PASSWORD"
      printf 'MAILBOX_TOKEN_KEYS=k1:%s\n' "$TOKEN_KEY"
      printf 'COLLIE_DESK_IMAGE=%s\n' "${COLLIE_DESK_IMAGE:-ghcr.io/colliedesk/colliedesk:latest}"
      printf 'COLLIE_DESK_PORT=%s\n' "${COLLIE_DESK_PORT:-8080}"
      printf 'APP_ORIGIN=%s\n' "${APP_ORIGIN:-http://localhost:${COLLIE_DESK_PORT:-8080}}"
      [ -z "$INSTALL_OPERATOR_ORIGIN" ] || printf 'OPERATOR_ORIGIN=%s\n' "$INSTALL_OPERATOR_ORIGIN"
    } >"$TARGET_DIR/.env"
  )
fi

# Settings live in the server's database (collie config). Those an older install kept in .env move
# there before the server starts; a group the database already holds is kept (docs/self-host/upgrade.md).
# trusted_proxies gets what the older compose file passed when .env did not set it: the reverse proxy on
# this host (loopback and the Docker bridge range). The server alone trusts no proxy.
SETTINGS=
for name in $MOVED_SETTINGS; do
  line=$(grep -E "^$name=.+" "$TARGET_DIR/.env" | tail -n 1 || :)
  [ -z "$line" ] || SETTINGS="$SETTINGS$line
"
done
if ! grep -Eq '^TRUSTED_PROXIES=' "$TARGET_DIR/.env"; then
  SETTINGS="${SETTINGS}TRUSTED_PROXIES=127.0.0.0/8,::1/128,172.16.0.0/12
"
fi
# An older image cannot store settings and would run without the ones the new compose file no longer
# passes: stop before changing anything.
IMAGE=${COLLIE_DESK_IMAGE:-$(sed -n 's/^COLLIE_DESK_IMAGE=//p' "$TARGET_DIR/.env" | tail -n 1)}
IMAGE=${IMAGE:-ghcr.io/colliedesk/colliedesk:latest}
if ! "$DOCKER" run --rm --entrypoint /app/collie "$IMAGE" help 2>/dev/null | grep -q '|config|'; then
  fail "The image $IMAGE is older than this installer. Pull a newer one (docker compose pull in $TARGET_DIR, or set COLLIE_DESK_IMAGE to a newer release), then run the installer again."
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

cat >"$TARGET_DIR/compose.yaml.next" <<'COMPOSE'
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
      OPERATOR_ORIGIN: ${OPERATOR_ORIGIN:-}
      LISTEN_ADDR: :8080
      MAILBOX_TOKEN_KEYS: ${MAILBOX_TOKEN_KEYS:-}
      # Secrets stay readable from .env until a key ring seals them in the database (docs/self-host/environment-variables.md#no-key-ring).
      SMTP_PASSWORD: ${SMTP_PASSWORD:-}
      MAIL_FROM_INBOUND_SECRET: ${MAIL_FROM_INBOUND_SECRET:-}
      RESEND_API_KEY: ${RESEND_API_KEY:-}
      RESEND_WEBHOOK_SECRET: ${RESEND_WEBHOOK_SECRET:-}
      AI_SERVER_API_KEY: ${AI_SERVER_API_KEY:-}
      GOOGLE_OAUTH_CLIENT_SECRET: ${GOOGLE_OAUTH_CLIENT_SECRET:-}
      MICROSOFT_OAUTH_CLIENT_SECRET: ${MICROSOFT_OAUTH_CLIENT_SECRET:-}
      GITHUB_APP_PRIVATE_KEY: ${GITHUB_APP_PRIVATE_KEY:-}
      GITHUB_APP_CLIENT_SECRET: ${GITHUB_APP_CLIENT_SECRET:-}
      GITHUB_APP_WEBHOOK_SECRET: ${GITHUB_APP_WEBHOOK_SECRET:-}
      POLAR_ACCESS_TOKEN: ${POLAR_ACCESS_TOKEN:-}
      POLAR_WEBHOOK_SECRET: ${POLAR_WEBHOOK_SECRET:-}
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
    command: ["/app/collie migrate && exec /app/collie serve"]
    healthcheck:
      test: ["CMD", "/app/collie", "healthcheck"]
      interval: 2s
      timeout: 4s
      retries: 30

volumes:
  postgres-data:
  attachments-data:
COMPOSE
# The compose file this run replaces stays next to it, so the older image can run again with it.
REPLACED=
if [ -f "$TARGET_DIR/compose.yaml" ] && ! cmp -s "$TARGET_DIR/compose.yaml" "$TARGET_DIR/compose.yaml.next"; then
  cp "$TARGET_DIR/compose.yaml" "$TARGET_DIR/compose.yaml.bak"
  REPLACED=1
fi
mv "$TARGET_DIR/compose.yaml.next" "$TARGET_DIR/compose.yaml"

cd "$TARGET_DIR"
if [ -n "$SETTINGS" ]; then
  "$DOCKER" compose up -d --wait postgres || fail 'PostgreSQL did not start. Run docker compose ps for details.'
  if ! printf '%s' "$SETTINGS" | "$DOCKER" compose run --rm -T server '/app/collie migrate && /app/collie config import --stdin --only-missing --by installer'; then
    # The settings are still only in .env: put back the compose file that passes them to the server.
    [ -z "$REPLACED" ] || mv compose.yaml.bak compose.yaml
    fail 'Importing the settings from .env failed; the compose file was left as it was. Fix the lines it names in .env, then run the installer again.'
  fi
fi
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

PAIRING_URL=$("$DOCKER" compose exec -T server /app/collie create-workspace \
  --name "$WORKSPACE_NAME" --admin-email "$ADMIN_EMAIL" </dev/null)
[ -n "$PAIRING_URL" ] || fail 'Workspace creation did not return a pairing URL.'
# The same person runs the server: the operator console's link is printed once, beside the workspace's.
OPERATOR_URL=$("$DOCKER" compose exec -T server /app/collie operator pair --email "$ADMIN_EMAIL" --by installer </dev/null) ||
  fail 'Making the operator console link failed. Print one later with: docker compose exec server /app/collie operator pair --email <your email>'
touch .initialized
printf 'Colliedesk is ready. Open this one-time pairing URL:\n%s\n' "$PAIRING_URL"
printf 'Operator console (one-time link): %s\n' "$OPERATOR_URL"
}

main "$@"
