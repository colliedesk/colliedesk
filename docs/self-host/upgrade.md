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

## Roll back

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
tokens. Anything that workspace wrote after the backup is gone.

## From a clone

The repository's `compose.yaml` runs the binary directly instead of a shell. Put
`--entrypoint sh` before `server` and `-c` after it in the `docker compose run` commands above.
`restore-workspace.sh` needs the installer's layout.
