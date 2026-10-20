#!/bin/sh
# Upgrade test: an install of the previous release, filled with the fixture in tests/upgrade/fixture,
# is upgraded to a candidate image. It fails when the candidate's migrations lose or change a row, a
# column or a stored file, or link a row to another workspace, or when the upgraded server does not
# boot and serve each workspace its own inbox and nothing else. It also tests the two recovery paths
# of docs/user/upgrade.md: restoring one workspace (restore-workspace.sh, twice) and rolling the
# whole install back to the backup and the previous image. Needs Docker with Compose v2 and curl.
# See docs/internals/upgrades.md.
#   sh tests/upgrade-test.sh PREVIOUS_IMAGE CANDIDATE_IMAGE
#   sh tests/upgrade-test.sh "$(sh .github/scripts/previous-release.sh)" colliedesk:ci
# UPGRADE_TEST_RESTORE=after (default) tests the restores on the upgraded install: one workspace
#   from the older backup, then the rollback. =before tests them on the previous release, before
#   the upgrade, so they need no candidate image: CI runs them while the image builds.
# UPGRADE_TEST_WAIT=N waits up to N seconds for the candidate image to exist.
# UPGRADE_TEST_KEEP=1 leaves the install running for a look.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
PREVIOUS=${1:?usage: upgrade-test.sh PREVIOUS_IMAGE CANDIDATE_IMAGE}
CANDIDATE=${2:?usage: upgrade-test.sh PREVIOUS_IMAGE CANDIDATE_IMAGE}
DOCKER=${DOCKER:-docker}
WAIT=${UPGRADE_TEST_WAIT:-0}
RESTORE=${UPGRADE_TEST_RESTORE:-after}
case $RESTORE in before | after) ;; *) echo 'UPGRADE_TEST_RESTORE is before or after' >&2; exit 2 ;; esac
# Ports and names derive from the checkout path, so two checkouts on one host do not collide.
PORT=${COLLIEDESK_UPGRADE_PORT:-$((19700 + $(printf '%s' "$ROOT" | cksum | cut -d" " -f1) % 800))}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/colliedesk-upgrade.XXXXXX")
COMPOSE_PROJECT_NAME=colliedesk-upgrade-$$
FIXTURE=$ROOT/tests/upgrade
CHECK_DB=colliedesk
tab=$(printf '\t')
export COMPOSE_PROJECT_NAME

started=$(date +%s)
step() { printf 'upgrade test [%ss]: %s\n' "$(($(date +%s) - started))" "$1"; }

cleanup() {
  status=$?
  if [ "$status" != 0 ] && [ -f "$TMP/compose.yaml" ]; then
    (cd "$TMP" && "$DOCKER" compose logs --no-color --tail 80 server >&2 2>&1) || :
  fi
  if [ "${UPGRADE_TEST_KEEP:-}" = 1 ]; then
    echo "kept: cd $TMP && COMPOSE_PROJECT_NAME=$COMPOSE_PROJECT_NAME docker compose ps" >&2
  else
    if [ -f "$TMP/compose.yaml" ]; then
      (cd "$TMP" && "$DOCKER" compose down -v --remove-orphans >/dev/null 2>&1) || :
    fi
    rm -rf "$TMP"
  fi
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'upgrade test: %s\n' "$1" >&2
  exit 1
}

dc() { (cd "$TMP" && "$DOCKER" compose "$@"); }
# psql in the postgres container, reading SQL from stdin: psql_as USER DATABASE [psql options].
psql_as() {
  user=$1 db=$2
  shift 2
  dc exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U "$user" -d "$db" "$@"
}
# One shell command in the image of .env, with the install's settings and attachments volume.
server_run() { dc run --rm --no-deps -T server "$@"; }
# A migrate that prints exactly "migrations applied", as both releases do.
migrate() { server_run '/app/collie migrate' 2>/dev/null | grep -qx 'migrations applied'; }

