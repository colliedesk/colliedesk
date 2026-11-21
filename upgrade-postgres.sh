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
# 19 the server may write there, so nothing points it away and the 19 volume is never removed: a run
# cut off is finished by the next run, or --rollback goes back. A rollback cut off is finished by
# --rollback again. The state is in .postgres-upgrade; a run acts only on the compose project and
# the volumes it records, and refuses a compose.yaml that names neither of them. See
# docs/self-host/upgrade.md#postgresql-versions.
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
# The dump holds password hashes: it, and every file the script makes, are for the operator only.
# The compose files keep the mode the install gave them. A write to a pipe that closed (a dropped
# session) fails instead of killing the script.
umask 077
trap '' PIPE

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
state='' prior='' too_new='' from_major='' old_image='' old_key='' old_cluster='' project='' dump_dir='' cluster='' started_at='' finished_at=''
load_state() {
  for key in state prior from_major old_image old_key old_cluster project dump_dir cluster started_at finished_at; do
    # shellcheck disable=SC2034 # read by the eval below
    value=$(sed -n "s/^$key=//p" "$STATE_FILE" | tail -n 1)
    eval "$key=\$value"
  done
}
save_state() {
  for key in state prior from_major old_image old_key old_cluster project dump_dir cluster started_at finished_at; do
    eval "printf '%s=%s\n' $key \"\$$key\""
  done >"$STATE_FILE.tmp" && mv "$STATE_FILE.tmp" "$STATE_FILE"
}

