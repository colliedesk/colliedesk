#!/bin/sh
# Moves the bundled PostgreSQL of a self-hosted Colliedesk from 16, 17 or 18 to 19, keeping a way
# back. Run it in the install directory (compose.yaml and .env):
#   sh upgrade-postgres.sh              upgrade to PostgreSQL 19
#   sh upgrade-postgres.sh --rollback   back to the major and the data volume of before the upgrade
# PostgreSQL 19 cannot open data an older major wrote, so the data moves: the server stops, the
# roles and the database are dumped to postgres-upgrade-DATE/, compose.yaml is pointed at the 19
# image and a new volume postgres19-data, the dump is restored there and the install starts again.
# The server is down from the dump to the end of the restore; the script prints how long. The old
# volume is never written: --rollback points compose.yaml back at it. A failure before the server
# serves on 19 puts compose.yaml back, removes the half-made volume and starts the install on its
# old major. The state is in .postgres-upgrade; a run that was cut off is undone by the next run.
# See docs/self-host/upgrade.md#postgresql-versions.
set -eu

DOCKER=${DOCKER:-docker}
DF=${DF:-df}
# The PostgreSQL release a fresh install runs (POSTGRES_IMAGE in install.sh), pinned by digest.
NEW_IMAGE=postgres:19.0-bookworm@sha256:f4859caf96de494e38efa69ed703b08a5bbd9605e152e604f2daec8841c87c41
NEW_MAJOR=19
NEW_KEY=postgres19-data
DATA=/var/lib/postgresql/data
STATE_FILE=.postgres-upgrade
BACKUP=compose.yaml.before-postgres-19
ROLLBACK=false

fail() {
  printf 'upgrade-postgres: %s\n' "$1" >&2
  exit 1
}

usage() {
  echo 'usage: upgrade-postgres.sh [--rollback]' >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case $1 in
    --rollback) ROLLBACK=true; shift ;;
    -h | --help) usage ;;
    *) usage ;;
  esac
done
[ -f compose.yaml ] && [ -f .env ] || fail 'run this in the install directory, where compose.yaml and .env are'
# The dump, the state file and the compose copies hold password hashes: only the operator reads them.
umask 077