# The install as install.sh lays it out: its compose file and database users, generated passwords.
sed -n "/^cat >\"\$TARGET_DIR\/compose.yaml\" <<'COMPOSE'$/,/^COMPOSE$/p" "$ROOT/install.sh" | sed '1d;$d' >"$TMP/compose.yaml"
mkdir -p "$TMP/initdb"
sed -n "/^cat >\"\$TARGET_DIR\/initdb\/10-colliedesk-owner.sh\" <<'INITDB'$/,/^INITDB$/p" "$ROOT/install.sh" | sed '1d;$d' >"$TMP/initdb/10-colliedesk-owner.sh"
chmod 755 "$TMP/initdb/10-colliedesk-owner.sh"
grep -q '^services:' "$TMP/compose.yaml" && grep -q 'CREATE ROLE colliedesk' "$TMP/initdb/10-colliedesk-owner.sh" ||
  fail 'could not read the compose file and database setup from install.sh'
secret() { od -An -N16 -tx1 /dev/urandom | tr -d ' \n'; }
owner_password=$(secret)
{
  echo 'POSTGRES_USER=postgres'
  echo "POSTGRES_PASSWORD=$(secret)"
  echo "COLLIEDESK_OWNER_PASSWORD=$owner_password"
  echo "DATABASE_URL=postgres://colliedesk:$owner_password@postgres:5432/colliedesk?sslmode=disable"
  echo "DATABASE_REQUEST_URL=postgres://colliedesk_request:$(secret)@postgres:5432/colliedesk?sslmode=disable"
  echo "APP_SECRET=$(secret)"
  echo "COLLIE_DESK_IMAGE=$PREVIOUS"
  echo "COLLIE_DESK_PORT=$PORT"
  echo "APP_ORIGIN=http://localhost:$PORT"
  echo 'DISABLE_TELEMETRY=true'
} >"$TMP/.env"
use_image() {
  sed "s#^COLLIE_DESK_IMAGE=.*#COLLIE_DESK_IMAGE=$1#" "$TMP/.env" >"$TMP/.env.next" && mv "$TMP/.env.next" "$TMP/.env"
}

"$DOCKER" image inspect "$PREVIOUS" >/dev/null 2>&1 || "$DOCKER" pull -q "$PREVIOUS" >/dev/null || fail "cannot pull $PREVIOUS"
step "previous release $PREVIOUS ($("$DOCKER" run --rm "$PREVIOUS" version))"
dc up -d --wait postgres >/dev/null 2>&1 || fail 'postgres did not start'

# Which migrations the previous release has, and the schema it makes on its own.
echo 'CREATE DATABASE reference OWNER colliedesk;' | psql_as postgres postgres
dc run --rm --no-deps -T -e DATABASE_URL="postgres://colliedesk:$owner_password@postgres:5432/reference?sslmode=disable" \
  -e DATABASE_REQUEST_URL= server '/app/collie migrate' >/dev/null 2>&1 || fail "$PREVIOUS could not migrate an empty database"
echo 'SELECT name FROM schema_migrations ORDER BY name;' | psql_as postgres reference -At >"$TMP/previous-migrations"
[ -s "$TMP/previous-migrations" ] || fail "$PREVIOUS applied no migrations"

# The fixture database: the previous release's migrations one by one, as the owner and with the
# setting `collie migrate` uses, each followed by its fixture file. Migration files never change once
# merged (migration-check), so these are the files the previous release ran; the schema comparison
# below proves it.
step "building the fixture on $(grep -c . "$TMP/previous-migrations") migrations"
echo 'CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());' | psql_as colliedesk colliedesk
blocks=0
while read -r name; do
  [ -f "$ROOT/server/migrations/$name" ] || fail "migration $name of $PREVIOUS is not in this checkout"
  { echo "SELECT set_config('app.privileged', 'on', true) \\g /dev/null"
    cat "$ROOT/server/migrations/$name"
    echo
    echo "INSERT INTO schema_migrations(name) VALUES ('$name');"
  } | psql_as colliedesk colliedesk -1 >/dev/null || fail "migration $name failed on the fixture"
  block=$FIXTURE/fixture/${name%.up.sql}.sql
  if [ -f "$block" ]; then
    psql_as postgres colliedesk -1 <"$block" >/dev/null || fail "fixture file ${block#"$ROOT"/} failed"
    blocks=$((blocks + 1))
  fi
