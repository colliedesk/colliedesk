#!/bin/sh
# Restores one workspace of a self-hosted Colliedesk from a backup, leaving every other workspace as
# it is. The backup is the pair docs/self-host/upgrade.md makes: a pg_dump file (custom format) and a tar
# of the attachments directory. Run it in the install directory (compose.yaml and .env), server stopped:
#   docker compose stop server
#   sh restore-workspace.sh --workspace SLUG --database colliedesk.dump --attachments attachments.tar
#   docker compose up -d
# The backup is loaded into a scratch database next to yours and brought to the schema of the image
# in .env with `collie migrate`; then the workspace's rows are copied in one transaction that checks
# every table against the backup before it commits. Sessions, agent tokens and unused links come
# back revoked: everyone in the workspace signs in again. --replace replaces a workspace that still
# exists; without it the script refuses. Running it twice with --replace gives the same result.
set -eu

DOCKER=${DOCKER:-docker}
SLUG='' DUMP='' TARBALL='' REPLACE=false KEEP=false

fail() {
  printf 'restore-workspace: %s\n' "$1" >&2
  exit 1
}

usage() {
  echo 'usage: restore-workspace.sh --workspace SLUG --database FILE.dump --attachments FILE.tar [--replace] [--keep-scratch]' >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case $1 in
    --workspace) [ $# -ge 2 ] || usage; SLUG=$2; shift 2 ;;
    --database) [ $# -ge 2 ] || usage; DUMP=$2; shift 2 ;;
    --attachments) [ $# -ge 2 ] || usage; TARBALL=$2; shift 2 ;;
    --replace) REPLACE=true; shift ;;
    --keep-scratch) KEEP=true; shift ;;
    *) usage ;;
  esac
done
[ -n "$SLUG" ] && [ -n "$DUMP" ] && [ -n "$TARBALL" ] || usage
[ -f "$DUMP" ] || fail "no database backup at $DUMP"
[ -f "$TARBALL" ] || fail "no attachments backup at $TARBALL"
[ -f compose.yaml ] || fail 'run this in the install directory, where compose.yaml is'
case $SLUG in *[!a-z0-9-]*) fail "'$SLUG' is not a workspace slug" ;; esac

compose() { "$DOCKER" compose "$@"; }
# psql as the PostgreSQL superuser of the install (POSTGRES_USER of the postgres service).
psql_super() {
  compose exec -T postgres sh -c 'exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" "$@"' psql "$@"
}
query() {
  printf '%s\n' "$2" | psql_super -At -d "$1"
}

if compose ps --status running --services 2>/dev/null | grep -qx server; then
  fail 'the server is running; stop it first: docker compose stop server'
fi
compose up -d --wait postgres >/dev/null