compose() { "$DOCKER" compose "$@"; }
# psql as the PostgreSQL superuser of the install (POSTGRES_USER of the postgres service).
query() {
  printf '%s\n' "$2" | compose exec -T postgres sh -c 'exec psql -X -q -v ON_ERROR_STOP=1 -At -U "$POSTGRES_USER" -d "$1"' psql "$1"
}
major() { query postgres 'SHOW server_version_num;' | cut -c1-2; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# The state of an upgrade, one key=value per line, rewritten whole each time.
state='' phase='' from_major='' old_image='' old_key='' project='' dump_dir='' started_at='' finished_at=''
load_state() {
  for key in state phase from_major old_image old_key project dump_dir started_at finished_at; do
    # shellcheck disable=SC2034 # read by the eval below
    value=$(sed -n "s/^$key=//p" "$STATE_FILE" | tail -n 1)
    eval "$key=\$value"
  done
}
save_state() {
  for key in state phase from_major old_image old_key project dump_dir started_at finished_at; do
    eval "printf '%s=%s\n' $key \"\$$key\""
  done >"$STATE_FILE.tmp"
  mv "$STATE_FILE.tmp" "$STATE_FILE"
}

# The postgres service as compose.yaml writes it: its image and the volume key of its data.
postgres_block() {
  awk '/^  postgres:$/ { b = 1; next } b && (/^  [^ ]/ || /^[^ ]/) { b = 0 } b' compose.yaml
}
file_image() { postgres_block | sed -n 's/^    image: //p'; }
file_key() { postgres_block | sed -n "s#^      - \([A-Za-z0-9_.-]*\):$DATA\$#\1#p"; }
# edit_compose IMAGE FROM_KEY TO_KEY ADD_PGDATA: compose.yaml with the postgres service on IMAGE and
# its data on volume TO_KEY instead of FROM_KEY; ADD_PGDATA=1 adds the PGDATA line 18+ images need.
edit_compose() {
  awk -v image="$1" -v from="$2" -v to="$3" -v pgdata="$4" -v data="$DATA" '
    /^  postgres:$/ { b = 1; print; next }
    b && (/^  [^ ]/ || /^[^ ]/) { b = 0 }
    b && /^    image: / { print "    image: " image; next }
    b && $0 == "      - " from ":" data { print "      - " to ":" data; next }
    b && pgdata == "1" && $0 == "    environment:" { print; print "      PGDATA: " data; next }
    !b && $0 == "  " from ":" { print "  " to ":"; next }
    { print }' compose.yaml >compose.yaml.tmp
  mv compose.yaml.tmp compose.yaml
}
container() { compose ps -q postgres; }
running_image() { "$DOCKER" inspect -f '{{.Config.Image}}' "$(container)"; }
running_volume() {
  "$DOCKER" inspect -f "{{range .Mounts}}{{if eq .Destination \"$DATA\"}}{{.Name}}{{end}}{{end}}" "$(container)"
}
volume_key() { "$DOCKER" volume inspect -f '{{index .Labels "com.docker.compose.volume"}}' "$1"; }
# The volume compose made for key $1 in this project, if any.
volume_for() {
  "$DOCKER" volume ls -q --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.volume=$1"
}
start_postgres() { compose up -d --wait postgres >/dev/null 2>&1; }
start_all() { compose up -d --wait >/dev/null 2>&1; }

# undo: the install back on the compose file and the volume of before this upgrade, serving.
undo() {
  compose stop server >/dev/null 2>&1 || :
  if [ -f "$BACKUP" ]; then mv "$BACKUP" compose.yaml; fi
  start_postgres || return 1
  new=$(volume_for "$NEW_KEY")
  # A volume that served no one holds nothing the dump lacks; one that served may hold writes.
  if [ -n "$new" ] && [ "$phase" = before-serving ]; then "$DOCKER" volume rm "$new" >/dev/null || return 1; fi
  start_all || return 1
  state=failed finished_at=$(now)
  save_state
  printf 'upgrade-postgres: the install runs PostgreSQL %s again, on its old volume; the dump in %s is kept.\n' "$from_major" "$dump_dir" >&2
  if [ -n "$new" ] && [ "$phase" = serving ]; then
    printf 'upgrade-postgres: the volume %s is kept: the server ran on PostgreSQL 19 and may have written to it.\n' "$new" >&2
  fi
}
on_exit() {
  status=$?
  trap - EXIT HUP INT TERM
  if [ "$state" = started ]; then
    printf 'upgrade-postgres: the upgrade did not finish; putting the install back on PostgreSQL %s.\n' "$from_major" >&2
    undo || printf 'upgrade-postgres: could not put the install back by itself. compose.yaml.before-postgres-19 is the compose file of before the upgrade and %s the dump; see docs/self-host/upgrade.md#postgresql-versions.\n' "$dump_dir" >&2
    [ "$status" != 0 ] || status=1
  fi
  exit "$status"
}

if [ -f "$STATE_FILE" ]; then load_state; fi

# --rollback: compose.yaml back on the old major and volume. The current file is edited, not the copy
# restored, so an installer run since the upgrade keeps what it changed.
if [ "$ROLLBACK" = true ]; then
  [ "$state" = 'done' ] || fail "nothing to roll back: $STATE_FILE does not record a finished upgrade to PostgreSQL 19"
  [ "$(file_key)" = "$NEW_KEY" ] || fail "compose.yaml no longer keeps PostgreSQL data in $NEW_KEY, so it is not the install this upgrade left. Nothing was changed."
  new=$(volume_for "$NEW_KEY")
  echo "Stopping the server and moving PostgreSQL back to $from_major..."
  cp compose.yaml compose.yaml.before-rollback
  rolled() {
    status=$?
    trap - EXIT HUP INT TERM
    if [ "$status" != 0 ]; then
      mv compose.yaml.before-rollback compose.yaml
      start_all || :
      printf 'upgrade-postgres: the rollback failed; compose.yaml is as it was, on PostgreSQL 19.\n' >&2
    else
      rm -f compose.yaml.before-rollback
    fi
    exit "$status"
  }
  trap rolled EXIT
  trap 'exit 1' HUP INT TERM
  compose stop server >/dev/null 2>&1
  edit_compose "$old_image" "$NEW_KEY" "$old_key" ''
  start_postgres || fail "PostgreSQL $from_major did not start; run docker compose logs postgres for details"
  [ "$(major)" = "$from_major" ] && [ "$(volume_key "$(running_volume)")" = "$old_key" ] ||
    fail "PostgreSQL $from_major did not come back on the volume $old_key"
  start_all || fail 'the install did not become healthy on its old database; run docker compose ps for details'
  state=rolled_back finished_at=$(now)
  save_state
  echo "Colliedesk is back on PostgreSQL $from_major, with the data as it was when the upgrade started ($started_at)."
  echo "Writes made on PostgreSQL 19 since then are not in it: they stay in the volume ${new:-$NEW_KEY}, which is kept."
  exit 0
fi

case $state in
  '' | failed | rolled_back) ;;
  started)
    # A run that was cut off: undo it, then start again.
    echo "An upgrade started at $started_at did not finish; putting the install back on PostgreSQL $from_major first..."
    undo || fail "could not undo the upgrade started at $started_at; see docs/self-host/upgrade.md#postgresql-versions"
    ;;
  done) fail "this install was upgraded to PostgreSQL 19 at $finished_at. To go back: sh upgrade-postgres.sh --rollback" ;;
  *) fail "$STATE_FILE records the unknown state '$state'. Move it away if no upgrade is running, then run this again." ;;