done <"$TMP/previous-migrations"
[ "$blocks" -gt 0 ] || fail 'no fixture file matched a migration of the previous release'
# The files the rows point at, written by the server image so they belong to its user.
psql_as postgres colliedesk -At -F "$tab" <"$FIXTURE/files.sql" |
  server_run "while IFS='$tab' read -r key content; do mkdir -p \"/data/attachments/\${key%/*}\" && printf '%s' \"\$content\" >\"/data/attachments/\$key\"; done" 2>/dev/null ||
  fail 'could not write the fixture files'

# The previous release takes the fixture as its own: nothing left to migrate, the same schema.
migrate || fail "$PREVIOUS did not accept the fixture database"
schema() {
  dc exec -T postgres pg_dump -U postgres --schema-only --no-owner "$1" |
    grep -v -e '^--' -e '^$' -e '^SET ' -e '^SELECT pg_catalog.set_config' -e '^\\restrict' -e '^\\unrestrict' |
    sed "s/colliedesk_app_$1/colliedesk_app_DB/g"
}
schema reference >"$TMP/reference.schema"
schema colliedesk >"$TMP/fixture.schema"
diff "$TMP/reference.schema" "$TMP/fixture.schema" >"$TMP/schema.diff" || {
  cat "$TMP/schema.diff" >&2
  fail "the fixture database's schema differs from the one $PREVIOUS makes"
}

# Every stored file as "file<TAB>storage key<TAB>sha256<TAB>size".
files() {
  server_run 'cd /data/attachments && find . -type f ! -path "./.incoming/*" | sort | while read -r f; do
    printf "file\t%s\t%s\t%s\n" "${f#./}" "$(sha256sum <"$f" | cut -d" " -f1)" "$(stat -c %s "$f")"; done' 2>/dev/null
}
# check MODE OUT WORKSPACE [INPUT...]: tests/upgrade/check.sql on $CHECK_DB with the files stored now
# and the input lines given (kind<TAB>a<TAB>b<TAB>c). OUT gets the snapshot; problems go to stderr.
check() {
  mode=$1 out=$2 workspace=$3
  shift 3
  files >"$TMP/files.now"
  { echo "SET upgrade.mode = '$mode'; SET upgrade.workspace = '$workspace'; SET upgrade.allow = 'allow';"
    echo 'CREATE TEMP TABLE input (kind text, a text, b text, c text);'
    echo 'COPY input FROM stdin;'
    cat "$TMP/files.now" "$@"
    printf '\\.\n'
    cat "$FIXTURE/check.sql"
  } | psql_as postgres "$CHECK_DB" >"$out.raw" 2>"$out.err"
  result=$?
  grep -v '^problem' "$out.raw" >"$out" || :
  if [ "$result" != 0 ]; then
    grep '^problem' "$out.raw" | cut -f2- >&2 || :
    grep -v '^psql' "$out.err" >&2 || :
    return 1
  fi
}
workspace_id() { echo "SELECT id FROM workspaces WHERE slug = '$1';" | psql_as postgres colliedesk -At; }
acme=$(workspace_id acme) beta=$(workspace_id beta) cobalt=$(workspace_id cobalt)
[ -n "$acme" ] && [ -n "$beta" ] && [ -n "$cobalt" ] || fail 'the fixture has no acme, beta and cobalt workspaces'