# The postgres service of a compose file read from stdin; postgres_block: as compose.yaml writes it,
# with its image and the volume key of its data.
postgres_service() { awk '/^  postgres:$/ { b = 1; next } b && (/^  [^ ]/ || /^[^ ]/) { b = 0 } b'; }
postgres_block() { postgres_service <compose.yaml; }
# The identity of the PostgreSQL cluster: initdb gives each one its own.
CLUSTER='SELECT system_identifier FROM pg_control_system();'
file_image() { postgres_block | sed -n 's/^    image: //p'; }
file_key() { postgres_block | sed -n "s#^      - \([A-Za-z0-9_.-]*\):$DATA\$#\1#p"; }
# Whether the postgres environment is a KEY: value mapping, where a PGDATA line can go.
env_is_map() {
  postgres_block | awk 'prev == "    environment:" { found = 1; ok = ($0 ~ /^      [A-Za-z_][A-Za-z0-9_]*: /) }
    { prev = $0 } END { exit !(found && ok) }'
}
# edit_compose IMAGE FROM_KEY TO_KEY ADD_PGDATA OUT: compose.yaml with the postgres service on IMAGE
# and its data on volume TO_KEY instead of FROM_KEY, written to OUT; ADD_PGDATA=1 adds the PGDATA
# line 18+ images need. OUT keeps the mode of compose.yaml.
edit_compose() {
  cp -p compose.yaml "$5.tmp" || return 1
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
# refuses_major: the server stopped because its release needs a newer PostgreSQL than this one.
refuses_major() { compose logs --no-color server 2>/dev/null | grep -q 'or newer is required'; }
EARLIER_IMAGE=ghcr.io/colliedesk/colliedesk:1.0.1
# is_cluster MAJOR KEY ID: the running PostgreSQL is MAJOR on the volume KEY, and its cluster is ID
# (pg_control_system). An edit that moves PGDATA starts an empty cluster on the same volume.
is_cluster() {
  [ -n "$3" ] && [ "$(major)" = "$1" ] && [ "$(volume_key "$(running_volume)")" = "$2" ] &&
    [ "$(query postgres "$CLUSTER")" = "$3" ]
}
# start_if MAJOR KEY ID: PostgreSQL started, then the install only on that cluster. Status 2: it
# is another cluster, so PostgreSQL is stopped and nothing runs; 1: something did not start.
start_if() {
  start_postgres || return 1
  is_cluster "$1" "$2" "$3" || { compose stop postgres >/dev/null 2>&1; return 2; }
  start_all
}

# undo: the install serving on compose.yaml, which names the old major and volume. The new volume
# is removed only while $STAGE is there: compose.yaml has never named it (the switch moves $STAGE
# to compose.yaml), so no server ran on it and it holds nothing the dump lacks. Otherwise it is
# kept, since it may hold the only copy of writes made on 19.
undo() {
  cf=''
  key=$(file_key)
  [ "$key" = "$old_key" ] || {
    warn "compose.yaml keeps the PostgreSQL data in '$key', neither $old_key (PostgreSQL $from_major) nor $NEW_KEY (PostgreSQL $NEW_MAJOR), so it is not the install this upgrade left. Nothing was changed."
    return 1
  }
  # Without its volume docker compose would start an empty database. A volume list that fails says
  # nothing here: the start below then finds no cluster of before the upgrade and starts nothing.
  if old=$(volume_for "$old_key") && [ -z "$old" ]; then
    warn "the volume $old_key of PostgreSQL $from_major is gone, so the undo would start an empty database. Nothing was changed."
    return 1
  fi
  # A server started by hand since the cut-off run stops before PostgreSQL changes under it.
  compose rm -sf server >/dev/null 2>&1
  start_if "$from_major" "$old_key" "$old_cluster" || {
    [ $? != 2 ] ||
      warn "the PostgreSQL that docker compose starts from compose.yaml is not the PostgreSQL $from_major database of before the upgrade: an edit of the postgres service (PGDATA, the volume) changes it. Nothing was started. Undo the edit, then run sh upgrade-postgres.sh again."
    return 1
  }
  # Without the list the half-made volume cannot be told apart: $STAGE and the state stay, so the
  # next run still removes it.
  new=$(volume_for "$NEW_KEY") || return 1
  if [ -n "$new" ] && [ -f "$STAGE" ]; then
    "$DOCKER" volume rm "$new" >/dev/null 2>&1 || return 1
    new=''
  fi
  rm -f "$STAGE"
  state=failed finished_at=$(now)
  save_state || return 1
  warn "the install runs PostgreSQL $from_major again, on its old volume; the dump in $dump_dir is kept."
  if [ -n "$new" ]; then
    warn "the volume $new holds the PostgreSQL 19 database of this upgrade and, if Colliedesk ran on it, the only copy of what was written there. It is kept: see docs/self-host/upgrade.md#postgresql-versions before you remove it."
  fi
}
# finish: the install serving on PostgreSQL 19 from compose.yaml, which names it; state done.
# PostgreSQL starts first, and the server only on the cluster the restore wrote: an edit that makes
# docker compose start another one (PGDATA moved, the volume replaced) stops here with nothing
# started, so no server writes to an empty database.
foreign=''
finish() {
  say 'Starting Colliedesk...'
  start_postgres || { warn "PostgreSQL $NEW_MAJOR did not start; run docker compose logs postgres for details"; return 1; }
  if ! is_cluster "$NEW_MAJOR" "$NEW_KEY" "$cluster"; then
    foreign=1
    compose stop postgres >/dev/null 2>&1
    warn "the PostgreSQL that docker compose starts from compose.yaml is not the PostgreSQL $NEW_MAJOR database the restore wrote in the volume $NEW_KEY: an edit of the postgres service (PGDATA, the volume) changes it. Nothing was started."
    return 1
  fi
  start_all || { warn 'the install did not become healthy on PostgreSQL 19; run docker compose logs for details'; return 1; }
  state='done' finished_at=$(now)
  save_state || return 1
  trap - EXIT HUP INT TERM
  say "Colliedesk is on PostgreSQL $NEW_MAJOR${down_at:+ (downtime $(($(date +%s) - down_at)) s)}."
  say "The dump is in $dump_dir/, readable only by you: remove it (rm -r $dump_dir) once the install has run well for a week."
  say "The PostgreSQL $from_major data stays in the volume $(volume_for "$old_key"), not written by the upgrade. To go back to it: sh upgrade-postgres.sh --rollback"
  say "A release newer than v1.0.1 does not start on PostgreSQL $from_major: before a rollback, set COLLIE_DESK_IMAGE in .env to the release the install ran before, for example $EARLIER_IMAGE."
}
# on_exit: a run that stops with the upgrade started (a failure or a signal) is undone while
# compose.yaml names the old major. Once it names 19 the server may have written there, so nothing
# points compose.yaml away from it: the install is started as it is, and the next run finishes the
# upgrade or --rollback goes back. A second signal is ignored.
down_at=''
on_exit() {
  status=$?
  set +e
  trap - EXIT
  trap '' HUP INT TERM PIPE
  if [ "$state" = started ]; then
    [ "$status" != 0 ] || status=1
    if [ -n "$foreign" ]; then
      warn "compose.yaml names PostgreSQL $NEW_MAJOR. Undo the edit of the postgres service first; then run sh upgrade-postgres.sh again to finish, or sh upgrade-postgres.sh --rollback to go back to PostgreSQL $from_major."
    elif [ "$(file_key)" = "$NEW_KEY" ]; then
      cf=''
      start_if "$NEW_MAJOR" "$NEW_KEY" "$cluster" || [ $? != 2 ] ||
        warn "the PostgreSQL that docker compose starts from compose.yaml is not the PostgreSQL $NEW_MAJOR database the restore wrote; nothing was started. Undo the edit of the postgres service first."
      warn "the upgrade stopped after the restore into PostgreSQL $NEW_MAJOR was complete; compose.yaml names it. Run sh upgrade-postgres.sh again to finish, or sh upgrade-postgres.sh --rollback to go back to PostgreSQL $from_major."
    else
      warn "the upgrade did not finish; putting the install back on PostgreSQL $from_major."
      undo || warn "could not put the install back by itself. compose.yaml names PostgreSQL $from_major; $dump_dir holds the dump. See docs/self-host/upgrade.md#postgresql-versions."
    fi
  fi
  exit "$status"
}
arm() {
  trap on_exit EXIT
  trap 'trap "" HUP INT TERM; exit 1' HUP INT TERM
}

if [ -f "$STATE_FILE" ]; then load_state; fi
# The volumes of an install belong to the compose project it runs in. Run in another one
# (COMPOSE_PROJECT_NAME in the shell or in .env), docker compose would start an empty database
# beside the install and upgrade that. The project is the one of the recorded upgrade, or, for a
# first one, a project that has volumes.
name=$(compose config 2>/dev/null | sed -n '1s/^name: //p')
if [ -n "$project" ]; then
  [ "$name" = "$project" ] ||
    fail "docker compose works on the project '${name:-unknown}' here, but the upgrade in $STATE_FILE ran in '$project': COMPOSE_PROJECT_NAME in the shell or in .env differs. Run it again with COMPOSE_PROJECT_NAME=$project. Nothing was changed."
else
  [ -n "$name" ] && [ -n "$("$DOCKER" volume ls -q --filter "label=com.docker.compose.project=$name")" ] ||
    fail "docker compose works on the project '${name:-unknown}' here, which has no volumes, so no install ran in it. If COMPOSE_PROJECT_NAME in the shell or in .env changed since the install, run this again with the project the install was made in. Nothing was changed."
  project=$name
fi
# The upgrade reads the postgres service from compose.yaml alone, and PostgreSQL 19 first runs from a
# copy of it. Another compose file that changes the service could make docker compose start another
# cluster than the one restored.
[ "$(compose config 2>/dev/null | postgres_service)" = "$("$DOCKER" compose -f compose.yaml config 2>/dev/null | postgres_service)" ] ||
  fail "another compose file (compose.override.yaml, or COMPOSE_FILE in the shell or in .env) changes the postgres service, and this script reads it from compose.yaml alone. Move the change into compose.yaml or remove it, then run this again. Nothing was changed."

# A finished upgrade whose compose.yaml names the old volume again: compose.yaml.before-postgres-19
# was put back by hand. A plain run says so; --rollback finishes the rollback the file already made.
put_back=''
if [ "$state" = 'done' ] && [ "$(file_key)" = "$old_key" ]; then
  [ "$ROLLBACK" = true ] ||
    fail "compose.yaml names the volume $old_key of PostgreSQL $from_major again: it was put back by hand after the upgrade to PostgreSQL 19 finished at $finished_at. Colliedesk serves the data as it was when the server stopped for the upgrade ($started_at); what was written on 19 since then stays in the volume $NEW_KEY, which is kept. Nothing was changed. To serve the PostgreSQL 19 data again, put back the compose.yaml that names $NEW_KEY; to stay on $from_major, run sh upgrade-postgres.sh --rollback."
  put_back=true
fi

# --rollback: compose.yaml back on the old major and volume. The current file is edited, not the copy
# restored, so an installer run since the upgrade keeps what it changed. While it runs the state is
# rolling_back; phase says how far it got: revert while the server has not run on the old volume
# (a stop puts compose.yaml back as it was, on 19), resume once it may have (a stop leaves
# compose.yaml on the old major, and --rollback again finishes).
if [ "$ROLLBACK" = true ]; then
  phase=''
  case $state in
    'done' | started)
      # A run cut off before the switch, or one whose old compose file was put back by hand: the
      # undo puts the install back on the old major, and nothing is upgraded again.
      if [ "$state" = started ] && [ "$(file_key)" = "$old_key" ]; then
        say "An upgrade started at $started_at did not finish; putting the install back on PostgreSQL $from_major..."
        undo || fail "could not undo the upgrade started at $started_at; see docs/self-host/upgrade.md#postgresql-versions"
        exit 0
      fi
      # compose.yaml already names the old major: the rollback only checks it and records it.
      if [ -n "$put_back" ]; then phase=resume; fi
      [ -n "$phase" ] || [ "$(file_key)" = "$NEW_KEY" ] || fail "compose.yaml no longer keeps PostgreSQL data in $NEW_KEY, so it is not the install this upgrade left. Nothing was changed."
      ;;
    rolling_back)
      # On 19 the rollback stopped before compose.yaml changed, so it starts over; on the old major
      # the server may have run there, so it finishes.
      case $(file_key) in
        "$NEW_KEY") ;;
        "$old_key") phase=resume ;;
        *) fail "compose.yaml keeps PostgreSQL data in neither $old_key nor $NEW_KEY, so it is not the install the unfinished rollback left. Nothing was changed." ;;
      esac
      ;;
    *) fail "nothing to roll back: $STATE_FILE does not record an upgrade to PostgreSQL 19" ;;
  esac
  [ -n "$(volume_for "$old_key")" ] ||
    fail "the volume $old_key of PostgreSQL $from_major is gone, so a rollback would start an empty database. Nothing was changed."
  new=$(volume_for "$NEW_KEY")
  rolled() {
    status=$?
    set +e
    trap - EXIT
    trap '' HUP INT TERM PIPE
    if [ "$status" != 0 ]; then
      cf=''
      case $phase in
        revert)
          if [ -f compose.yaml.before-rollback ]; then mv compose.yaml.before-rollback compose.yaml; fi
          if [ "$(file_key)" = "$NEW_KEY" ]; then
            state=$prior
            save_state
            start_if "$NEW_MAJOR" "$NEW_KEY" "$cluster"
            if [ $? != 2 ]; then
              warn "the rollback failed; compose.yaml is as it was, on PostgreSQL $NEW_MAJOR."
            else
              warn "the rollback failed; compose.yaml is as it was, on PostgreSQL $NEW_MAJOR, but the PostgreSQL it starts is not the database the restore wrote, so nothing was started. Undo the edit of the postgres service (PGDATA, the volume), then run docker compose up -d."
            fi
          else
            # Not back on 19: the state stays rolling_back and nothing starts, so no server runs on
            # the old volume under a state that says it is on 19.
            warn "the rollback failed and compose.yaml could not be put back: it names PostgreSQL $from_major and nothing was started. Run sh upgrade-postgres.sh --rollback again to finish it."
          fi
          ;;
        resume)
          # A server release that refuses this major was already named; starting it again would not help.
          if [ -z "$too_new" ]; then
            start_if "$from_major" "$old_key" "$old_cluster" || [ $? != 2 ] ||
              warn "the PostgreSQL that docker compose starts from compose.yaml is not the PostgreSQL $from_major database of before the upgrade; nothing was started. Undo the edit of the postgres service first."
            warn "the rollback did not finish. compose.yaml names PostgreSQL $from_major and the server may have written to the volume $old_key: run sh upgrade-postgres.sh --rollback again to finish it."
          fi
          ;;
      esac
    fi
    exit "$status"
  }
  trap rolled EXIT
  trap 'trap "" HUP INT TERM; exit 1' HUP INT TERM
  if [ -z "$phase" ]; then
    say "Stopping the server and moving PostgreSQL back to $from_major..."
    cp -p compose.yaml compose.yaml.before-rollback
    [ "$state" = rolling_back ] || prior=$state
    phase=revert state=rolling_back
    save_state
    compose rm -sf server >/dev/null 2>&1
    edit_compose "$old_image" "$NEW_KEY" "$old_key" '' compose.yaml
    sync
  elif [ -n "$put_back" ]; then
    say "compose.yaml was put back on PostgreSQL $from_major by hand; finishing the rollback on it..."
    compose rm -sf server >/dev/null 2>&1
  else
    say "A rollback did not finish; finishing it on PostgreSQL $from_major..."
    # A server started by hand since stops before PostgreSQL changes under it.
    compose rm -sf server >/dev/null 2>&1
  fi
  start_postgres || fail "PostgreSQL $from_major did not start; run docker compose logs postgres for details"
  is_cluster "$from_major" "$old_key" "$old_cluster" || {
    compose stop postgres >/dev/null 2>&1
    fail "the PostgreSQL that docker compose starts on the volume $old_key is not the PostgreSQL $from_major database of before the upgrade: an edit of the postgres service (PGDATA, the volume) changes it. Undo the edit, then run sh upgrade-postgres.sh --rollback again."
  }
  phase=resume too_new=''
  start_all || {
    if refuses_major; then
      too_new=1
      compose stop server >/dev/null 2>&1 || :
      fail "PostgreSQL $from_major is back on its old volume, but the server release in COLLIE_DESK_IMAGE needs a newer PostgreSQL, so the server is stopped. Set COLLIE_DESK_IMAGE in .env to the release the install ran before, for example $EARLIER_IMAGE, then run sh upgrade-postgres.sh --rollback again to finish it."
    fi
    fail 'the install did not become healthy on its old database; run docker compose ps for details'
  }
  state=rolled_back finished_at=$(now)
  save_state
  rm -f compose.yaml.before-rollback
  trap - EXIT HUP INT TERM
  say "Colliedesk is back on PostgreSQL $from_major, with the data as it was when the server stopped for the upgrade ($started_at)."
  say "Writes made on PostgreSQL 19 since then are not in it: they stay in the volume ${new:-$NEW_KEY}, which is kept."
  exit 0