esac

# Only the bundled PostgreSQL moves: DATABASE_URL must name the compose service postgres.
url=$(compose run --rm --no-deps -T server 'printf %s "$DATABASE_URL"' 2>/dev/null) || fail 'cannot read DATABASE_URL'
base=${url%%\?*}
db=${base##*/}
host=${base#*://}
host=${host%%/*}
host=${host##*@}
host=${host%%:*}
[ "$host" = postgres ] || fail "DATABASE_URL names the database host $host, not the bundled postgres service. Upgrade that server the way its provider says; nothing was changed."
case $db in '' | *[!A-Za-z0-9_]*) fail 'cannot read the database name from DATABASE_URL' ;; esac

start_postgres || fail 'PostgreSQL did not start; run docker compose ps for details'
project=$("$DOCKER" inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$(container)")
old_image=$(file_image) old_key=$(file_key)
[ "$(printf '%s\n' "$old_image" | grep -c .)" = 1 ] && [ "$(printf '%s\n' "$old_key" | grep -c .)" = 1 ] ||
  fail "compose.yaml does not give the postgres service one image line and one volume at $DATA. Upgrade by hand: docs/self-host/upgrade.md#postgresql-versions"
running_key=$(volume_key "$(running_volume)")
[ "$(running_image)" = "$old_image" ] && [ "$running_key" = "$old_key" ] ||
  fail "postgres runs $(running_image) on the volume $running_key, but compose.yaml names $old_image and $old_key: another compose file changes them. Upgrade by hand: docs/self-host/upgrade.md#postgresql-versions"
from_major=$(major)
case $from_major in
  "$NEW_MAJOR") echo "PostgreSQL is already $NEW_MAJOR; nothing to do."; exit 0 ;;
  16 | 17 | 18) ;;
  *) fail "the database runs PostgreSQL $from_major; this script moves 16, 17 or 18 to $NEW_MAJOR" ;;
esac
existing=$(volume_for "$NEW_KEY")
[ -z "$existing" ] || fail "the volume $existing already exists. It holds the PostgreSQL 19 database of an earlier upgrade, with what was written on 19. Remove it (docker volume rm $existing) once you no longer need it, then run this again."
others=$(query postgres "SELECT string_agg(datname, ', ') FROM pg_database WHERE NOT datistemplate AND datname NOT IN ('postgres', '$db');")
[ -z "$others" ] || echo "Only the database $db moves to PostgreSQL 19; $others stay in the volume of PostgreSQL $from_major."

# Room for the dump next to compose.yaml and for the new volume: twice the databases, and 256 MiB.
size=$(query postgres 'SELECT sum(pg_database_size(datname))::bigint FROM pg_database;')
need=$((size / 1048576 * 2 + 256))
for dir in . "$("$DOCKER" info -f '{{.DockerRootDir}}' 2>/dev/null || :)"; do
  [ -n "$dir" ] && [ -d "$dir" ] || continue
  free=$("$DF" -Pk "$dir" 2>/dev/null | awk 'NR == 2 { print int($4 / 1024) }')
  [ -n "$free" ] || continue
  [ "$free" -ge "$need" ] ||
    fail "$(cd "$dir" && pwd) has $free MiB free; the upgrade needs $need MiB (twice the database, $((size / 1048576)) MiB, and 256 MiB). Nothing was changed."
done

dump_dir=postgres-upgrade-$(date -u +%Y%m%d-%H%M%S)
rm -f "$BACKUP"
state=started phase=before-serving started_at=$(now) finished_at=''
save_state
trap on_exit EXIT
trap 'exit 1' HUP INT TERM

echo "Stopping the server: Colliedesk is down until PostgreSQL $NEW_MAJOR serves its data."
down_at=$(date +%s)
compose stop server >/dev/null 2>&1
mkdir "$dump_dir"
echo "Dumping the roles and the database $db (PostgreSQL $from_major) to $dump_dir/..."
compose exec -T postgres sh -c 'exec pg_dumpall -U "$POSTGRES_USER" --roles-only' >"$dump_dir/roles.sql"
compose exec -T postgres sh -c 'exec pg_dump -U "$POSTGRES_USER" --format=custom "$1"' sh "$db" >"$dump_dir/$db.dump"
compose exec -T postgres sh -c 'exec pg_restore --list' <"$dump_dir/$db.dump" >/dev/null

cp compose.yaml "$BACKUP"
pgdata=1
if postgres_block | grep -q '^      PGDATA:'; then pgdata=''; fi
edit_compose "$NEW_IMAGE" "$old_key" "$NEW_KEY" "$pgdata"
echo "Starting PostgreSQL $NEW_MAJOR on the new volume $NEW_KEY..."
start_postgres
[ "$(major)" = "$NEW_MAJOR" ] && [ "$(volume_key "$(running_volume)")" = "$NEW_KEY" ] ||
  fail "PostgreSQL $NEW_MAJOR did not start on the volume $NEW_KEY"
# The image made the superuser (and the owner role of install.sh); the dump adds the others.
compose exec -T postgres sh -c 'exec psql -X -U "$POSTGRES_USER" -d postgres' <"$dump_dir/roles.sql" >"$dump_dir/roles.log" 2>&1 || :
if grep 'ERROR' "$dump_dir/roles.log" | grep -qv 'role "[^"]*" already exists'; then
  grep 'ERROR' "$dump_dir/roles.log" | grep -v 'role "[^"]*" already exists' >&2
  fail 'loading the roles into PostgreSQL 19 failed (above)'
fi
echo "Restoring the database $db..."
compose exec -T postgres sh -c 'exec pg_restore -U "$POSTGRES_USER" -d "$1" --exit-on-error' sh "$db" <"$dump_dir/$db.dump"
compose exec -T postgres sh -c 'exec vacuumdb -U "$POSTGRES_USER" --analyze-only --quiet "$1"' sh "$db"

phase=serving
save_state
echo 'Starting Colliedesk...'
start_all || fail 'the install did not become healthy on PostgreSQL 19; run docker compose logs for details'
state='done' finished_at=$(now)
save_state
trap - EXIT HUP INT TERM

echo "Colliedesk is on PostgreSQL $NEW_MAJOR (downtime $(($(date +%s) - down_at)) s)."
echo "The dump is in $dump_dir/, readable only by you: delete it once the install has run well for a week."
echo "The PostgreSQL $from_major data stays in the volume $(volume_for "$old_key"), untouched. To go back to it: sh upgrade-postgres.sh --rollback"
