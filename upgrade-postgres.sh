#!/bin/sh
# Moves the bundled PostgreSQL of a self-hosted Colliedesk from 16, 17 or 18 to 19, keeping a way
# back. Run it in the install directory (compose.yaml and .env):
#   sh upgrade-postgres.sh              upgrade to PostgreSQL 19
#   sh upgrade-postgres.sh --rollback   back to the major and the data volume of before the upgrade
# PostgreSQL 19 cannot open data an older major wrote, so the data moves: the server stops, the
# roles and the database are dumped to postgres-upgrade-DATE/, PostgreSQL 19 starts on a new volume
# postgres19-data from a copy of compose.yaml (compose.postgres-19.yaml), the dump is restored there,
# and only then does that copy replace compose.yaml and the install start. Colliedesk is down from
# the dump until it is healthy on 19; the script prints how long. The upgrade does not write the old
# volume: --rollback points compose.yaml back at it.
# Until the restore is done compose.yaml names the old major, so a run cut off there (even by
# kill -9 or a power loss) leaves an install that a plain docker compose up serves as it was; the
# script undoes it, or the next run does, and removes the half-made volume. Once compose.yaml names
# 19 the restore was complete, and a run cut off is finished by the next run. The state is in
# .postgres-upgrade. See docs/self-host/upgrade.md#postgresql-versions.
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
STAGE=compose.postgres-19.yaml
ROLLBACK=false

# Messages never stop the script: a terminal that went away (a dropped SSH session) must not cut
# an undo short.
say() { printf '%s\n' "$*" 2>/dev/null || :; }
warn() { printf 'upgrade-postgres: %s\n' "$*" >&2 2>/dev/null || :; }
fail() {
  warn "$1"
  exit 1
}

usage() {
  warn 'usage: upgrade-postgres.sh [--rollback]'
  exit 2
}

while [ $# -gt 0 ]; do
  case $1 in
    --rollback) ROLLBACK=true; shift ;;
    *) usage ;;
  esac
done
[ -f compose.yaml ] && [ -f .env ] || fail 'run this in the install directory, where compose.yaml and .env are'
# The dump, the state file and the compose copies hold password hashes: only the operator reads them.
umask 077