fi

case $state in
  '' | failed | rolled_back) ;;
  started)
    say "An upgrade started at $started_at did not finish."
    if [ "$(file_key)" = "$NEW_KEY" ]; then
      # compose.yaml is switched only after the restore: the data is on 19, so finish. Without its
      # volume docker compose would start an empty database there.
      [ -n "$(volume_for "$NEW_KEY")" ] ||
        fail "compose.yaml names the volume $NEW_KEY of the restored PostgreSQL $NEW_MAJOR database, but it is gone, so finishing would start an empty database. Nothing was changed: see docs/self-host/upgrade.md#postgresql-versions."
      say "The restore into PostgreSQL $NEW_MAJOR was complete; finishing it..."
      arm
      # The server may run on another database (a compose file put back by hand): it stops before
      # PostgreSQL changes under it.
      compose rm -sf server >/dev/null 2>&1
      finish || exit 1
      exit 0
    fi
    say "Putting the install back on PostgreSQL $from_major first..."
    undo || fail "could not undo the upgrade started at $started_at; see docs/self-host/upgrade.md#postgresql-versions"
    # A 19 volume the undo kept may hold writes made on 19: no new upgrade runs over it.
    [ -z "$new" ] || exit 1
    ;;
  done) fail "this install was upgraded to PostgreSQL 19 at $finished_at. To go back: sh upgrade-postgres.sh --rollback" ;;
  rolling_back) fail "a rollback to PostgreSQL $from_major did not finish. Run sh upgrade-postgres.sh --rollback again to finish it." ;;
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
[ -n "$(volume_for "$old_key")" ] ||
  fail "the project $project has no volume $old_key, which compose.yaml names for the PostgreSQL data, so there is no database to move. Nothing was changed."