check snapshot "$TMP/before" '' || fail 'the fixture database fails its own check'
cp "$TMP/files.now" "$TMP/files.before"
step "snapshot: $(grep -c '^rows' "$TMP/before") tables, $(awk -F "$tab" '$1 == "rows" { n += $3 } END { print n }' "$TMP/before") rows, $(grep -c . "$TMP/files.now") files"

# The check must catch what it is for: lost rows and a row linked to another workspace.
{ echo 'BEGIN;'
  echo "DELETE FROM messages WHERE id = md5('msg-beta-2-note')::uuid OR id = md5('msg-acme-60-3')::uuid;"
  echo "UPDATE saved_replies SET created_by_agent_id = md5('agent-beta-cy')::uuid WHERE id = md5('reply-acme-2')::uuid;"
  # A policy that lets the request role read every workspace's conversations.
  echo "DO \$\$ BEGIN EXECUTE format('CREATE POLICY upgrade_selftest_leak ON conversations FOR SELECT TO %I USING (true)',
    (SELECT roles[1] FROM pg_policies WHERE tablename = 'workspaces' AND policyname = 'workspace_isolation')); END \$\$;"
  echo "SET upgrade.mode = 'compare'; SET upgrade.workspace = ''; SET upgrade.allow = 'allow';"
  echo 'CREATE TEMP TABLE input (kind text, a text, b text, c text);'
  echo 'COPY input FROM stdin;'
  cat "$TMP/before" "$TMP/files.now"
  printf '\\.\n'
  cat "$FIXTURE/check.sql"
} | psql_as postgres colliedesk >"$TMP/selftest" 2>&1 && fail 'the check passed a database that lost rows'
grep -q '^problem.messages: [0-9]* rows, had [0-9]*' "$TMP/selftest" &&
  grep -q 'saved_replies_created_by_agent_id_fkey: 1 rows of saved_replies point at a row of agents in another workspace' "$TMP/selftest" &&
  grep -q "^problem.conversations: the request role in workspace $acme reads [0-9]* rows of other workspaces" "$TMP/selftest" ||
  { cat "$TMP/selftest" >&2; fail 'the check missed deliberately lost rows, a cross-workspace link or a leaking policy'; }