# The database the server uses, as compose resolves it, and a scratch database beside it.
url=$(compose run --rm --no-deps -T server 'printf %s "$DATABASE_URL"' 2>/dev/null) || fail 'cannot read DATABASE_URL'
base=${url%%\?*}
params=${url#"$base"}
live=${base##*/}
owner=$(printf '%s' "$base" | sed -n 's#^[a-z]*://\([^:@/]*\).*#\1#p')
[ -n "$live" ] && [ -n "$owner" ] || fail 'cannot read the database name and user from DATABASE_URL'
scratch=${live}_restore
scratch_url=${base%/*}/$scratch$params

work=$(mktemp -d)
cleanup() {
  status=$?
  rm -rf "$work"
  if [ "$KEEP" = false ]; then
    printf 'DROP DATABASE IF EXISTS %s;\n' "$scratch" | psql_super -d postgres >/dev/null 2>&1 || :
  fi
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

echo "Loading the backup into the scratch database $scratch..."
printf 'DROP DATABASE IF EXISTS %s;\nCREATE DATABASE %s OWNER %s;\n' "$scratch" "$scratch" "$owner" | psql_super -d postgres
compose exec -T postgres sh -c 'exec pg_restore -U "$POSTGRES_USER" -d "$1" --exit-on-error' pg_restore "$scratch" <"$DUMP" ||
  fail 'pg_restore failed; is the file a pg_dump --format=custom backup?'

echo "Bringing it to the schema of the image this install runs..."
compose run --rm --no-deps -T -e DATABASE_URL="$scratch_url" -e DATABASE_REQUEST_URL= server '/app/collie migrate' >/dev/null 2>"$work/migrate.err" ||
  fail "collie migrate failed on the backup: $(cat "$work/migrate.err")"
query "$scratch" 'SELECT name FROM schema_migrations ORDER BY 1;' >"$work/backup-migrations"
query "$live" 'SELECT name FROM schema_migrations ORDER BY 1;' >"$work/live-migrations"
newer=$(grep -vxF -f "$work/live-migrations" "$work/backup-migrations" | tr '\n' ' ' || :)
[ -z "$newer" ] || fail "the backup has migrations this install does not ($newer); install that release first"

wid=$(query "$scratch" "SELECT id FROM workspaces WHERE slug = '$SLUG';")
[ -n "$wid" ] || fail "the backup has no workspace '$SLUG'"
existing=$(query "$live" "SELECT id || ' ' || slug FROM workspaces WHERE id = '$wid' OR slug = '$SLUG';")
if [ -n "$existing" ]; then
  [ "$existing" = "$wid $SLUG" ] ||
    fail "this install has another workspace with that slug or id ($existing); rename or remove it first"
  [ "$REPLACE" = true ] || fail "workspace '$SLUG' exists; add --replace to replace it with the backup"
fi

# Credentials come back revoked, so a session or token revoked after the backup cannot sign in again.
psql_super -d "$scratch" <<SQL
UPDATE sessions SET revoked_at = now() WHERE workspace_id = '$wid' AND revoked_at IS NULL;
UPDATE pairing_links SET revoked_at = now() WHERE workspace_id = '$wid' AND used_at IS NULL AND revoked_at IS NULL;
DO \$\$
BEGIN
    IF to_regclass('mailbox_oauth_states') IS NOT NULL THEN
        DELETE FROM mailbox_oauth_states WHERE workspace_id = '$wid';
    END IF;
END
\$\$;
SQL

echo "Copying workspace $SLUG ($wid)..."
# Every table with a workspace_id column, and the workspace row itself, with their columns.
psql_super -At -F ' ' -d "$scratch" >"$work/plan" <<'SQL'
SELECT c.relname, string_agg(quote_ident(a.attname), ',' ORDER BY a.attnum)
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = 'public'
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE c.relkind IN ('r', 'p')
  AND (c.relname = 'workspaces' OR EXISTS (
      SELECT 1 FROM pg_attribute w WHERE w.attrelid = c.oid AND w.attname = 'workspace_id' AND NOT w.attisdropped))
GROUP BY c.relname
ORDER BY c.relname <> 'workspaces', c.relname;
SQL

# The script psql runs on the scratch database to print the restore transaction for the live one;
# emit prints one line of that transaction through psql's \echo.
emit() {
  printf "\\\\echo '%s'\n" "$(printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e "s/'/''/g")"
}
export_script() {
  echo '\set QUIET on'
  echo "SET TimeZone = 'UTC'; SET DateStyle = 'ISO, YMD'; SET extra_float_digits = 3;"
  emit '\set ON_ERROR_STOP on'
  emit 'BEGIN;'
  emit "SET LOCAL TimeZone = 'UTC'; SET LOCAL DateStyle = 'ISO, YMD'; SET LOCAL extra_float_digits = 3;"
  # Removing the workspace cascades to its rows; tables without a foreign key to it are emptied of it next.
  emit "DELETE FROM workspaces WHERE id = '$wid';"
  while read -r table columns; do
    [ "$table" = workspaces ] || emit "DELETE FROM $table WHERE workspace_id = '$wid';"
  done <"$work/plan"
  # Triggers (so also foreign key checks) are off while the rows go in exactly as the backup has
  # them; the checks after the copy stand in for them.
  emit 'SET LOCAL session_replication_role = replica;'
  emit 'CREATE TEMP TABLE expected (name text, columns text, n bigint, sum text) ON COMMIT DROP;'
  while read -r table columns; do
    key=workspace_id
    [ "$table" = workspaces ] && key=id
    emit "COPY $table ($columns) FROM stdin;"
    echo "COPY (SELECT $columns FROM $table WHERE $key = '$wid') TO STDOUT;"
    emit '\.'
    echo "SELECT format('INSERT INTO expected VALUES (%L, %L, %s, %L);', '$table', '$columns', count(*), md5(coalesce(string_agg(r, E'\\n' ORDER BY r), ''))) FROM (SELECT ROW($columns)::text AS r FROM $table WHERE $key = '$wid') s \\g (format=unaligned tuples_only=on)"
  done <"$work/plan"
  emit 'SET LOCAL session_replication_role = origin;'
}

verify_sql() {
  cat <<SQL
DO \$verify\$
DECLARE
    e record;
    fk record;
    n bigint;
    s text;
BEGIN
    FOR e IN SELECT * FROM expected ORDER BY name LOOP
        EXECUTE format('SELECT count(*), md5(coalesce(string_agg(r, E''\\n'' ORDER BY r), '''')) FROM (SELECT ROW(%s)::text AS r FROM %I WHERE %I = %L) s',
                       e.columns, e.name, CASE WHEN e.name = 'workspaces' THEN 'id' ELSE 'workspace_id' END, '$wid')
            INTO n, s;
        IF n <> e.n OR s <> e.sum THEN
            RAISE EXCEPTION 'restore-workspace: % has % rows after the copy and % in the backup, or different content', e.name, n, e.n;
        END IF;
        RAISE NOTICE '%', rpad(e.name, 32) || n;
    END LOOP;
    -- Every foreign key of a restored row points at a row that exists.
    FOR fk IN
        SELECT con.conname, con.conrelid::regclass AS child, con.confrelid::regclass AS parent,
               (SELECT string_agg(format('c.%I IS NOT NULL', ca.attname), ' AND ') FROM unnest(con.conkey) k(c)
                JOIN pg_attribute ca ON ca.attrelid = con.conrelid AND ca.attnum = k.c) AS present,
               (SELECT string_agg(format('p.%I = c.%I', pa.attname, ca.attname), ' AND ') FROM unnest(con.conkey, con.confkey) k(c, p)
                JOIN pg_attribute ca ON ca.attrelid = con.conrelid AND ca.attnum = k.c
                JOIN pg_attribute pa ON pa.attrelid = con.confrelid AND pa.attnum = k.p) AS joins
        FROM pg_constraint con
        WHERE con.contype = 'f' AND con.conrelid IN (SELECT e2.name::regclass FROM expected e2)
    LOOP
        EXECUTE format('SELECT count(*) FROM %s c WHERE %s = %L AND %s AND NOT EXISTS (SELECT 1 FROM %s p WHERE %s)',
                       fk.child, CASE WHEN fk.child::text = 'workspaces' THEN 'c.id' ELSE 'c.workspace_id' END, '$wid',
                       fk.present, fk.parent, fk.joins)
            INTO n;
        IF n > 0 THEN
            RAISE EXCEPTION 'restore-workspace: % rows of % break %', n, fk.child, fk.conname;
        END IF;
    END LOOP;
END
\$verify\$;
COMMIT;
SQL
}

# Written out in full first, so a failure while reading the backup never reaches the live database.
export_script >"$work/export.sql"
psql_super -d "$scratch" <"$work/export.sql" >"$work/restore.sql" || fail 'could not read the workspace from the backup'
verify_sql >>"$work/restore.sql"
psql_super -d "$live" <"$work/restore.sql" || fail 'the copy did not match the backup; nothing was changed'

echo "Restoring the workspace's files..."
compose run --rm --no-deps -T server "rm -rf '/data/attachments/$wid' &&
  { tar -C /data/attachments -xf - --wildcards '*$wid/*' 2>/tmp/tar.err || grep -q 'Not found in archive' /tmp/tar.err; }" \
  <"$TARBALL" 2>/dev/null || fail "could not extract the files of $wid from $TARBALL"
query "$live" "SELECT DISTINCT sha256 || '  /data/attachments/' || storage_key FROM attachments WHERE workspace_id = '$wid';" >"$work/sums"
if [ -s "$work/sums" ]; then
  compose run --rm --no-deps -T server 'sha256sum -c --quiet -' <"$work/sums" 2>/dev/null ||
    fail 'some attachment files are missing or differ from their rows: the rows are restored, the files are not'
fi

echo "Workspace $SLUG restored. Start the server with: docker compose up -d"
echo "Everyone in $SLUG signs in again; collie pair prints a sign-in link for an admin."