pgdata=1
if postgres_block | grep '^      PGDATA:' >/dev/null; then pgdata=''; fi
[ -z "$pgdata" ] || env_is_map ||
  fail "the postgres service in compose.yaml has no environment written as KEY: value lines, so the line PGDATA: $DATA that PostgreSQL 19 needs cannot be added. Write it that way, or upgrade by hand: docs/self-host/upgrade.md#postgresql-versions. Nothing was changed."
start_postgres || fail 'PostgreSQL did not start; run docker compose ps for details'
project=$("$DOCKER" inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$(container)")
running_key=$(volume_key "$(running_volume)")
[ "$(running_image)" = "$old_image" ] && [ "$running_key" = "$old_key" ] ||
  fail "postgres runs $(running_image) on the volume $running_key, but compose.yaml names $old_image and $old_key: another compose file changes them. Upgrade by hand: docs/self-host/upgrade.md#postgresql-versions"
from_major=$(major) old_cluster=$(query postgres "$CLUSTER")
case $from_major in
  "$NEW_MAJOR") say "PostgreSQL is already $NEW_MAJOR; nothing to do."; exit 0 ;;
  16 | 17 | 18) ;;
  *) fail "the database runs PostgreSQL $from_major; this script moves 16, 17 or 18 to $NEW_MAJOR" ;;
