# Upgrade a self-hosted install

An upgrade is five steps: back up, pull the new release, migrate, start and check. If something goes
wrong, restore the backup and run the release you had. Colliedesk never undoes a migration for you:
undoing one could drop data the new release wrote, so a backup is the way back.

Every release is tested this way before it is published. An install of the previous release, filled
with data shaped like real use of everything that release can hold (for example imported tickets,
attachments, tags, contacts, saved replies, several agents, a locked workspace, three workspaces),
is upgraded to the new release. The release is refused if any row, column or file is lost or changed, if a row
ends up linked to another workspace, or if the upgraded server does not serve each inbox. The backup
and both restores below are part of the same test. Details:
[upgrade testing](../internals/upgrades.md).

Releases are tested from the release just before them. If you are several releases behind, upgrade
one release at a time.

The commands below are for an install made with the [installer](README.md#with-the-installer): run them in its
directory (`./colliedesk` by default), where `compose.yaml` and `.env` are.

## 1. Back up

Note the release you run now; you need it to roll back:

```sh
docker compose run --rm --no-deps -T server '/app/collie version'
grep COLLIE_DESK_IMAGE .env
```

Stop the server, so nothing changes during the backup, then save the database and the attachment
files:

```sh
docker compose stop server
stamp=$(date -u +%Y%m%dT%H%M%SZ)
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" --format=custom colliedesk' >"colliedesk-$stamp.dump"
docker compose run --rm --no-deps -T server 'tar -C /data/attachments -cf - .' >"attachments-$stamp.tar"
```

Check that both files are complete before you go on. Both commands end without an error (`echo $?`
prints `0`) when they are:

```sh
docker compose exec -T postgres pg_restore --list <"colliedesk-$stamp.dump" >/dev/null
tar -tf "attachments-$stamp.tar" >/dev/null
```

Keep the two files somewhere other than this machine. `pg_dump` runs as the PostgreSQL superuser
(`POSTGRES_USER` in `.env`), because row-level security hides other workspaces' rows from every
other user.

## 2. Pull the new release

If `.env` has `COLLIE_DESK_IMAGE=ghcr.io/colliedesk/colliedesk:latest`, pulling is enough. If it
pins a version (`...:0.0.1`), change it to the new one first. Then:

```sh
docker compose pull server
```

Read the [release notes](https://github.com/colliedesk/colliedesk/releases) of the new release.

## 3. Migrate

```sh
docker compose run --rm --no-deps -T server '/app/collie migrate'
```

It prints `migrations applied`. If it prints an error instead, do not start the server: roll back
(below) and report the error. Each migration runs in its own transaction, so a failed upgrade can
leave the database between two releases, and only the backup brings it back.

A migration that rewrites every row of a large table takes time in proportion: numbering the existing
conversations (`20261031150100-conversation-numbers-backfill`) took about 20 to 70 seconds for a million
conversations on a laptop. While it runs, conversations stay readable; a change to one of the existing
conversations waits for it.

## 4. Start and check

```sh
docker compose up -d
docker compose ps
docker compose exec -T server /app/collie version
```

`docker compose ps` shows the server `healthy` within a minute, and `collie version` prints the new
release. If it prints the old one, `COLLIE_DESK_IMAGE` is set in your shell and wins over `.env`:
`unset COLLIE_DESK_IMAGE` and start again from step 3. Sign in, open the inbox, and open a
conversation that has an attachment. If your session has ended, print a sign-in link:

```sh
docker compose exec -T server /app/collie pair --workspace <slug> --email <your email>
```

Every `exec` and `run` here has `-T` (no terminal), so the commands also work from a script.

## PostgreSQL versions

A new install runs PostgreSQL 19, the release Colliedesk Cloud runs. PostgreSQL cannot open a data
directory that another major version wrote, so the installer reads the version of the database
volume before it writes `compose.yaml` and keeps that major:

- a volume made by PostgreSQL 16, 17 or 18 stays on it. The installer says so and changes nothing
  else. Colliedesk supports every major from 16, and every migration is checked on 16 and 17, so
  you do not need to act;
- a volume made by a version older than 16, or newer than 19, stops the installer before it changes
  `compose.yaml`;
- an install that was set up before but whose volume is empty stops the installer too: it never
  starts an empty database in place of yours. Restore the volume or the backup
  ([roll back](#roll-back)) and run it again.

The installer does not move a database to a new major. That is a dump and a restore into a new
volume, and `upgrade-postgres.sh` does it for the bundled database. An install on an
[external database](database.md) runs whatever version that server runs: upgrade that server the
way its provider says.

### Move the bundled database to PostgreSQL 19

You do not have to move: 16, 17 and 18 stay supported. To move, run `upgrade-postgres.sh` of the
newest release in the install directory. `<tag>` is that release, for example `v1.1.0`:

```sh
curl -fsSLO https://raw.githubusercontent.com/colliedesk/colliedesk/<tag>/upgrade-postgres.sh
sh upgrade-postgres.sh
```

It stops the server and dumps the roles and the database to `postgres-upgrade-<date>/`. It starts
PostgreSQL 19 from the pinned image on a new volume, `<project>_postgres19-data` (`<project>` is the
install folder's name by default), through a copy of `compose.yaml` named `compose.postgres-19.yaml`,
restores the dump there and analyzes it. Only then does it point `compose.yaml` at the new image and
volume, keeping the old file as `compose.yaml.before-postgres-19`, and start Colliedesk. Colliedesk
is down from the dump until it is healthy on 19; the script prints how long. It needs free disk space
for twice the database, and it checks that before it stops anything. Only the Colliedesk database
moves: other databases on the same server stay in the old volume, and the script names them.

- **The upgrade does not change the old volume.** It only dumps from it, so your PostgreSQL 16, 17
  or 18 data stays as it was when the server stopped.
- **A failure puts the install back.** Until the restore is complete, `compose.yaml` still names the
  old major: a failure there removes the half-made volume and starts the install on its old major,
  and even after a run that was killed, `docker compose up -d` serves the old data. If Colliedesk
  does not become healthy on 19 after the switch, the script puts `compose.yaml` back and starts the
  old major; it keeps the 19 volume then and says so: remove it before you run the script again.
- **A run that was cut off** (a reboot, a lost SSH session) is put back by the next run, or finished
  on 19 when its restore was complete.
- **It refuses what it cannot do safely** and changes nothing: an external database (a
  `DATABASE_URL` that does not name the bundled `postgres` service), a `compose.yaml` that does not
  give the postgres service one image and one data volume, an `environment:` list the `PGDATA` line
  cannot be added to, a major older than 16, an existing `postgres19-data` volume, too little disk
  space. It never prints a password.

To go back to the major and the volume you had before:

```sh
sh upgrade-postgres.sh --rollback
```

The data is then as it was when the upgrade started. Writes made on PostgreSQL 19 since then are not
in it; they stay in the volume `<project>_postgres19-data`, which is kept. Remove that volume
(`docker volume rm`) only when you no longer need it. Remove the old volume (`docker volume rm`) and
the dump directory (`rm -r`) once the install has run on 19 long enough for you to trust it. The
rollback refuses, changing nothing, when the old volume is gone.

Every pull request and every release runs this on an install of v1.0.1 (PostgreSQL 17) filled with
the upgrade fixture, for both database login layouts the installer has made: upgrade, installer
rerun, restart, a failed upgrade, a run killed in the restore, a run killed after it, one that cannot
start on 19, rollback. No row or file may change.

## Settings move into the database

From the release that keeps settings in the database (see [settings](environment-variables.md#settings)),
the first start imports the settings your `.env` still holds (SMTP, mailboxes, AI, proxies…), a group at
a time, and logs `imported settings from the environment into the database`. From then on the database
wins: a variable left in `.env` is ignored, the server logs it at every start, and `collie config doctor`
exits 1 naming it. Check, then remove those lines from `.env` once you no longer plan to go back to an
older release (it reads only `.env`):

```sh
docker compose exec -T server /app/collie config doctor
```

Running the installer again does the same before the server starts: it keeps the compose file it
replaces as `compose.yaml.bak`, writes one that passes only the variables that stay in the environment,
and imports the settings of `.env` (a group the database holds or once held is kept, so one you unset stays unset). If the import fails, it puts the older compose file back and changes nothing.

A group whose variables do not make a valid setting together is not imported, and the start log says
why (`settings in the environment were not imported`): fix it with `collie config set`.

## Roll back

If you changed settings after the upgrade (with `collie config` or the operator console), the older
release does not see them: it reads `.env` only. Before rolling back, write them out in its format and
put the lines you need in `.env`; the file holds secrets, so delete it afterwards:

```sh
docker compose exec -T server /app/collie config export --format env --include-secrets > rollback.env
```

If the installer of the new release replaced your compose file, put the older one back too (it passes
`.env` to the server): `mv compose.yaml.bak compose.yaml`.

Roll back when the migration fails or the new release does not work for you. The install goes back
to the moment of the backup: anything written since then (new conversations, replies, received
mail, changes) is gone. If you keep the new release running for a while before you decide, export
what you need first ([Export all your data](../user/export.md)).

1. Stop the server and put the release you noted back in `.env` (here `0.0.1`; edit the file by
   hand if you prefer):
   ```sh
   docker compose stop server
   sed -i.bak 's#^COLLIE_DESK_IMAGE=.*#COLLIE_DESK_IMAGE=ghcr.io/colliedesk/colliedesk:0.0.1#' .env
   grep COLLIE_DESK_IMAGE .env
   ```
2. Replace the database with the backup. Use the file names of your backup:
   ```sh
   docker compose exec -T postgres sh -c 'dropdb -U "$POSTGRES_USER" colliedesk && createdb -U "$POSTGRES_USER" -O colliedesk colliedesk'
   docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d colliedesk --exit-on-error' <colliedesk-20261020T090000Z.dump
   ```
   In installs older than the request user, `DATABASE_URL` connects as the superuser and the
   database owner is that user: use it after `-O`.
3. Replace the attachment files with the backup:
   ```sh
   docker compose run --rm --no-deps -T server 'find /data/attachments -mindepth 1 -delete && tar -C /data/attachments -xf -' <attachments-20261020T090000Z.tar
   ```
4. Check the previous release takes the database as it is, then start it:
   ```sh
   docker compose run --rm --no-deps -T server '/app/collie migrate'
   docker compose up -d
   ```
5. `.env.bak` is a copy of `.env` with its passwords: delete it once the install works.
   ```sh
   rm .env.bak
   ```

A release older than several channels of a type (two mailboxes, two web forms) does not run on a
database upgraded past it: the backup is the only way back to it, even when nobody added a second
channel yet. A second mailbox or form added since the backup is gone with everything else written
since then.

Sessions come back as they were at the backup. If you revoked a session or an agent token after the
backup, revoke it again ([Sessions and devices](../user/sessions.md)). An install rolled back to `v0.0.1`
cannot restore one workspace (below), only the whole install as above.

## Restore one workspace

To bring back one workspace and leave the others as they are (for example after a bad import or a
mistaken bulk change), use `restore-workspace.sh` of the release you are running, with a backup made
as in step 1. The script ships with every release after `v0.0.1`; on `v0.0.1`, upgrade first.
Replace `<tag>` with your release (`collie version` prints it, for example `v0.0.2`):

```sh
curl -fsSLO https://raw.githubusercontent.com/colliedesk/colliedesk/<tag>/restore-workspace.sh
docker compose stop server
sh restore-workspace.sh --workspace acme --database colliedesk-20261020T090000Z.dump \
  --attachments attachments-20261020T090000Z.tar --replace
docker compose up -d
```

Replace `acme` with the workspace slug. Each release tests its
script on its own schema and on a backup of the release before. It:

- loads the backup into a scratch database beside yours and brings it to the schema of the release
  you run, so the backup may come from an older release;
- copies every row of that workspace in one transaction, and commits only when each table matches
  the backup row for row and every link points at a row that exists;
- replaces the workspace's attachment files with the backup's and checks each one against its
  checksum;
- touches no other workspace, and removes the scratch database at the end.

Without `--replace` it refuses to overwrite a workspace that still exists. Running it again gives
the same result. The workspace's sessions, agent tokens and unused invite links come back revoked:
everyone in it signs in again (an admin prints a link with `collie pair`), and AI agents need new
tokens. An AI feature's automatic mode (suggestions applied by themselves) comes back off; an admin turns it
on again. Anything that workspace wrote after the backup is gone.

## From a clone

The repository's `compose.yaml` runs the binary directly instead of a shell. Put
`--entrypoint sh` before `server` and `-c` after it in the `docker compose run` commands above.
`restore-workspace.sh` needs the installer's layout.

### PostgreSQL 19 in a clone

The repository's `compose.yaml` runs PostgreSQL 19. PostgreSQL cannot open a data directory written
by an older major version, so a clone whose `postgres-data` volume comes from PostgreSQL 17 stops
with `database files are incompatible with server` after `git pull`. If the data in the clone is
disposable, remove the volume (`docker compose down -v`, which also removes the attachments) and
start again. To keep it, save it **before** you pull, while the old `compose.yaml` still runs 17:

```sh
docker compose stop server
docker compose exec -T postgres pg_dumpall -U colliedesk --roles-only >roles.sql
docker compose exec -T postgres pg_dump -U colliedesk --format=custom colliedesk >colliedesk.dump
docker compose exec -T postgres pg_restore --list <colliedesk.dump >/dev/null
```

`pg_dump` does not save roles, and the database grants to the roles that row-level security uses,
so `roles.sql` is needed too. Then remove only the database volume (`docker volume ls` shows its
name, `<folder>_postgres-data` by default), pull, and load both files into the new database:

```sh
docker compose down
docker volume rm colliedesk_postgres-data
git pull
docker compose up -d --wait postgres
docker compose exec -T postgres psql -U colliedesk -d postgres -q <roles.sql
docker compose exec -T postgres pg_restore -U colliedesk -d colliedesk --exit-on-error <colliedesk.dump
docker compose up -d
```

`psql` reports that the role `colliedesk` already exists; that line is expected. Any other error, or
`pg_restore` ending with an error, means the database is not complete: keep the two files and do not
delete them until the clone works. The `colliedesk_test` database is rebuilt empty by the tests.