# The backup, exactly as docs/user/upgrade.md tells an operator to take it.
step 'backup'
(cd "$TMP" && "$DOCKER" compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" --format=custom colliedesk' >colliedesk.dump) ||
  fail 'pg_dump failed'
(cd "$TMP" && "$DOCKER" compose run --rm --no-deps -T server 'tar -C /data/attachments -cf - .' >attachments.tar 2>/dev/null) ||
  fail 'could not archive the attachments'

# One workspace back from the backup with restore-workspace.sh, server stopped: acme is damaged first,
# must come back exactly as the backup has it (brought to the running schema), twice the same,
# with every session revoked, while beta and cobalt do not change.
restore_one_workspace() {
  step "restoring workspace acme from the backup ($1)"
  dc stop server >/dev/null 2>&1
  check snapshot "$TMP/acme.expected-count" "$acme" || fail 'acme fails its check before the restore'
  check snapshot "$TMP/beta.before" "$beta" || fail 'beta fails its check before the restore'
  check snapshot "$TMP/cobalt.before" "$cobalt" || fail 'cobalt fails its check before the restore'
  conversations=$(awk -F "$tab" -v id="$acme" '$1 == "owner" && $2 == "conversations" && $3 == id { print $4 }' "$TMP/acme.expected-count")
  echo "DELETE FROM conversations WHERE workspace_id = '$acme' AND subject LIKE 'Imported ticket #10_%';" | psql_as postgres colliedesk
  server_run "rm -f /data/attachments/$acme/*/*" 2>/dev/null
  (cd "$TMP" && sh "$ROOT/restore-workspace.sh" --workspace acme --database colliedesk.dump --attachments attachments.tar >"$TMP/restore.out" 2>&1) &&
    fail 'restore-workspace.sh replaced an existing workspace without --replace'
  grep -q -- 'add --replace' "$TMP/restore.out" || { cat "$TMP/restore.out" >&2; fail 'restore-workspace.sh did not say why it refused'; }
  for run in 1 2; do
    (cd "$TMP" && sh "$ROOT/restore-workspace.sh" --workspace acme --database colliedesk.dump --attachments attachments.tar --replace --keep-scratch >"$TMP/restore.out" 2>&1) ||
      { cat "$TMP/restore.out" >&2; fail "restore-workspace.sh failed (run $run)"; }
    CHECK_DB=colliedesk_restore
    check snapshot "$TMP/acme.backup" "$acme" || fail 'the scratch copy of the backup fails its check'
    CHECK_DB=colliedesk
    check compare "$TMP/acme.after" "$acme" "$TMP/acme.backup" || fail "acme after restore-workspace.sh (run $run) differs from the backup"
    grep -qx "owner${tab}conversations${tab}$acme${tab}$conversations" "$TMP/acme.after" ||
      fail "acme has not got its $conversations conversations back"
    check compare "$TMP/beta.after" "$beta" "$TMP/beta.before" || fail "restoring acme changed beta (run $run)"
    check compare "$TMP/cobalt.after" "$cobalt" "$TMP/cobalt.before" || fail "restoring acme changed cobalt (run $run)"
  done
  echo "SELECT count(*) FROM sessions WHERE workspace_id = '$acme' AND revoked_at IS NULL;" | psql_as postgres colliedesk -At | grep -qx 0 ||
    fail 'a restored session of acme is still valid'
  echo "DROP DATABASE colliedesk_restore;" | psql_as postgres postgres
  step 'acme restored twice the same; beta and cobalt untouched'
}

# The whole install back to the backup and the previous image, as docs/user/upgrade.md says.
roll_back() {
  step "rolling back to $PREVIOUS from the backup"
  (cd "$TMP" && "$DOCKER" compose stop server >/dev/null 2>&1 &&
    "$DOCKER" compose exec -T postgres sh -c 'dropdb -U "$POSTGRES_USER" colliedesk && createdb -U "$POSTGRES_USER" -O colliedesk colliedesk' &&
    "$DOCKER" compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d colliedesk --exit-on-error' <colliedesk.dump &&
    "$DOCKER" compose run --rm --no-deps -T server 'find /data/attachments -mindepth 1 -delete && tar -C /data/attachments -xf -' <attachments.tar 2>/dev/null) ||
    fail 'the documented restore failed'
  use_image "$PREVIOUS"
  migrate || fail "$PREVIOUS did not take its restored database"
  check compare "$TMP/rolled-back" '' "$TMP/before" || fail 'the restored install differs from the backup'
  step 'the install is the backup again'
}

api() { curl -fsS "http://localhost:$PORT$1" -H "Authorization: Bearer $2"; }
sign_in() {
  link=$(dc exec -T server /app/collie pair --workspace "$1" --email "$2" 2>/dev/null | grep -E '^http' | tail -n 1)
  [ -n "$link" ] || fail "collie pair printed no link for $2 in $1"
  curl -fsS -X POST "http://localhost:$PORT/api/v1/auth/redeem" -H 'Content-Type: application/json' \
    -d "{\"token\":\"${link#*token=}\",\"session_type\":\"bearer\",\"label\":\"upgrade test\"}" |
    sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
}
ids() { echo "SELECT id FROM $1 WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$2');" | psql_as postgres colliedesk -At; }
# The inbox of a workspace lists its conversations and no conversation or contact of another, and
# another workspace's conversation is not found.
check_inbox() {
  slug=$1 email=$2
  token=$(sign_in "$slug" "$email")
  [ -n "$token" ] || fail "could not sign in to $slug"
  inbox=$(api /api/v1/conversations "$token") || fail "the inbox of $slug did not load"
  # Every id in the answer, one per line.
  printf '%s\n' "$inbox" | tr -c '0-9a-f-' '\n' | grep -E '^[0-9a-f-]{36}$' | sort -u >"$TMP/inbox-$slug.ids"
  ids conversations "$slug" >"$TMP/own"
  found=$(grep -cxF -f "$TMP/own" "$TMP/inbox-$slug.ids" || :)
  [ "$found" -gt 0 ] || fail "the inbox of $slug shows none of its conversations: $inbox"
  for other in acme beta cobalt; do
    [ "$other" = "$slug" ] && continue
    { ids conversations "$other"; ids contacts "$other"; } >"$TMP/foreign"
    if grep -qxF -f "$TMP/foreign" "$TMP/inbox-$slug.ids"; then fail "the inbox of $slug shows rows of $other"; fi
    status=$(curl -sS -o /dev/null -w '%{http_code}' "http://localhost:$PORT/api/v1/conversations/$(head -n 1 "$TMP/foreign")" -H "Authorization: Bearer $token")
    [ "$status" = 404 ] || fail "$slug reading a conversation of $other answered $status, want 404"
  done
  step "$slug: the inbox lists $found of its conversations and nothing of the other workspaces"
}

if [ "$RESTORE" = before ]; then
  restore_one_workspace "$PREVIOUS"
  roll_back
fi

# The upgrade: the candidate's migrate, then the same snapshot over the previous release's columns.
if [ "$WAIT" -gt 0 ]; then
  waited=0
  until "$DOCKER" image inspect "$CANDIDATE" >/dev/null 2>&1; do
    [ "$waited" -lt "$WAIT" ] || fail "candidate image $CANDIDATE did not appear within ${WAIT}s"
    sleep 1
    waited=$((waited + 1))
  done
fi
step "upgrading to $CANDIDATE ($("$DOCKER" run --rm "$CANDIDATE" version))"
use_image "$CANDIDATE"
server_run '/app/collie migrate' >"$TMP/migrate.out" 2>&1 || { cat "$TMP/migrate.out" >&2; fail "$CANDIDATE could not migrate the previous release's database"; }
echo 'SELECT name FROM schema_migrations ORDER BY name;' | psql_as postgres colliedesk -At >"$TMP/candidate-migrations"
grep -vxF -f "$TMP/previous-migrations" "$TMP/candidate-migrations" >"$TMP/new-migrations" || :
# Declared changes count only when the migration that makes them is part of this upgrade.
: >"$TMP/allowed"
grep -v -e '^#' -e '^$' "$FIXTURE/allowed-changes.tsv" >"$TMP/declared" || :
while IFS="$tab" read -r table column migration reason; do
  [ -n "$reason" ] || fail "allowed-changes.tsv: $table $column has no migration and reason"
  if grep -qxF "$migration" "$TMP/new-migrations"; then
    printf 'allow\t%s\t%s\t%s\n' "$table" "$column" "$migration" >>"$TMP/allowed"
  fi
done <"$TMP/declared"
step "$(grep -c . "$TMP/new-migrations" || :) new migrations, $(grep -c . "$TMP/allowed" || :) declared changes"
check compare "$TMP/after" '' "$TMP/before" "$TMP/allowed" || fail "upgrading to $CANDIDATE lost or changed data (above)"
diff "$TMP/files.before" "$TMP/files.now" >&2 || fail 'the upgrade changed stored files (above)'

# The upgraded server boots and serves each workspace its own inbox.
step 'serving the upgraded install'
dc up -d --wait server >/dev/null 2>&1 || fail "$CANDIDATE did not become healthy on the upgraded database"
check_inbox acme owner@acme.example
check_inbox beta admin@beta.example

if [ "$RESTORE" = after ]; then
  restore_one_workspace "$CANDIDATE"
  roll_back
  dc up -d --wait server >/dev/null 2>&1 || fail "$PREVIOUS did not become healthy after the rollback"
  check_inbox acme owner@acme.example
fi

step "passed: $PREVIOUS -> $CANDIDATE keeps every row and file; both restores work"