esac
existing=$(volume_for "$NEW_KEY")
[ -z "$existing" ] || fail "the volume $existing already exists. It holds the PostgreSQL 19 database of an earlier upgrade and, if Colliedesk ran on it, the only copy of what was written there. Nothing was changed: see docs/self-host/upgrade.md#postgresql-versions before you remove it."
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
state=started prior='' started_at=$(now) finished_at=''
save_state
arm

say "Stopping the server: Colliedesk is down until it is healthy on PostgreSQL $NEW_MAJOR."
down_at=$(date +%s)
compose rm -sf server >/dev/null 2>&1
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
# pg_restore and vacuumdb quote the rows and statements they fail on: that goes to a file of the dump,
# for the operator only, not to the terminal.
compose exec -T postgres sh -c 'exec pg_restore -U "$POSTGRES_USER" -d "$1" --exit-on-error' sh "$db" <"$dump_dir/$db.dump" 2>"$dump_dir/restore.log" ||
  fail "restoring the database $db into PostgreSQL $NEW_MAJOR failed: $dump_dir/restore.log says why"
compose exec -T postgres sh -c 'exec vacuumdb -U "$POSTGRES_USER" --analyze-only --quiet "$1"' sh "$db" 2>>"$dump_dir/restore.log" ||
  fail "analyzing the database $db on PostgreSQL $NEW_MAJOR failed: $dump_dir/restore.log says why"
# The cluster the restore wrote: the server starts only on it.
cluster=$(query postgres "$CLUSTER")
save_state

# The switch: from here compose.yaml names 19, and a run cut off is finished, not undone.
cp -p compose.yaml "$BACKUP.tmp"
mv "$BACKUP.tmp" "$BACKUP"
mv "$STAGE" compose.yaml
# On disk before anything starts on it: after a power loss compose.yaml must name the volume the
# server wrote.
sync
cf=''
finish || exit 1
