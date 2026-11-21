#!/bin/sh
# Test of upgrade-postgres.sh: an install laid out by install.sh of v1.0.1 (bundled PostgreSQL 17,
# no PGDATA line), filled with the upgrade fixture, moves to PostgreSQL 19 keeping every row and
# file and serving each workspace its own inbox, survives an installer rerun and a restart on 19,
# and goes back to 17 with --rollback, data as it was. A failure in the middle leaves the install
# on 17 and serving, even with its terminal gone; a run killed in the restore leaves compose.yaml
# on 17, so a plain docker compose up serves the data as it was, and an undo that cannot list the
# volumes keeps the half-made one known to the next run. Once compose.yaml names 19 the server may
# write there, so nothing points it away again by itself: a run killed, hung up or unable to start
# on 19 leaves compose.yaml and the volume on 19 for the next run to finish or --rollback to undo,
# and a run after the operator put the old compose file back keeps the 19 volume. A hangup in a
# rollback after 17 started leaves compose.yaml on 17 for --rollback to finish; a rollback killed
# before it changed compose.yaml starts over, and one that cannot put compose.yaml back on 19
# leaves it on 17 with nothing started and the rollback unfinished. The compose files keep the
# mode the install gave them, and what pg_restore writes to stderr goes to restore.log, not the
# terminal. A finish whose PostgreSQL 19 is not the cluster the restore wrote (PGDATA edited)
# stops it and starts nothing, and so does a rollback whose PostgreSQL 17 is not the cluster of
# before the upgrade, which puts compose.yaml back as it was, and the undo of a killed run onto
# such a cluster. The refusals (external database, already upgraded, unknown state, unfinished
# rollback, too little disk, an environment PGDATA cannot be added to, a rollback whose old volume
# is gone, a finish whose 19 volume is gone, a first run or a rollback in another compose project,
# another compose file changing the postgres service, a compose.yaml naming neither volume of the
# upgrade) change nothing and print no password. All of that runs the server of v1.0.1, the last
# release that runs on PostgreSQL 17. Then the install pulls IMAGE, which runs on 19 or newer only:
# it refuses the 17 database and names upgrade-postgres.sh, which moves the install to 19 once the
# 19 volume an earlier rollback kept is removed, and IMAGE then migrates and serves each workspace
# its own inbox. Needs Docker with Compose v2 and curl.
#   sh tests/upgrade-postgres-test.sh LAYOUT IMAGE
# LAYOUT is the database login layout of the install:
#   owner      POSTGRES_USER=postgres, the server logs in as the colliedesk owner role (install.sh
#              since the request login)
#   superuser  no POSTGRES_USER: colliedesk is the PostgreSQL superuser and the server's login
# IMAGE is the Colliedesk server image the install moves to (colliedesk:ci in CI).
# UPGRADE_POSTGRES_TEST_KEEP=1 leaves the install running for a look.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
LAYOUT=${1:?usage: upgrade-postgres-test.sh owner|superuser IMAGE}
IMAGE=${2:?usage: upgrade-postgres-test.sh owner|superuser IMAGE}
case $LAYOUT in owner) SUPER=postgres ;; superuser) SUPER=colliedesk ;; *) echo 'LAYOUT is owner or superuser' >&2; exit 2 ;; esac
DOCKER=${DOCKER:-docker}
BASE_TAG=v1.0.1
# The server of $BASE_TAG runs on PostgreSQL 17 and on 19; every later release needs 19.
PREVIOUS=ghcr.io/colliedesk/colliedesk:${BASE_TAG#v}
# Ports and names derive from the checkout path and the layout, so both layouts run at once.
PORT=${COLLIEDESK_UPGRADE_POSTGRES_PORT:-$((20500 + $(printf '%s %s' "$ROOT" "$LAYOUT" | cksum | cut -d" " -f1) % 800))}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/colliedesk-pgup.XXXXXX")
COMPOSE_PROJECT_NAME=colliedesk-pgup-$LAYOUT-$$
FIXTURE=$ROOT/tests/upgrade
tab=$(printf '\t')
export COMPOSE_PROJECT_NAME

started=$(date +%s)
step() { printf 'upgrade-postgres test %s [%ss]: %s\n' "$LAYOUT" "$(($(date +%s) - started))" "$1"; }

cleanup() {
  status=$?
  if [ "$status" != 0 ] && [ -f "$TMP/compose.yaml" ]; then
    (cd "$TMP" && "$DOCKER" compose logs --no-color --tail 40 >&2 2>&1) || :
  fi
  if [ "${UPGRADE_POSTGRES_TEST_KEEP:-}" = 1 ]; then
    echo "kept: cd $TMP && COMPOSE_PROJECT_NAME=$COMPOSE_PROJECT_NAME docker compose ps" >&2
  else
    # The second project is the one a rollback run in the wrong project would make.
    for project in "$COMPOSE_PROJECT_NAME" "$COMPOSE_PROJECT_NAME-other"; do
      if [ -f "$TMP/compose.yaml" ]; then
        (cd "$TMP" && COMPOSE_PROJECT_NAME=$project "$DOCKER" compose down -v --remove-orphans >/dev/null 2>&1) || :
      fi
      # The volume the compose file no longer names (the other major's) is the project's too.
      "$DOCKER" volume ls -q --filter "label=com.docker.compose.project=$project" |
        while read -r volume; do "$DOCKER" volume rm "$volume" >/dev/null 2>&1 || :; done
    done
    rm -rf "$TMP"
  fi
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'upgrade-postgres test %s: %s\n' "$LAYOUT" "$1" >&2
  exit 1
}

dc() { (cd "$TMP" && "$DOCKER" compose "$@"); }
psql_as() {
  user=$1 db=$2
  shift 2
  dc exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U "$user" -d "$db" "$@"
}
server_run() { dc run --rm --no-deps -T server "$@"; }
migrate() { server_run '/app/collie migrate' 2>/dev/null | grep -qx 'migrations applied'; }
pg_major() { echo 'SHOW server_version_num;' | psql_as "$SUPER" postgres -At | cut -c1-2; }
# The volume the running postgres container keeps its data in.
data_volume() {
  "$DOCKER" inspect -f '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Name}}{{end}}{{end}}' "$(dc ps -q postgres)"
}
# The project's volume made for compose volume key $1, if any.
volume_for() {
  "$DOCKER" volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME" \
    --filter "label=com.docker.compose.volume=$1"
}
state() { sed -n "s/^$1=//p" "$TMP/.postgres-upgrade"; }
mode() { stat -c %a "$1"; }
# upgrade [ARGS...]: upgrade-postgres.sh in the install directory; output in $TMP/upgrade.out.
# script_project, when set, is the COMPOSE_PROJECT_NAME the script runs with.
script_project=''
upgrade() {
  (cd "$TMP" && COMPOSE_PROJECT_NAME=${script_project:-$COMPOSE_PROJECT_NAME} sh "$ROOT/upgrade-postgres.sh" "$@" >"$TMP/upgrade.out" 2>&1)
}
# No secret of .env reaches the operator's terminal.
no_secret_printed() {
  for secret in "$database_password" "$postgres_password" ${marker:+"$marker"}; do
    if grep -qF "$secret" "$TMP/upgrade.out"; then fail "upgrade-postgres.sh printed a database password ($1)"; fi
  done
}

# The install as install.sh of $BASE_TAG laid it out: PostgreSQL 17, its data at the image's default
# path, no PGDATA line.
# The other layout's run, and upgrade-test.sh, fetch into the same shallow checkout at the same time:
# a fetch that finds .git/shallow.lock taken fails, so wait a second and look again.
fetches=0
until git -C "$ROOT" show "$BASE_TAG:install.sh" >"$TMP/install.sh" 2>/dev/null; do
  fetches=$((fetches + 1))
  [ "$fetches" -le 10 ] || fail "cannot read install.sh of $BASE_TAG: $(cat "$TMP/fetch.err")"
  git -C "$ROOT" fetch -q --depth 1 origin "refs/tags/$BASE_TAG:refs/tags/$BASE_TAG" 2>"$TMP/fetch.err" || sleep 1
done
sed -n "/^cat >\"\$TARGET_DIR\/compose\.yaml\.next\" <<'COMPOSE'$/,/^COMPOSE$/p" "$TMP/install.sh" | sed '1d;$d' >"$TMP/compose.yaml"
mkdir -p "$TMP/initdb"
sed -n "/^cat >\"\$TARGET_DIR\/initdb\/10-colliedesk-owner.sh\" <<'INITDB'$/,/^INITDB$/p" "$TMP/install.sh" | sed '1d;$d' >"$TMP/initdb/10-colliedesk-owner.sh"
chmod 755 "$TMP/initdb/10-colliedesk-owner.sh"
rm "$TMP/install.sh"
grep -qx '    image: postgres:17-bookworm' "$TMP/compose.yaml" && ! grep -q PGDATA "$TMP/compose.yaml" ||
  fail "the compose file of $BASE_TAG is not the PostgreSQL 17 layout this test starts from"

secret() { od -An -N16 -tx1 /dev/urandom | tr -d ' \n'; }
postgres_password=$(secret)
{
  if [ "$LAYOUT" = owner ]; then
    database_password=$(secret)
    echo 'POSTGRES_USER=postgres'
    echo "POSTGRES_PASSWORD=$postgres_password"
    echo "COLLIEDESK_OWNER_PASSWORD=$database_password"
  else
    database_password=$postgres_password
    echo "POSTGRES_PASSWORD=$postgres_password"
  fi
  echo "DATABASE_URL=postgres://colliedesk:$database_password@postgres:5432/colliedesk?sslmode=disable"
  echo "DATABASE_REQUEST_URL=postgres://colliedesk_request:$(secret)@postgres:5432/colliedesk?sslmode=disable"
  echo "COLLIE_DESK_IMAGE=$PREVIOUS"
  echo "COLLIE_DESK_PORT=$PORT"
  echo "APP_ORIGIN=http://localhost:$PORT"
  echo 'DISABLE_TELEMETRY=true'
} >"$TMP/.env"
"$DOCKER" image inspect "$IMAGE" >/dev/null 2>&1 || fail "no image $IMAGE"
"$DOCKER" image inspect "$PREVIOUS" >/dev/null 2>&1 || "$DOCKER" pull -q "$PREVIOUS" >/dev/null || fail "cannot pull $PREVIOUS"
dc up -d --wait postgres >/dev/null 2>&1 || fail 'postgres did not start'
[ "$(pg_major)" = 17 ] || fail "the $BASE_TAG install does not run PostgreSQL 17"
old_volume=$(data_volume)
compose_mode=$(mode "$TMP/compose.yaml")

# The fixture: every migration of $BASE_TAG in order, as the owner and with the setting
# `collie migrate` uses, each followed by its fixture file, in one psql session. Migration files
# never change once merged (migration-check), so the files of this checkout are the ones it ran.
git -C "$ROOT" ls-tree --name-only "$BASE_TAG" server/migrations/ | sed -n 's#^server/migrations/\(.*\.up\.sql\)$#\1#p' >"$TMP/base-migrations"
[ -s "$TMP/base-migrations" ] || fail "cannot list the migrations of $BASE_TAG"
while read -r name; do
  [ -f "$ROOT/server/migrations/$name" ] || fail "migration $name of $BASE_TAG is not in this checkout"
done <"$TMP/base-migrations"
step "building the fixture on the $(grep -c . "$TMP/base-migrations") migrations of $BASE_TAG"
{
  echo 'SET ROLE colliedesk;'
  echo 'CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());'
  echo 'RESET ROLE;'
  while read -r name; do
    file=$ROOT/server/migrations/$name
    echo 'BEGIN;'
    echo 'SET LOCAL ROLE colliedesk;'
    echo "SELECT set_config('app.privileged', 'on', true) \\g /dev/null"
    cat "$file"
    echo
    echo "INSERT INTO schema_migrations(name) VALUES ('$name');"
    echo 'COMMIT;'
    block=$FIXTURE/fixture/${name%.up.sql}.sql
    if [ -f "$block" ]; then
      echo 'BEGIN;'
      cat "$block"
      echo
      echo 'COMMIT;'
    fi
  done <"$TMP/base-migrations"
} | psql_as "$SUPER" colliedesk >/dev/null || fail 'the fixture did not build'
psql_as "$SUPER" colliedesk -At -F "$tab" <"$FIXTURE/files.sql" |
  server_run "while IFS='$tab' read -r key content; do mkdir -p \"/data/attachments/\${key%/*}\" && printf '%s' \"\$content\" >\"/data/attachments/\$key\"; done" 2>/dev/null ||
  fail 'could not write the fixture files'
migrate || fail "$PREVIOUS did not accept the fixture database"

files() {
  server_run 'cd /data/attachments && find . -type f ! -path "./.incoming/*" | sort | while read -r f; do
    printf "file\t%s\t%s\t%s\n" "${f#./}" "$(sha256sum <"$f" | cut -d" " -f1)" "$(stat -c %s "$f")"; done' 2>/dev/null
}
# check MODE OUT [INPUT...]: tests/upgrade/check.sql over every workspace (see tests/upgrade-test.sh).
check() {
  mode=$1 out=$2
  shift 2
  files >"$TMP/files.now"
  { echo "SET upgrade.mode = '$mode'; SET upgrade.workspace = ''; SET upgrade.allow = 'allow';"
    echo 'CREATE TEMP TABLE input (kind text, a text, b text, c text);'
    echo 'COPY input FROM stdin;'
    cat "$TMP/files.now" "$@"
    printf '\\.\n'
    cat "$FIXTURE/check.sql"
  } | psql_as "$SUPER" colliedesk >"$out.raw" 2>"$out.err"
  result=$?
  grep -v '^problem' "$out.raw" >"$out" || :
  if [ "$result" != 0 ]; then
    grep '^problem' "$out.raw" | cut -f2- >&2 || :
    grep -v '^psql' "$out.err" >&2 || :
    return 1
  fi
}
# The running server keeps writing (background work, its heartbeat), so the data is read with the
# server stopped, the way upgrade-postgres.sh dumps it. Tables in $TMP/churn are the ones a server
# start rewrites by itself.
# A fixture email whose file is not stored is retried on every start, queued replies are tried
# again with no mail relay to take them, and the custom help center domains are checked again with
# no DNS record or address to prove them.
{ printf 'allow\tchannel_inbound_events\t%s\tretried on each server start\n' attempts error
  printf 'allow\thelp_center_domains\t%s\tchecked on each server start\n' checked_at check_error \
    address_checked_at address_error failing_since status pending_reason
  printf 'allow\tchannel_outbox\t%s\tdelivery tried again\n' status attempts next_attempt_at locked_until claim_token \
    last_error last_error_code held_by_pause updated_at
} >"$TMP/churn"
# baseline NAME: the data now, as $TMP/NAME, the server serving again after.
baseline() {
  dc stop server >/dev/null 2>&1
  check snapshot "$TMP/$1" || fail "the database fails its own check ($1)"
  cp "$TMP/files.now" "$TMP/$1.files"
  dc up -d --wait >/dev/null 2>&1 || fail "the install did not start again after reading $1"
}
# same_data WHEN NAME [ALLOWED [AS]]: every row and file of baseline NAME is there as it was, but for
# the changes ALLOWED (allow lines) declares; AS keeps the data now as baseline AS, in the same stop.
same_data() {
  dc stop server >/dev/null 2>&1
  check compare "$TMP/after" "$TMP/$2" "$TMP/churn" ${3:+"$3"} || fail "the data differs from $2 $1 (above)"
  diff "$TMP/$2.files" "$TMP/files.now" >&2 || fail "the stored files differ from $2 $1 (above)"
  if [ -n "${4:-}" ]; then
    check snapshot "$TMP/$4" || fail "the database fails its own check ($4)"
    cp "$TMP/files.now" "$TMP/$4.files"
  fi
  dc up -d --wait >/dev/null 2>&1 || fail "the install did not start again $1"
}
dc up -d --wait >/dev/null 2>&1 || fail "$PREVIOUS did not become healthy on the fixture"
baseline before
step "fixture: $(grep -c '^rows' "$TMP/before") tables, $(grep -c . "$TMP/before.files") files, serving on PostgreSQL 17"

api() { curl -fsS "http://localhost:$PORT$1" -H "Authorization: Bearer $2"; }
sign_in() {
  link=$(dc exec -T server /app/collie pair --workspace "$1" --email "$2" 2>/dev/null | grep -E '^http' | tail -n 1)
  [ -n "$link" ] || fail "collie pair printed no link for $2 in $1"
  curl -fsS -X POST "http://localhost:$PORT/api/v1/auth/redeem" -H 'Content-Type: application/json' \
    -d "{\"token\":\"${link#*token=}\",\"session_type\":\"bearer\",\"label\":\"upgrade-postgres test\"}" |
    sed -n 's/.*"token":"\([^"]*\)".*/\1/p'
}
ids() { echo "SELECT id FROM $1 WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = '$2');" | psql_as "$SUPER" colliedesk -At; }
# The inbox of a workspace lists its own conversations and nothing of the other workspaces.
check_inbox() {
  slug=$1 email=$2
  token=$(sign_in "$slug" "$email")
  [ -n "$token" ] || fail "could not sign in to $slug"
  api /api/v1/conversations "$token" | tr -c '0-9a-f-' '\n' | grep -E '^[0-9a-f-]{36}$' | sort -u >"$TMP/inbox.ids" ||
    fail "the inbox of $slug did not load"
  ids conversations "$slug" >"$TMP/own"
  grep -qxF -f "$TMP/own" "$TMP/inbox.ids" || fail "the inbox of $slug shows none of its conversations"
  for other in acme beta cobalt; do
    [ "$other" = "$slug" ] && continue
    { ids conversations "$other"; ids contacts "$other"; } >"$TMP/foreign"
    if grep -qxF -f "$TMP/foreign" "$TMP/inbox.ids"; then fail "the inbox of $slug shows rows of $other"; fi
  done
}
server_running() { dc ps --status running --services 2>/dev/null | grep -qx server; }

# refused WHAT MESSAGE [ARGS...]: upgrade-postgres.sh refuses, says MESSAGE, and changes nothing.
refused() {
  what=$1 message=$2
  shift 2
  cp "$TMP/compose.yaml" "$TMP/compose.before-refusal"
  volume=$(data_volume)
  if upgrade "$@"; then cat "$TMP/upgrade.out" >&2; fail "upgrade-postgres.sh did not refuse $what"; fi
  grep -qF -- "$message" "$TMP/upgrade.out" || { cat "$TMP/upgrade.out" >&2; fail "refusing $what, upgrade-postgres.sh did not say: $message"; }
  cmp -s "$TMP/compose.before-refusal" "$TMP/compose.yaml" || fail "refusing $what changed compose.yaml"
  [ "$(data_volume)" = "$volume" ] || fail "refusing $what moved the database to another volume"
  server_running || fail "refusing $what left the server stopped"
  no_secret_printed "refusing $what"
}

# upgraded REPLACED [AS]: the install runs PostgreSQL 19 on a new volume, with every row and file,
# serving; compose.yaml.before-postgres-19 is REPLACED; AS keeps the data on 19 as baseline AS.
upgraded() {
  grep -q 'is on PostgreSQL 19' "$TMP/upgrade.out" || { cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh did not say the install is on PostgreSQL 19'; }
  no_secret_printed 'upgrading'
  [ "$(pg_major)" = 19 ] || fail 'the database is not on PostgreSQL 19 after the upgrade'
  pinned=$(sed -n 's/^POSTGRES_IMAGE=//p' "$ROOT/install.sh")
  grep -qxF "    image: $pinned" "$TMP/compose.yaml" || fail "compose.yaml does not run $pinned, the image install.sh pins"
  grep -qx '      PGDATA: /var/lib/postgresql/data' "$TMP/compose.yaml" || fail 'compose.yaml has no PGDATA line for PostgreSQL 19'
  grep -qx '      - postgres19-data:/var/lib/postgresql/data' "$TMP/compose.yaml" && grep -qx '  postgres19-data:' "$TMP/compose.yaml" ||
    fail 'compose.yaml does not keep the PostgreSQL 19 data in the volume postgres19-data'
  [ "$(data_volume)" = "$(volume_for postgres19-data)" ] || fail 'PostgreSQL 19 does not run on the volume postgres19-data'
  [ "$(volume_for postgres-data)" = "$old_volume" ] || fail 'the PostgreSQL 17 volume is gone after the upgrade'
  cmp -s "$TMP/compose.yaml.before-postgres-19" "$1" || fail 'compose.yaml.before-postgres-19 is not the compose file the upgrade replaced'
  [ "$(state state)" = 'done' ] || fail "the state file says $(state state), want done"
  dump_dir=$TMP/$(state dump_dir)
  for file in "$TMP/.postgres-upgrade" "$dump_dir/roles.sql" "$dump_dir/colliedesk.dump"; do
    [ -s "$file" ] || fail "no ${file#"$TMP"/} after the upgrade"
    [ "$(mode "$file")" = 600 ] || fail "${file#"$TMP"/} is readable by others (mode $(mode "$file"))"
  done
  [ "$(mode "$dump_dir")" = 700 ] || fail "${dump_dir#"$TMP"/} is open to others (mode $(mode "$dump_dir"))"
  for file in "$TMP/compose.yaml" "$TMP/compose.yaml.before-postgres-19"; do
    [ "$(mode "$file")" = "$compose_mode" ] || fail "${file#"$TMP"/} has mode $(mode "$file"), want $compose_mode as the install had"
  done
  server_running || fail 'the server is not running after the upgrade'
  same_data 'after the upgrade' before
  check_inbox acme owner@acme.example
  check_inbox beta admin@beta.example
  if [ -n "${2:-}" ]; then baseline "$2"; fi
  step "on PostgreSQL 19: every row and file kept, each inbox its own ($(grep 'downtime' "$TMP/upgrade.out" | head -n 1))"
}

# rolled_back: --rollback put the install on PostgreSQL 17 and its old volume, data as before.
# rolled_back gone: the same with stdout and stderr failing on every write (an SSH session that dropped).
# rolled_back '' NAME: the data as baseline NAME instead of before.
rolled_back() {
  if [ "${1:-}" = gone ]; then
    (cd "$TMP" && sh "$ROOT/upgrade-postgres.sh" --rollback >/dev/full 2>&1) || fail 'a rollback whose terminal is gone failed'
  else
    upgrade --rollback || { cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh --rollback failed'; }
    no_secret_printed 'rolling back'
    grep -q 'postgres19-data' "$TMP/upgrade.out" || { cat "$TMP/upgrade.out" >&2; fail 'the rollback did not name the volume that keeps the writes made on 19'; }
  fi
  [ "$(pg_major)" = 17 ] || fail 'the database is not on PostgreSQL 17 after the rollback'
  grep -qx '    image: postgres:17-bookworm' "$TMP/compose.yaml" || fail 'compose.yaml does not run postgres:17-bookworm after the rollback'
  grep -qx '      - postgres-data:/var/lib/postgresql/data' "$TMP/compose.yaml" && grep -qx '  postgres-data:' "$TMP/compose.yaml" ||
    fail 'compose.yaml does not name the volume postgres-data after the rollback'
  [ "$(data_volume)" = "$old_volume" ] || fail 'PostgreSQL 17 does not run on its old volume after the rollback'
  [ -n "$(volume_for postgres19-data)" ] || fail 'the rollback removed the PostgreSQL 19 volume'
  [ ! -e "$TMP/compose.yaml.before-rollback" ] || fail 'the rollback left compose.yaml.before-rollback'
  [ "$(mode "$TMP/compose.yaml")" = "$compose_mode" ] || fail "the rollback left compose.yaml with mode $(mode "$TMP/compose.yaml"), want $compose_mode"
  [ "$(state state)" = rolled_back ] || fail "the state file says $(state state), want rolled_back"
  server_running || fail 'the server is not running after the rollback'
  same_data 'after the rollback' "${2:-before}"
  check_inbox acme owner@acme.example
  refused 'a second upgrade over the kept PostgreSQL 19 volume' 'postgres19-data'
  step 'rolled back: PostgreSQL 17 on its old volume, data as before, the 19 volume kept'
}

# The server image .env names: the one the install runs.
env_image() { sed -n 's/^COLLIE_DESK_IMAGE=//p' "$TMP/.env"; }
# The installer, run again with the image the install runs: it must keep the major and the volume
# the install runs on.
rerun_installer() {
  base=$1
  touch "$TMP/.initialized"
  major=$(pg_major) volume=$(data_volume)
  out=$(COLLIE_DESK_IMAGE=$(env_image) sh "$ROOT/install.sh" --dir "$TMP" --name 'Upgrade Desk' --email owner@acme.example </dev/null 2>&1) ||
    { printf '%s\n' "$out" >&2; fail 'the installer failed on the install'; }
  dc up -d --wait >/dev/null 2>&1 || fail 'the install is not healthy after the installer'
  [ "$(pg_major)" = "$major" ] || fail "the installer moved the database off PostgreSQL $major"
  [ "$(data_volume)" = "$volume" ] || fail "the installer moved PostgreSQL $major to another volume"
  # It moves the settings still in .env into the database, as it does on every rerun.
  for table in operator_audit server_config_state server_config_values server_config_versions; do
    printf 'allow\t%s\t*\tthe installer imports the settings in .env\n' "$table"
  done >"$TMP/installer.allowed"
  same_data 'after the installer' "$base" "$TMP/installer.allowed" "$base"
  step "installer rerun: still PostgreSQL $major on the same volume"
}

if [ "$LAYOUT" = owner ]; then
  cp "$TMP/compose.yaml" "$TMP/compose.base"
  # An install that never becomes healthy once compose.yaml names PostgreSQL 19. The server may have
  # written on 19 by then, so the run leaves compose.yaml on 19 and the 19 volume in place, and
  # says how to go on; --rollback from there goes back to 17 with the data as it was.
  cat >"$TMP/docker" <<EOF
#!/bin/sh
if [ "\$*" = 'compose up -d --wait' ] && grep -q postgres19-data compose.yaml; then exit 1; fi
exec "$DOCKER" "\$@"
EOF
  chmod 755 "$TMP/docker"
  if (cd "$TMP" && DOCKER="$TMP/docker" sh "$ROOT/upgrade-postgres.sh" >"$TMP/upgrade.out" 2>&1); then fail 'upgrade-postgres.sh succeeded with an install that never became healthy on 19'; fi
  no_secret_printed 'stopping on 19'
  [ "$(state state)" = started ] || { cat "$TMP/upgrade.out" >&2; fail "a run that could not finish on 19 left the state $(state state), want started"; }
  grep -qx '      - postgres19-data:/var/lib/postgresql/data' "$TMP/compose.yaml" ||
    { cat "$TMP/upgrade.out" >&2; fail 'a run that could not finish on 19 pointed compose.yaml away from the volume the server may have written'; }
  [ -n "$(volume_for postgres19-data)" ] || fail 'a run that could not finish on 19 removed the PostgreSQL 19 volume'
  grep -q -- '--rollback' "$TMP/upgrade.out" || { cat "$TMP/upgrade.out" >&2; fail 'a run that could not finish on 19 did not say how to go on'; }
  if grep -qF 'Undo the edit' "$TMP/upgrade.out"; then cat "$TMP/upgrade.out" >&2; fail 'a run that could not finish on 19 blamed an edit of the postgres service that nobody made'; fi
  step 'could not finish on 19: compose.yaml and the volume stay on 19'
  cp "$TMP/compose.yaml" "$TMP/compose.first19"
  cp "$TMP/.postgres-upgrade" "$TMP/state.first19"
  rolled_back
  "$DOCKER" volume rm "$(volume_for postgres19-data)" >/dev/null
  # The 19 volume removed while compose.yaml still names it: finishing would start an empty
  # database, so the run refuses and makes no volume.
  cp "$TMP/.postgres-upgrade" "$TMP/state.rolled"
  mv "$TMP/compose.first19" "$TMP/compose.yaml"
  mv "$TMP/state.first19" "$TMP/.postgres-upgrade"
  refused 'finishing an upgrade whose PostgreSQL 19 volume is gone' 'postgres19-data'
  [ -z "$(volume_for postgres19-data)" ] || fail 'finishing an upgrade whose PostgreSQL 19 volume is gone made an empty one'
  mv "$TMP/state.rolled" "$TMP/.postgres-upgrade"
  cp "$TMP/compose.base" "$TMP/compose.yaml"
  # The rollback check signed in (a pairing link and a session): the next upgrade carries them.
  baseline before

  # The upgrade straight on the compose file of $BASE_TAG, killed for good once the restore is done
  # and compose.yaml names PostgreSQL 19: the next run finishes it.
  cat >"$TMP/docker" <<EOF
#!/bin/sh
if [ "\$*" = 'compose up -d --wait' ] && grep -q postgres19-data compose.yaml; then kill -9 \$PPID; exit 1; fi
exec "$DOCKER" "\$@"
EOF
  chmod 755 "$TMP/docker"
  if (cd "$TMP" && DOCKER="$TMP/docker" sh "$ROOT/upgrade-postgres.sh" >"$TMP/upgrade.out" 2>&1); then fail 'upgrade-postgres.sh outlived kill -9'; fi
  [ "$(state state)" = started ] || { cat "$TMP/upgrade.out" >&2; fail "a killed upgrade left the state $(state state), want started"; }
  grep -qx '      - postgres19-data:/var/lib/postgresql/data' "$TMP/compose.yaml" || fail 'the upgrade was killed before compose.yaml named PostgreSQL 19'
  # The operator puts the compose file of before the upgrade back by hand: the next run serves 17 and
  # keeps the 19 volume, which the server may have written.
  cp "$TMP/compose.yaml" "$TMP/compose.on19"
  cp "$TMP/.postgres-upgrade" "$TMP/state.on19"
  mv "$TMP/compose.yaml.before-postgres-19" "$TMP/compose.yaml"
  if upgrade; then cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh upgraded over a PostgreSQL 19 volume the server may have written'; fi
  no_secret_printed 'keeping the 19 volume'
  grep -qF 'runs PostgreSQL 17 again' "$TMP/upgrade.out" || { cat "$TMP/upgrade.out" >&2; fail 'a run that put the install back on 17 did not say so'; }
  if grep -qF 'Nothing was changed' "$TMP/upgrade.out"; then cat "$TMP/upgrade.out" >&2; fail 'a run that put the install back on 17 said nothing was changed'; fi
  [ -n "$(volume_for postgres19-data)" ] || { cat "$TMP/upgrade.out" >&2; fail 'a run after the old compose file was put back removed the PostgreSQL 19 volume'; }
  [ "$(pg_major)" = 17 ] && [ "$(data_volume)" = "$old_volume" ] && server_running ||
    fail 'a run after the old compose file was put back does not serve PostgreSQL 17 from its old volume'
  step 'old compose file put back after the switch: serving 17, the 19 volume kept'
  # Back to where the killed run left the install.
  mv "$TMP/compose.on19" "$TMP/compose.yaml"
  cp "$TMP/compose.base" "$TMP/compose.yaml.before-postgres-19"
  mv "$TMP/state.on19" "$TMP/.postgres-upgrade"
  upgrade || { cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh did not finish an upgrade killed after the restore'; }
  upgraded "$TMP/compose.base" on19
  refused 'an install already on PostgreSQL 19' '--rollback'
  # The old compose file put back by hand on a finished upgrade: a plain run says so and changes
  # nothing; --rollback records the install as rolled back on the old major, the file as it is.
  cp "$TMP/compose.yaml" "$TMP/compose.done"
  cp "$TMP/.postgres-upgrade" "$TMP/state.done"
  cp "$TMP/compose.yaml.before-postgres-19" "$TMP/compose.yaml"
  cp "$TMP/compose.yaml" "$TMP/compose.put-back"
  refused 'a finished upgrade whose old compose file was put back by hand' 'sh upgrade-postgres.sh --rollback'
  upgrade --rollback || { cat "$TMP/upgrade.out" >&2; fail '--rollback did not finish on a finished upgrade whose old compose file was put back by hand'; }
  no_secret_printed 'a rollback of a finished upgrade whose old compose file was put back by hand'
  [ "$(state state)" = rolled_back ] || fail "a rollback of a finished upgrade whose old compose file was put back by hand left the state $(state state), want rolled_back"
  cmp -s "$TMP/compose.yaml" "$TMP/compose.put-back" || fail 'a rollback of a finished upgrade whose old compose file was put back by hand changed compose.yaml'
  [ "$(pg_major)" = 17 ] && [ "$(data_volume)" = "$old_volume" ] && server_running ||
    fail 'a rollback of a finished upgrade whose old compose file was put back by hand does not serve PostgreSQL 17 from its old volume'
  [ -n "$(volume_for postgres19-data)" ] && grep -q 'postgres19-data' "$TMP/upgrade.out" ||
    { cat "$TMP/upgrade.out" >&2; fail 'a rollback of a finished upgrade whose old compose file was put back by hand did not keep and name the PostgreSQL 19 volume'; }
  step 'a rollback of a finished upgrade whose old compose file was put back by hand: on 17, the file as it is, the 19 volume kept'
  mv "$TMP/compose.done" "$TMP/compose.yaml"
  mv "$TMP/state.done" "$TMP/.postgres-upgrade"
  rm -f "$TMP/compose.put-back"
  dc down >/dev/null 2>&1 && dc up -d --wait >/dev/null 2>&1 || fail 'the install did not start again on PostgreSQL 19'
  [ "$(pg_major)" = 19 ] || fail 'the install is not on PostgreSQL 19 after a restart'
  same_data 'after a restart on PostgreSQL 19' on19
  rerun_installer on19
  # A rollback whose old volume was removed would serve an empty database: refused.
  cp "$TMP/.postgres-upgrade" "$TMP/state.kept"
  sed 's/^old_key=.*/old_key=postgres-gone/' "$TMP/state.kept" >"$TMP/.postgres-upgrade"
  refused 'a rollback whose old volume is gone' 'postgres-gone' --rollback
  mv "$TMP/state.kept" "$TMP/.postgres-upgrade"
  # docker compose working on another project here (COMPOSE_PROJECT_NAME in the shell or in .env)
  # would start an empty database beside the install: refused.
  script_project=$COMPOSE_PROJECT_NAME-other
  refused 'a rollback in another compose project' 'COMPOSE_PROJECT_NAME' --rollback
  script_project=''
  [ -z "$("$DOCKER" ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME-other")" ] ||
    fail 'a rollback in another compose project started containers there'
  [ "$(state state)" = 'done' ] || fail "a rollback in another compose project left the state $(state state), want done"

  # A rollback killed for good just after it recorded itself, compose.yaml still on 19.
  cat >"$TMP/docker" <<EOF
#!/bin/sh
if [ "\$*" = 'compose rm -sf server' ] && grep -qx 'state=rolling_back' .postgres-upgrade; then kill -9 \$PPID; exit 1; fi
exec "$DOCKER" "\$@"
EOF
  chmod 755 "$TMP/docker"
  if (cd "$TMP" && DOCKER="$TMP/docker" sh "$ROOT/upgrade-postgres.sh" --rollback >"$TMP/upgrade.out" 2>&1); then fail 'upgrade-postgres.sh --rollback outlived kill -9'; fi
  [ "$(state state)" = rolling_back ] || { cat "$TMP/upgrade.out" >&2; fail "a killed rollback left the state $(state state), want rolling_back"; }
  grep -qx '      - postgres19-data:/var/lib/postgresql/data' "$TMP/compose.yaml" || fail 'the rollback was killed after it changed compose.yaml'
  step 'rollback killed before compose.yaml changed: still on 19'
  # --rollback again starts it over. PostgreSQL 17 fails to start and compose.yaml.before-rollback is
  # gone, so compose.yaml cannot go back to 19: it stays on 17 with nothing started and the state
  # rolling_back, never on the old volume with the state of before the rollback.
  cat >"$TMP/docker" <<EOF
#!/bin/sh
if [ "\$*" = 'compose up -d --wait postgres' ] && grep -qx '      - postgres-data:/var/lib/postgresql/data' compose.yaml; then
  : >"$TMP/no-way-back"; rm -f compose.yaml.before-rollback; echo 'injected start failure' >&2; exit 1
fi
exec "$DOCKER" "\$@"
EOF
  if (cd "$TMP" && DOCKER="$TMP/docker" sh "$ROOT/upgrade-postgres.sh" --rollback >"$TMP/upgrade.out" 2>&1); then fail 'upgrade-postgres.sh --rollback succeeded with PostgreSQL 17 failing to start'; fi
  [ -e "$TMP/no-way-back" ] || { cat "$TMP/upgrade.out" >&2; fail 'a rollback killed before it changed compose.yaml could not be run again'; }
  no_secret_printed 'a rollback with no way back'
  [ "$(state state)" = rolling_back ] || { cat "$TMP/upgrade.out" >&2; fail "a rollback that could not put compose.yaml back left the state $(state state), want rolling_back"; }
  grep -qx '      - postgres-data:/var/lib/postgresql/data' "$TMP/compose.yaml" || fail 'a rollback that could not put compose.yaml back left it off 17'
  if server_running; then fail 'a rollback that could not put compose.yaml back started the server'; fi
  grep -q -- '--rollback again' "$TMP/upgrade.out" || { cat "$TMP/upgrade.out" >&2; fail 'a rollback that could not put compose.yaml back did not say how to go on'; }
  step 'rollback restarted, compose.yaml with no way back: stays on 17, nothing started'
  # A server started by hand on 17 since, and PGDATA moved: the resumed rollback stops the server
  # before PostgreSQL changes under it, and starts nothing on the other cluster.
  dc up -d --wait >/dev/null 2>&1 || fail 'the install did not start by hand on PostgreSQL 17 before a resumed rollback'
  cp "$TMP/compose.yaml" "$TMP/compose.on17"
  sed 's#^      PGDATA: /var/lib/postgresql/data$#      PGDATA: /var/lib/postgresql/data/moved#' "$TMP/compose.on17" >"$TMP/compose.yaml"
  if cmp -s "$TMP/compose.on17" "$TMP/compose.yaml"; then fail 'compose.yaml on 17 has no PGDATA line to move'; fi
  if upgrade --rollback; then cat "$TMP/upgrade.out" >&2; fail 'a resumed rollback onto a PostgreSQL 17 cluster that is not the old one succeeded'; fi
  grep -qF 'not the PostgreSQL 17 database of before the upgrade' "$TMP/upgrade.out" ||
    { cat "$TMP/upgrade.out" >&2; fail 'a resumed rollback onto another cluster did not name it as such'; }
  no_secret_printed 'a resumed rollback onto another cluster'
  if server_running; then fail 'a resumed rollback onto another cluster left the server running'; fi
  [ "$(state state)" = rolling_back ] || fail "a resumed rollback onto another cluster left the state $(state state), want rolling_back"
  mv "$TMP/compose.on17" "$TMP/compose.yaml"
  "$DOCKER" run --rm -v "$old_volume:/data" --entrypoint rm "$(sed -n 's/^    image: \(postgres:.*\)/\1/p' "$TMP/compose.yaml")" -rf /data/moved
  step 'a resumed rollback onto another cluster: the server stopped, nothing started'
  rolled_back
  # An upgrade in another compose project would upgrade an empty database made there: refused,
  # with the state of an earlier upgrade and without one.
  script_project=$COMPOSE_PROJECT_NAME-other
  refused 'an upgrade in another compose project' 'COMPOSE_PROJECT_NAME'
  mv "$TMP/.postgres-upgrade" "$TMP/state.kept"
  refused 'a first upgrade in another compose project' 'COMPOSE_PROJECT_NAME'
  mv "$TMP/state.kept" "$TMP/.postgres-upgrade"
  script_project=''
  [ -z "$("$DOCKER" ps -aq --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME-other")" ] &&
    [ -z "$("$DOCKER" volume ls -q --filter "label=com.docker.compose.project=$COMPOSE_PROJECT_NAME-other")" ] ||
    fail 'an upgrade in another compose project made containers or volumes there'
  step 'upgrade in another compose project: refused, nothing made there'
else
  # The installer of this checkout stops on the bundled PostgreSQL 17 database before it changes
  # compose.yaml, and says how to move it to 19.
  cp "$TMP/compose.yaml" "$TMP/compose.before-installer"
  touch "$TMP/.initialized"
  if out=$(COLLIE_DESK_IMAGE=$(env_image) sh "$ROOT/install.sh" --dir "$TMP" --name 'Upgrade Desk' --email owner@acme.example </dev/null 2>&1); then
    printf '%s\n' "$out" >&2
    fail 'the installer ran on a bundled PostgreSQL 17 database'
  fi
  printf '%s\n' "$out" | grep -qF 'the database volume holds PostgreSQL 17' && printf '%s\n' "$out" | grep -qF 'sh upgrade-postgres.sh' ||
    { printf '%s\n' "$out" >&2; fail 'the installer did not say how to move PostgreSQL 17 to 19'; }
  cmp -s "$TMP/compose.before-installer" "$TMP/compose.yaml" || fail 'the installer refusing PostgreSQL 17 changed compose.yaml'
  server_running || fail 'the installer refusing PostgreSQL 17 stopped the server'
  step 'installer on PostgreSQL 17: refused, compose.yaml unchanged'
  # The compose file the installer wrote on a rerun before Colliedesk needed 19: PostgreSQL 17 with
  # the PGDATA line.
  awk '/^  postgres:$/ { b = 1; print; next }
    b && (/^  [^ ]/ || /^[^ ]/) { b = 0 }
    b && $0 == "    environment:" { print; print "      PGDATA: /var/lib/postgresql/data"; next }
    { print }' "$TMP/compose.before-installer" >"$TMP/compose.yaml"
  rm "$TMP/compose.before-installer"
  grep -qx '      PGDATA: /var/lib/postgresql/data' "$TMP/compose.yaml" || fail 'could not add the PGDATA line to compose.yaml'
  dc up -d --wait >/dev/null 2>&1 || fail 'the install did not start with the PGDATA line'
  [ "$(pg_major)" = 17 ] && [ "$(data_volume)" = "$old_volume" ] || fail 'the PGDATA line moved PostgreSQL 17 off its data'

  # Refusals that change nothing.
  printf '#!/bin/sh\necho "Filesystem 1024-blocks Used Available Capacity Mounted on"\necho "/dev/x 1048576 1047552 1024 99%% /"\n' >"$TMP/df"
  chmod 755 "$TMP/df"
  export DF="$TMP/df"
  refused 'too little disk' 'free'
  unset DF
  printf 'state=sideways\n' >"$TMP/.postgres-upgrade"
  refused 'an unknown state file' 'sideways'
  rm "$TMP/.postgres-upgrade"
  cp "$TMP/.env" "$TMP/.env.bundled"
  sed "s#@postgres:5432/colliedesk#@db.example:5432/colliedesk#" "$TMP/.env.bundled" >"$TMP/.env"
  refused 'an external database' 'bundled postgres'
  # A keyword/value DATABASE_URL carries its password in the clear: none of it is printed.
  marker=$(secret)
  sed "s#^DATABASE_URL=.*#DATABASE_URL='host=db.example user=colliedesk password=$marker dbname=colliedesk'#" "$TMP/.env.bundled" >"$TMP/.env"
  refused 'a keyword/value DATABASE_URL' 'bundled postgres'
  mv "$TMP/.env.bundled" "$TMP/.env"
  # An environment written as a list has no place for the PGDATA line 19 needs.
  cp "$TMP/compose.yaml" "$TMP/compose.mapped"
  awk '/^  postgres:$/ { b = 1; print; next }
    b && (/^  [^ ]/ || /^[^ ]/) { b = 0 }
    b && /^    [^ ]/ { e = ($0 == "    environment:") }
    b && e && /^      PGDATA:/ { next }
    b && e && /^      [A-Za-z_][A-Za-z0-9_]*: / { sub(/: /, "="); sub(/^      /, "      - ") }
    { print }' "$TMP/compose.mapped" >"$TMP/compose.yaml"
  grep -q '^      - POSTGRES_PASSWORD=' "$TMP/compose.yaml" || fail 'could not write the postgres environment as a list'
  refused 'a postgres environment written as a list' 'PGDATA'
  mv "$TMP/compose.mapped" "$TMP/compose.yaml"

  # A failure in the middle (the restore into 19) leaves the install on 17, serving, as it was, also
  # when no message reaches the terminal (an SSH session that dropped).
  cp "$TMP/compose.yaml" "$TMP/compose.installer"
  cat >"$TMP/docker" <<EOF
#!/bin/sh
case "\$*" in *pg_restore*' --exit-on-error'*)
  if [ ! -e "$TMP/failed-once" ]; then : >"$TMP/failed-once"; echo 'injected pg_restore failure' >&2; exit 1; fi ;;
esac
exec "$DOCKER" "\$@"
EOF
  chmod 755 "$TMP/docker"
  if (cd "$TMP" && DOCKER="$TMP/docker" sh "$ROOT/upgrade-postgres.sh" >"$TMP/upgrade.out" 2>/dev/full); then fail 'upgrade-postgres.sh succeeded with a failing restore'; fi
  [ -e "$TMP/failed-once" ] || { cat "$TMP/upgrade.out" >&2; fail 'the injected restore failure never ran'; }
  no_secret_printed 'failing'
  [ "$(pg_major)" = 17 ] || fail 'a failed upgrade left the database off PostgreSQL 17'
  cmp -s "$TMP/compose.installer" "$TMP/compose.yaml" || fail 'a failed upgrade did not put compose.yaml back'
  [ "$(data_volume)" = "$old_volume" ] || fail 'a failed upgrade left PostgreSQL off its old volume'
  [ -z "$(volume_for postgres19-data)" ] || fail 'a failed upgrade left the half-made PostgreSQL 19 volume'
  [ "$(state state)" = failed ] || fail "a failed upgrade left the state $(state state), want failed"
  [ ! -e "$TMP/compose.postgres-19.yaml" ] || fail 'a failed upgrade left compose.postgres-19.yaml'
  server_running || fail 'a failed upgrade left the server stopped'
  same_data 'after a failed upgrade' before
  step 'failed upgrade: back on PostgreSQL 17, serving, nothing lost'

  # The undo of a failed restore cannot list the volumes (the Docker daemon stops answering): it keeps
  # compose.postgres-19.yaml and the state started, so the next run still knows the 19 volume as the
  # half-made one it may remove.
  # pg_restore quotes the rows and statements it fails on: they go to a file of the dump, not the
  # terminal.
  sentinel=$(secret)
  cat >"$TMP/docker" <<EOF
#!/bin/sh
case "\$*" in *pg_restore*' --exit-on-error'*) echo 'injected pg_restore failure $sentinel' >&2; exit 1 ;; esac
if [ "\$1 \$2" = 'volume ls' ] && [ -e compose.postgres-19.yaml ]; then echo 'injected volume ls failure' >&2; exit 1; fi
exec "$DOCKER" "\$@"
EOF
  if (cd "$TMP" && DOCKER="$TMP/docker" sh "$ROOT/upgrade-postgres.sh" >"$TMP/upgrade.out" 2>&1); then fail 'upgrade-postgres.sh succeeded with a failing restore'; fi
  no_secret_printed 'an undo with no volume list'
  if grep -qF "$sentinel" "$TMP/upgrade.out"; then fail 'upgrade-postgres.sh printed what pg_restore wrote to stderr'; fi
  grep -qF "$sentinel" "$TMP/$(state dump_dir)/restore.log" || { cat "$TMP/upgrade.out" >&2; fail 'the stderr of pg_restore is not in restore.log of the dump'; }
  grep -qF 'restore.log' "$TMP/upgrade.out" || fail 'a failed restore did not name restore.log'
  [ "$(state state)" = started ] || { cat "$TMP/upgrade.out" >&2; fail "an undo that could not list the volumes left the state $(state state), want started"; }
  [ -e "$TMP/compose.postgres-19.yaml" ] || fail 'an undo that could not list the volumes removed compose.postgres-19.yaml'
  [ -n "$(volume_for postgres19-data)" ] || fail 'the half-made PostgreSQL 19 volume is gone, though the undo could not list it'
  [ "$(pg_major)" = 17 ] && [ "$(data_volume)" = "$old_volume" ] && server_running ||
    fail 'an undo that could not list the volumes does not serve PostgreSQL 17 from its old volume'
  step 'undo without the volume list: compose.postgres-19.yaml kept for the next run'

  # An undo whose old volume was removed would start an empty database: refused, the server kept.
  cp "$TMP/compose.yaml" "$TMP/compose.on17"
  cp "$TMP/.postgres-upgrade" "$TMP/state.started"
  sed 's/postgres-data:/postgres-gone:/' "$TMP/compose.on17" >"$TMP/compose.yaml"
  sed 's/^old_key=.*/old_key=postgres-gone/' "$TMP/state.started" >"$TMP/.postgres-upgrade"
  refused 'an undo whose old volume is gone' 'postgres-gone of PostgreSQL 17 is gone'
  mv "$TMP/compose.on17" "$TMP/compose.yaml"
  mv "$TMP/state.started" "$TMP/.postgres-upgrade"
  gone=$(volume_for postgres-gone)
  [ -z "$gone" ] || { "$DOCKER" volume rm -f "$gone" >/dev/null 2>&1; fail 'an undo whose old volume is gone made an empty one'; }

  # --rollback after a run cut off before the switch puts the install back and upgrades nothing.
  upgrade --rollback || { cat "$TMP/upgrade.out" >&2; fail '--rollback did not put back an upgrade cut off before the switch'; }
  no_secret_printed 'a rollback of a run cut off before the switch'
  [ "$(state state)" = failed ] || fail "a rollback of a run cut off before the switch left the state $(state state), want failed"
  [ ! -e "$TMP/compose.postgres-19.yaml" ] && [ -z "$(volume_for postgres19-data)" ] ||
    fail 'a rollback of a run cut off before the switch kept the half-made PostgreSQL 19 volume'
  [ "$(pg_major)" = 17 ] && [ "$(data_volume)" = "$old_volume" ] && server_running ||
    fail 'a rollback of a run cut off before the switch does not serve PostgreSQL 17 from its old volume'
  same_data 'after a rollback of a run cut off before the switch' before
  step 'rollback of a run cut off before the switch: back on 17, the half-made volume removed'

  # Killed for good in the restore (kill -9, power loss): compose.yaml still names PostgreSQL 17 and
  # its volume, so a plain start serves the data as it was, and the next run undoes and upgrades.
  cat >"$TMP/docker" <<EOF
#!/bin/sh
case "\$*" in *pg_restore*' --exit-on-error'*) kill -9 \$PPID; exit 1 ;; esac
exec "$DOCKER" "\$@"
EOF
  chmod 755 "$TMP/docker"
  if (cd "$TMP" && DOCKER="$TMP/docker" sh "$ROOT/upgrade-postgres.sh" >"$TMP/upgrade.out" 2>&1); then fail 'upgrade-postgres.sh outlived kill -9'; fi
  [ "$(state state)" = started ] || { cat "$TMP/upgrade.out" >&2; fail "a killed upgrade left the state $(state state), want started"; }
  dc up -d --wait >/dev/null 2>&1 || fail 'a plain docker compose up did not start the install after a killed upgrade'
  [ "$(pg_major)" = 17 ] && [ "$(data_volume)" = "$old_volume" ] ||
    fail 'after an upgrade killed in the restore, a plain docker compose up does not serve PostgreSQL 17 from its old volume'
  same_data 'after a killed upgrade and a plain start' before
  step 'killed in the restore: a plain start serves PostgreSQL 17, nothing lost'

  # The run after the killed one, with PGDATA moved in compose.yaml since: PostgreSQL 17 would start
  # an empty cluster on the old volume. The undo tells it from the cluster of before the upgrade and
  # starts nothing, so the next upgrade never dumps an empty database.
  cp "$TMP/compose.yaml" "$TMP/compose.on17"
  sed 's#^      PGDATA: /var/lib/postgresql/data$#      PGDATA: /var/lib/postgresql/data/moved#' "$TMP/compose.on17" >"$TMP/compose.yaml"
  if upgrade; then cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh undid a killed upgrade onto a PostgreSQL 17 cluster that is not the one of before the upgrade'; fi
  grep -qF 'not the PostgreSQL 17 database of before the upgrade' "$TMP/upgrade.out" ||
    { cat "$TMP/upgrade.out" >&2; fail 'an undo onto another cluster did not name it as such'; }
  no_secret_printed 'an undo onto another cluster'
  if server_running; then fail 'an undo onto another cluster started the server'; fi
  [ "$(state state)" = started ] || fail "an undo onto another cluster left the state $(state state), want started"
  mv "$TMP/compose.on17" "$TMP/compose.yaml"
  "$DOCKER" run --rm -v "$old_volume:/data" --entrypoint rm "$(sed -n 's/^    image: \(postgres:.*\)/\1/p' "$TMP/compose.yaml")" -rf /data/moved
  dc up -d --wait >/dev/null 2>&1 || fail 'the install did not start on PostgreSQL 17 after an undo onto another cluster'
  step 'an undo onto another cluster: refused, nothing started'

  # The run after the killed one undoes it and upgrades, and gets a hangup (the SSH session closed)
  # while the install starts on 19: the server may have written on 19, so compose.yaml and the volume
  # stay on 19 and the next run finishes the upgrade.
  cat >"$TMP/docker" <<EOF
#!/bin/sh
if [ "\$*" = 'compose up -d --wait' ] && grep -q postgres19-data compose.yaml && [ ! -e "$TMP/hup-once" ]; then
  : >"$TMP/hup-once"; kill -HUP \$PPID
fi
exec "$DOCKER" "\$@"
EOF
  chmod 755 "$TMP/docker"
  if (cd "$TMP" && DOCKER="$TMP/docker" sh "$ROOT/upgrade-postgres.sh" >"$TMP/upgrade.out" 2>&1); then fail 'upgrade-postgres.sh ignored a hangup'; fi
  [ -e "$TMP/hup-once" ] || { cat "$TMP/upgrade.out" >&2; fail 'the hangup after the switch never ran'; }
  grep -q 'did not finish' "$TMP/upgrade.out" || { cat "$TMP/upgrade.out" >&2; fail 'the run after a killed one did not say it undid it'; }
  no_secret_printed 'hangup on 19'
  [ "$(state state)" = started ] || { cat "$TMP/upgrade.out" >&2; fail "a hangup after the switch left the state $(state state), want started"; }
  grep -qx '      - postgres19-data:/var/lib/postgresql/data' "$TMP/compose.yaml" ||
    { cat "$TMP/upgrade.out" >&2; fail 'a hangup after the switch pointed compose.yaml away from the volume the server may have written'; }
  [ -n "$(volume_for postgres19-data)" ] || fail 'a hangup after the switch removed the PostgreSQL 19 volume'
  step 'hangup after the switch: compose.yaml and the volume stay on 19'
  # compose.yaml edited to keep the data in a third volume: the run cannot tell where the server
  # wrote, so it refuses and changes nothing.
  cp "$TMP/compose.yaml" "$TMP/compose.on19"
  sed 's/postgres19-data:/postgres-other:/' "$TMP/compose.on19" >"$TMP/compose.yaml"
  refused 'a compose file naming neither volume of the upgrade' 'postgres-other'
  [ "$(state state)" = started ] || fail "a compose file naming neither volume left the state $(state state), want started"
  [ -z "$(volume_for postgres-other)" ] || fail 'a compose file naming neither volume of the upgrade got its volume made'
  mv "$TMP/compose.on19" "$TMP/compose.yaml"
  # compose.override.yaml moving PGDATA: docker compose would start an empty cluster beside the
  # restored one, so the run refuses before it starts anything.
  printf 'services:\n  postgres:\n    environment:\n      PGDATA: /var/lib/postgresql/data/moved\n' >"$TMP/compose.override.yaml"
  refused 'another compose file changing the postgres service' 'compose.override.yaml'
  [ "$(state state)" = started ] || fail "another compose file changing postgres left the state $(state state), want started"
  rm "$TMP/compose.override.yaml"
  # compose.yaml itself edited to move PGDATA: PostgreSQL 19 starts on its volume, but an empty cluster
  # made beside the restored one. The run tells the clusters apart, starts nothing and stays started.
  cp "$TMP/compose.yaml" "$TMP/compose.on19"
  sed 's#^      PGDATA: /var/lib/postgresql/data$#      PGDATA: /var/lib/postgresql/data/moved#' "$TMP/compose.on19" >"$TMP/compose.yaml"
  if upgrade; then cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh finished on a PostgreSQL 19 cluster the restore did not write'; fi
  grep -qF 'not the PostgreSQL 19 database the restore wrote' "$TMP/upgrade.out" ||
    { cat "$TMP/upgrade.out" >&2; fail 'a cluster the restore did not write was not named as such'; }
  no_secret_printed 'a cluster the restore did not write'
  if server_running; then fail 'upgrade-postgres.sh started the server on a cluster the restore did not write'; fi
  [ "$(state state)" = started ] || fail "a cluster the restore did not write left the state $(state state), want started"
  mv "$TMP/compose.on19" "$TMP/compose.yaml"
  "$DOCKER" run --rm -v "$(volume_for postgres19-data):/data" --entrypoint rm "$(sed -n 's/^    image: \(postgres:.*\)/\1/p' "$TMP/compose.yaml")" -rf /data/moved
  step 'a cluster the restore did not write: refused, nothing started'
  upgrade || { cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh did not finish an upgrade cut off by a hangup'; }
  upgraded "$TMP/compose.installer"

  # A rollback with PGDATA moved in compose.yaml: PostgreSQL 17 starts on its old volume, but an
  # empty cluster made beside the data of before the upgrade. The rollback tells the clusters apart,
  # puts compose.yaml back as it was and starts nothing, not on 17 and not on an empty cluster on 19.
  cp "$TMP/compose.yaml" "$TMP/compose.on19"
  sed 's#^      PGDATA: /var/lib/postgresql/data$#      PGDATA: /var/lib/postgresql/data/moved#' "$TMP/compose.on19" >"$TMP/compose.yaml"
  cp "$TMP/compose.yaml" "$TMP/compose.moved"
  if upgrade --rollback; then cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh --rollback went back to a PostgreSQL 17 cluster that is not the one of before the upgrade'; fi
  grep -qF 'not the PostgreSQL 17 database of before the upgrade' "$TMP/upgrade.out" ||
    { cat "$TMP/upgrade.out" >&2; fail 'a rollback to another cluster did not name it as such'; }
  no_secret_printed 'a rollback to another cluster'
  if server_running; then fail 'a rollback to another cluster started the server'; fi
  [ "$(state state)" = 'done' ] || fail "a rollback to another cluster left the state $(state state), want done"
  cmp -s "$TMP/compose.moved" "$TMP/compose.yaml" || fail 'a rollback to another cluster did not put compose.yaml back as it was'
  mv "$TMP/compose.on19" "$TMP/compose.yaml"
  rm "$TMP/compose.moved"
  for volume in "$(volume_for postgres-data)" "$(volume_for postgres19-data)"; do
    "$DOCKER" run --rm -v "$volume:/data" --entrypoint rm "$(sed -n 's/^    image: \(postgres:.*\)/\1/p' "$TMP/compose.yaml")" -rf /data/moved
  done
  dc up -d --wait >/dev/null 2>&1 || fail 'the install did not start on PostgreSQL 19 after a rollback to another cluster'
  step 'a rollback to another cluster: refused, compose.yaml as it was, nothing started'

  # A hangup while the install starts on 17 again in a rollback: the server may have written on 17,
  # so compose.yaml stays on 17 and --rollback, run again, finishes.
  cat >"$TMP/docker" <<EOF
#!/bin/sh
if [ "\$*" = 'compose up -d --wait' ] && grep -qx '      - postgres-data:/var/lib/postgresql/data' compose.yaml && [ ! -e "$TMP/hup-rollback" ]; then
  : >"$TMP/hup-rollback"; kill -HUP \$PPID
fi
exec "$DOCKER" "\$@"
EOF
  if (cd "$TMP" && DOCKER="$TMP/docker" sh "$ROOT/upgrade-postgres.sh" --rollback >"$TMP/upgrade.out" 2>&1); then fail 'upgrade-postgres.sh --rollback ignored a hangup'; fi
  [ -e "$TMP/hup-rollback" ] || { cat "$TMP/upgrade.out" >&2; fail 'the hangup in the rollback never ran'; }
  no_secret_printed 'hangup in the rollback'
  grep -qx '      - postgres-data:/var/lib/postgresql/data' "$TMP/compose.yaml" ||
    { cat "$TMP/upgrade.out" >&2; fail 'a hangup in the rollback pointed compose.yaml back at 19 after the server started on 17'; }
  [ "$(state state)" = rolling_back ] || { cat "$TMP/upgrade.out" >&2; fail "a hangup in the rollback left the state $(state state), want rolling_back"; }
  refused 'an upgrade while a rollback is unfinished' '--rollback'
  step 'hangup in the rollback: compose.yaml stays on 17'
  rolled_back gone
fi

# Both layouts end rolled back: PostgreSQL 17 on its old volume, the 19 volume kept. The install
# pulls IMAGE, which does not start on PostgreSQL 17 and names upgrade-postgres.sh, and changes
# nothing in the database.
migrations=$(echo 'SELECT count(*) FROM schema_migrations;' | psql_as "$SUPER" colliedesk -At)
sed "s#^COLLIE_DESK_IMAGE=.*#COLLIE_DESK_IMAGE=$IMAGE#" "$TMP/.env" >"$TMP/.env.next"
cat "$TMP/.env.next" >"$TMP/.env"
rm "$TMP/.env.next"
if dc up -d --wait >/dev/null 2>&1; then fail "$IMAGE started on PostgreSQL 17"; fi
dc logs --no-color server >"$TMP/server.log" 2>&1 || :
grep -qF 'PostgreSQL 19 or newer is required' "$TMP/server.log" && grep -qF 'sh upgrade-postgres.sh' "$TMP/server.log" ||
  { grep -v -i -E 'password|DATABASE_URL' "$TMP/server.log" | tail -n 20 >&2; fail "$IMAGE on PostgreSQL 17 did not say it needs 19 and how to move"; }
[ "$(pg_major)" = 17 ] || fail "$IMAGE moved the database off PostgreSQL 17"
[ "$(echo 'SELECT count(*) FROM schema_migrations;' | psql_as "$SUPER" colliedesk -At)" = "$migrations" ] ||
  fail "$IMAGE migrated the PostgreSQL 17 database it refused"
step "$IMAGE on PostgreSQL 17: refused, names upgrade-postgres.sh"
# The 17 volume as a rollback brings it back: before, plus the sign-ins of the checks since.
check snapshot "$TMP/on17" || fail 'the database fails its own check (on17)'
cp "$TMP/files.now" "$TMP/on17.files"
# The 19 volume the rollback kept holds what the server wrote on 19: the run refuses until the
# operator removes it, as docs/self-host/upgrade.md says, and then moves the install to 19.
if upgrade; then cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh upgraded over the PostgreSQL 19 volume a rollback kept'; fi
grep -qF 'postgres19-data' "$TMP/upgrade.out" || { cat "$TMP/upgrade.out" >&2; fail 'refusing the kept 19 volume, upgrade-postgres.sh did not name it'; }
no_secret_printed 'refusing the kept 19 volume'
[ "$(pg_major)" = 17 ] && [ "$(data_volume)" = "$old_volume" ] || fail 'refusing the kept 19 volume moved PostgreSQL 17 off its volume'
"$DOCKER" volume rm "$(volume_for postgres19-data)" >/dev/null
upgrade || { cat "$TMP/upgrade.out" >&2; fail "upgrade-postgres.sh did not move the install to PostgreSQL 19 for $IMAGE"; }
grep -q 'is on PostgreSQL 19' "$TMP/upgrade.out" || { cat "$TMP/upgrade.out" >&2; fail 'upgrade-postgres.sh did not say the install is on PostgreSQL 19'; }
no_secret_printed "moving to PostgreSQL 19 for $IMAGE"
[ "$(pg_major)" = 19 ] && [ "$(data_volume)" = "$(volume_for postgres19-data)" ] || fail "the install does not run PostgreSQL 19 on postgres19-data for $IMAGE"
[ "$(volume_for postgres-data)" = "$old_volume" ] || fail 'the PostgreSQL 17 volume is gone after the upgrade'
server_running || fail "$IMAGE is not running on PostgreSQL 19"
# IMAGE applied every migration of this checkout on start.
echo 'SELECT name FROM schema_migrations;' | psql_as "$SUPER" colliedesk -At | LC_ALL=C sort >"$TMP/applied"
(cd "$ROOT/server/migrations" && ls -- *.up.sql) | LC_ALL=C sort | diff - "$TMP/applied" >&2 ||
  fail "$IMAGE did not apply the migrations of this checkout on PostgreSQL 19 (above)"
check_inbox acme owner@acme.example
check_inbox beta admin@beta.example
baseline candidate
rerun_installer candidate
step "$IMAGE on PostgreSQL 19: migrated, each inbox its own, the installer keeps it"
# A rollback while .env names IMAGE: PostgreSQL 17 comes back on its old volume, IMAGE refuses it,
# and the run says which setting to change instead of only "run --rollback again". With the
# earlier release in .env, --rollback again finishes it.
grep -qF 'COLLIE_DESK_IMAGE' "$TMP/upgrade.out" ||
  { cat "$TMP/upgrade.out" >&2; fail 'moving to PostgreSQL 19, upgrade-postgres.sh did not say a rollback needs COLLIE_DESK_IMAGE set to an earlier release'; }
if upgrade --rollback; then cat "$TMP/upgrade.out" >&2; fail "a rollback with $IMAGE in .env said it finished"; fi
grep -qF 'COLLIE_DESK_IMAGE' "$TMP/upgrade.out" && grep -qF 'ghcr.io/colliedesk/colliedesk:1.0.1' "$TMP/upgrade.out" ||
  { cat "$TMP/upgrade.out" >&2; fail "a rollback with $IMAGE in .env did not name COLLIE_DESK_IMAGE and the release to set"; }
! grep -qF 'the rollback did not finish' "$TMP/upgrade.out" ||
  { cat "$TMP/upgrade.out" >&2; fail "a rollback with $IMAGE in .env also said only to run it again"; }
no_secret_printed "rolling back with $IMAGE in .env"
[ "$(pg_major)" = 17 ] && [ "$(data_volume)" = "$old_volume" ] || fail "a rollback with $IMAGE in .env left PostgreSQL 17 off its old volume"
! server_running || fail "a rollback with $IMAGE in .env left a server running that refuses PostgreSQL 17"
[ "$(state state)" = rolling_back ] || fail "a rollback with $IMAGE in .env left the state $(state state), want rolling_back"
sed "s#^COLLIE_DESK_IMAGE=.*#COLLIE_DESK_IMAGE=$PREVIOUS#" "$TMP/.env" >"$TMP/.env.next"
cat "$TMP/.env.next" >"$TMP/.env"
rm "$TMP/.env.next"
rolled_back '' on17
step "a rollback with $IMAGE in .env names COLLIE_DESK_IMAGE; with $PREVIOUS it finishes"
step 'passed'