# cf: the compose file docker compose reads; empty for compose.yaml.
cf=''
compose() {
  if [ -n "$cf" ]; then "$DOCKER" compose -f "$cf" "$@"; else "$DOCKER" compose "$@"; fi
}
# psql as the PostgreSQL superuser of the install (POSTGRES_USER of the postgres service).
query() {
  printf '%s\n' "$2" | compose exec -T postgres sh -c 'exec psql -X -q -v ON_ERROR_STOP=1 -At -U "$POSTGRES_USER" -d "$1"' psql "$1"
}
major() { query postgres 'SHOW server_version_num;' | cut -c1-2; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# The state of an upgrade, one key=value per line, rewritten whole each time.
state='' from_major='' old_image='' old_key='' project='' dump_dir='' started_at='' finished_at=''
load_state() {
  for key in state from_major old_image old_key project dump_dir started_at finished_at; do
    # shellcheck disable=SC2034 # read by the eval below
    value=$(sed -n "s/^$key=//p" "$STATE_FILE" | tail -n 1)
    eval "$key=\$value"
  done
}
save_state() {
  for key in state from_major old_image old_key project dump_dir started_at finished_at; do
    eval "printf '%s=%s\n' $key \"\$$key\""
  done >"$STATE_FILE.tmp" && mv "$STATE_FILE.tmp" "$STATE_FILE"
}

# The postgres service as compose.yaml writes it: its image and the volume key of its data.
postgres_block() {
  awk '/^  postgres:$/ { b = 1; next } b && (/^  [^ ]/ || /^[^ ]/) { b = 0 } b' compose.yaml
}
file_image() { postgres_block | sed -n 's/^    image: //p'; }
file_key() { postgres_block | sed -n "s#^      - \([A-Za-z0-9_.-]*\):$DATA\$#\1#p"; }
# Whether the postgres environment is a KEY: value mapping, where a PGDATA line can go.
env_is_map() {
  postgres_block | awk 'prev == "    environment:" { found = 1; ok = ($0 ~ /^      [A-Za-z_][A-Za-z0-9_]*: /) }
    { prev = $0 } END { exit !(found && ok) }'
}
# edit_compose IMAGE FROM_KEY TO_KEY ADD_PGDATA OUT: compose.yaml with the postgres service on IMAGE
# and its data on volume TO_KEY instead of FROM_KEY, written to OUT; ADD_PGDATA=1 adds the PGDATA
# line 18+ images need.
edit_compose() {
  awk -v image="$1" -v from="$2" -v to="$3" -v pgdata="$4" -v data="$DATA" '
    /^  postgres:$/ { b = 1; print; next }
    b && (/^  [^ ]/ || /^[^ ]/) { b = 0 }
    b && /^    image: / { print "    image: " image; next }
    b && $0 == "      - " from ":" data { print "      - " to ":" data; next }
    b && pgdata == "1" && $0 == "    environment:" { print; print "      PGDATA: " data; next }
    !b && $0 == "  " from ":" { print "  " to ":"; next }
    { print }' compose.yaml >"$5.tmp" && mv "$5.tmp" "$5"
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

# undo: the install serving on compose.yaml, which names the old major and volume. The new volume
# is removed only when compose.yaml never named it ($BACKUP is written just before the switch):
# then it served no one and holds nothing the dump lacks.
undo() {
  cf=''
  rm -f "$STAGE"
  start_all || return 1
  new=$(volume_for "$NEW_KEY")
  if [ -n "$new" ] && [ ! -f "$BACKUP" ]; then
    "$DOCKER" volume rm "$new" >/dev/null 2>&1 || return 1
    new=''
  fi
  state=failed finished_at=$(now)
  save_state || return 1
  warn "the install runs PostgreSQL $from_major again, on its old volume; the dump in $dump_dir is kept."
  if [ -n "$new" ]; then
    warn "the volume $new holds the PostgreSQL 19 database this upgrade restored and may hold writes made on 19; it is kept. Remove it (docker volume rm $new) before running this again."
  fi
}
# back_to_old: compose.yaml, which names 19, pointed back at the old major and volume, then undo.
# compose.yaml is edited rather than replaced by $BACKUP, so an installer run since keeps its changes.
back_to_old() {
  warn "putting the install back on PostgreSQL $from_major."
  edit_compose "$old_image" "$NEW_KEY" "$old_key" '' compose.yaml || return 1
  undo
}
# finish: the install serving on PostgreSQL 19 from compose.yaml, which names it; state done.
finish() {
  say 'Starting Colliedesk...'
  start_all || { warn 'the install did not become healthy on PostgreSQL 19; run docker compose logs for details'; return 1; }
  [ "$(major)" = "$NEW_MAJOR" ] && [ "$(volume_key "$(running_volume)")" = "$NEW_KEY" ] ||
    { warn "PostgreSQL $NEW_MAJOR does not run on the volume $NEW_KEY"; return 1; }
  state='done' finished_at=$(now)
  save_state || return 1
  trap - EXIT HUP INT TERM
  say "Colliedesk is on PostgreSQL $NEW_MAJOR${down_at:+ (downtime $(($(date +%s) - down_at)) s)}."
  say "The dump is in $dump_dir/, readable only by you: remove it (rm -r $dump_dir) once the install has run well for a week."
  say "The PostgreSQL $from_major data stays in the volume $(volume_for "$old_key"), not written by the upgrade. To go back to it: sh upgrade-postgres.sh --rollback"
}
# on_exit: a run that stops with the upgrade started (a failure or a signal) is undone while
# compose.yaml names the old major, and finished once it names 19. A second signal is ignored.
finishing='' down_at=''
on_exit() {
  status=$?
  set +e
  trap - EXIT
  trap '' HUP INT TERM PIPE
  if [ "$state" = started ]; then
    [ "$status" != 0 ] || status=1
    if [ "$(file_key)" = "$NEW_KEY" ]; then
      if [ -z "$finishing" ] && finish; then exit 0; fi
      back_to_old
    else
      warn "the upgrade did not finish; putting the install back on PostgreSQL $from_major."
      undo
    fi || warn "could not put the install back by itself. $BACKUP (if present) is the compose file of before the upgrade and $dump_dir the dump; see docs/self-host/upgrade.md#postgresql-versions."
  fi
  exit "$status"
}
arm() {
  trap on_exit EXIT
  trap 'exit 1' HUP INT TERM
}

if [ -f "$STATE_FILE" ]; then load_state; fi

# --rollback: compose.yaml back on the old major and volume. The current file is edited, not the copy
# restored, so an installer run since the upgrade keeps what it changed.
if [ "$ROLLBACK" = true ]; then
  [ "$state" = 'done' ] || fail "nothing to roll back: $STATE_FILE does not record a finished upgrade to PostgreSQL 19"
  [ "$(file_key)" = "$NEW_KEY" ] || fail "compose.yaml no longer keeps PostgreSQL data in $NEW_KEY, so it is not the install this upgrade left. Nothing was changed."
  [ -n "$(volume_for "$old_key")" ] ||
    fail "the volume $old_key of PostgreSQL $from_major is gone, so a rollback would start an empty database. Nothing was changed."
  new=$(volume_for "$NEW_KEY")
  say "Stopping the server and moving PostgreSQL back to $from_major..."
  cp compose.yaml compose.yaml.before-rollback
  rolled() {
    status=$?
    set +e
    trap - EXIT
    trap '' HUP INT TERM PIPE
    if [ "$status" != 0 ]; then
      mv compose.yaml.before-rollback compose.yaml
      start_all
      warn 'the rollback failed; compose.yaml is as it was, on PostgreSQL 19.'
    fi
    exit "$status"
  }
  trap rolled EXIT
  trap 'exit 1' HUP INT TERM
  compose stop server >/dev/null 2>&1
  edit_compose "$old_image" "$NEW_KEY" "$old_key" '' compose.yaml
  start_postgres || fail "PostgreSQL $from_major did not start; run docker compose logs postgres for details"
  [ "$(major)" = "$from_major" ] && [ "$(volume_key "$(running_volume)")" = "$old_key" ] ||
    fail "PostgreSQL $from_major did not come back on the volume $old_key"
  start_all || fail 'the install did not become healthy on its old database; run docker compose ps for details'
  state=rolled_back finished_at=$(now)
  save_state
  rm -f compose.yaml.before-rollback
  trap - EXIT HUP INT TERM
  say "Colliedesk is back on PostgreSQL $from_major, with the data as it was when the upgrade started ($started_at)."
  say "Writes made on PostgreSQL 19 since then are not in it: they stay in the volume ${new:-$NEW_KEY}, which is kept."
  exit 0
fi

case $state in
  '' | failed | rolled_back) ;;
  started)
    say "An upgrade started at $started_at did not finish."
    if [ "$(file_key)" = "$NEW_KEY" ]; then
      # compose.yaml is switched only after the restore: the data is on 19, so finish.
      say "The restore into PostgreSQL $NEW_MAJOR was complete; finishing it..."
      down_at='' finishing=1
      arm
      finish || exit 1
      exit 0
    fi
    say "Putting the install back on PostgreSQL $from_major first..."
    undo || fail "could not undo the upgrade started at $started_at; see docs/self-host/upgrade.md#postgresql-versions"
    ;;
  done) fail "this install was upgraded to PostgreSQL 19 at $finished_at. To go back: sh upgrade-postgres.sh --rollback" ;;
  *) fail "$STATE_FILE records the unknown state '$state'. Move it away if no upgrade is running, then run this again." ;;
esac

# Only the bundled PostgreSQL moves: DATABASE_URL must name the compose service postgres. No part of
# it is printed, since it carries the database password.
not_bundled='DATABASE_URL does not name the bundled postgres service (postgres://USER:PASSWORD@postgres:5432/DB). Only the bundled database moves: upgrade another server the way its provider says. Nothing was changed.'
url=$(compose run --rm --no-deps -T server 'printf %s "$DATABASE_URL"' 2>/dev/null) || fail 'cannot read DATABASE_URL'
case $url in postgres://* | postgresql://*) ;; *) fail "$not_bundled" ;; esac
base=${url%%\?*}
db=${base##*/}
host=${base#*://}
host=${host%%/*}
host=${host##*@}
host=${host%%:*}
[ "$host" = postgres ] || fail "$not_bundled"
case $db in '' | *[!A-Za-z0-9_]*) fail 'cannot read the database name from DATABASE_URL' ;; esac

old_image=$(file_image) old_key=$(file_key)
[ "$(printf '%s\n' "$old_image" | grep -c .)" = 1 ] && [ "$(printf '%s\n' "$old_key" | grep -c .)" = 1 ] ||
  fail "compose.yaml does not give the postgres service one image line and one volume at $DATA. Upgrade by hand: docs/self-host/upgrade.md#postgresql-versions"
pgdata=1
if postgres_block | grep '^      PGDATA:' >/dev/null; then pgdata=''; fi
[ -z "$pgdata" ] || env_is_map ||
  fail "the postgres service in compose.yaml has no environment written as KEY: value lines, so the line PGDATA: $DATA that PostgreSQL 19 needs cannot be added. Write it that way, or upgrade by hand: docs/self-host/upgrade.md#postgresql-versions. Nothing was changed."
start_postgres || fail 'PostgreSQL did not start; run docker compose ps for details'
project=$("$DOCKER" inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$(container)")
running_key=$(volume_key "$(running_volume)")
[ "$(running_image)" = "$old_image" ] && [ "$running_key" = "$old_key" ] ||
  fail "postgres runs $(running_image) on the volume $running_key, but compose.yaml names $old_image and $old_key: another compose file changes them. Upgrade by hand: docs/self-host/upgrade.md#postgresql-versions"
from_major=$(major)
case $from_major in
  "$NEW_MAJOR") say "PostgreSQL is already $NEW_MAJOR; nothing to do."; exit 0 ;;
  16 | 17 | 18) ;;
  *) fail "the database runs PostgreSQL $from_major; this script moves 16, 17 or 18 to $NEW_MAJOR" ;;
esac
existing=$(volume_for "$NEW_KEY")
[ -z "$existing" ] || fail "the volume $existing already exists. It holds the PostgreSQL 19 database of an earlier upgrade, with what was written on 19. Remove it (docker volume rm $existing) once you no longer need it, then run this again."
others=$(query postgres "SELECT string_agg(datname, ', ') FROM pg_database WHERE NOT datistemplate AND datname NOT IN ('postgres', '$db');")
[ -z "$others" ] || say "Only the database $db moves to PostgreSQL 19; $others stay in the volume of PostgreSQL $from_major."

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
rm -f "$BACKUP" "$STAGE"
state=started started_at=$(now) finished_at=''
save_state
arm

say "Stopping the server: Colliedesk is down until it is healthy on PostgreSQL $NEW_MAJOR."
down_at=$(date +%s)
compose stop server >/dev/null 2>&1
mkdir "$dump_dir"
say "Dumping the roles and the database $db (PostgreSQL $from_major) to $dump_dir/..."
compose exec -T postgres sh -c 'exec pg_dumpall -U "$POSTGRES_USER" --roles-only' >"$dump_dir/roles.sql"
compose exec -T postgres sh -c 'exec pg_dump -U "$POSTGRES_USER" --format=custom "$1"' sh "$db" >"$dump_dir/$db.dump"
compose exec -T postgres sh -c 'exec pg_restore --list' <"$dump_dir/$db.dump" >/dev/null

# PostgreSQL 19 runs from the staged copy; compose.yaml keeps naming the old major until the restore
# is done.
edit_compose "$NEW_IMAGE" "$old_key" "$NEW_KEY" "$pgdata" "$STAGE"
cf=$STAGE
say "Starting PostgreSQL $NEW_MAJOR on the new volume $NEW_KEY..."
start_postgres || fail "PostgreSQL $NEW_MAJOR did not start; run docker compose -f $STAGE logs postgres for details"
[ "$(major)" = "$NEW_MAJOR" ] && [ "$(volume_key "$(running_volume)")" = "$NEW_KEY" ] ||
  fail "PostgreSQL $NEW_MAJOR did not start on the volume $NEW_KEY"
# The image made the superuser (and the owner role of install.sh); the dump adds the others.
compose exec -T postgres sh -c 'exec psql -X -U "$POSTGRES_USER" -d postgres' <"$dump_dir/roles.sql" >"$dump_dir/roles.log" 2>&1 || :
if grep 'ERROR' "$dump_dir/roles.log" | grep -qv 'role "[^"]*" already exists'; then
  grep 'ERROR' "$dump_dir/roles.log" | grep -v 'role "[^"]*" already exists' >&2 || :
  fail 'loading the roles into PostgreSQL 19 failed (above)'
fi
say "Restoring the database $db..."
compose exec -T postgres sh -c 'exec pg_restore -U "$POSTGRES_USER" -d "$1" --exit-on-error' sh "$db" <"$dump_dir/$db.dump"
compose exec -T postgres sh -c 'exec vacuumdb -U "$POSTGRES_USER" --analyze-only --quiet "$1"' sh "$db"

# The switch: from here compose.yaml names 19, and a run cut off is finished, not undone.
cp compose.yaml "$BACKUP.tmp"
mv "$BACKUP.tmp" "$BACKUP"
mv "$STAGE" compose.yaml
cf='' finishing=1
finish || exit 1
