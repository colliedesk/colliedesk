# Upgrade testing

`tests/upgrade-test.sh PREVIOUS_IMAGE CANDIDATE_IMAGE` proves that an install of the previous release
upgrades to a candidate without losing anything, and that both recovery paths of
[the upgrade guide](../self-host/upgrade.md) work. It runs in CI on every pull request and push (`image`
job) and gates every release (`release.yml`, step "Upgrade from the previous release").

## What it does

1. Lays out an install as the previous release's `install.sh` did (its compose file and database
   users, read from `git show <previous tag>:install.sh`, or `PREVIOUS_INSTALL_SH`) running the previous
   release, with settings in `.env` (SMTP, trusted proxies) that release read from the environment. The
   compose file stays the previous one through the upgrade, as for an operator who only changes the
   image: the candidate must import those settings into its database once (an `env.import`
   audit line, not again on restart), warn that they are still in the environment, and `collie config
   doctor` must exit 1 naming them; the previous release, rolled back, reads them from the environment
   again.
2. Builds the fixture database: the previous release's migrations one at a time, as the owner with
   `app.privileged` on (what `collie migrate` does), each followed by its fixture file
   (`tests/upgrade/fixture/<migration>.sql`), then writes the stored files the rows point at.
3. Proves the fixture is the previous release's own database: its `collie migrate` has nothing to
   apply, and `pg_dump --schema-only` equals the schema the previous image makes on an empty database.
4. Snapshots it with `tests/upgrade/check.sql` and makes sure the check fails on a database with
   deleted rows and a row linked to another workspace.
5. Takes the backup exactly as the guide says (`pg_dump --format=custom` as the superuser, `tar` of
   the attachments volume).
6. Runs the candidate's `collie migrate` and compares, before the server starts (it runs retention
   and purge jobs that change rows): every table of the snapshot with the same row count and the
   same rows over the previous release's columns, the same rows per workspace, every attachment row
   with its file and checksum, no foreign key joining two workspaces, row-level security enabled and
   forced on every table with `workspace_id`, and the stored files unchanged. It also reads every
   table with `workspace_id` (and `workspaces`) as the request role in each workspace, through the
   policies themselves: all of that workspace's rows and none of another's, so a policy or grant
   that opens another workspace fails even when the flags are on. The self-test adds such a policy.
7. Starts the candidate and, for two workspaces, signs in with `collie pair`, loads the inbox, and
   checks it lists its own conversations, no conversation or contact of another workspace, and
   that another workspace's conversation answers 404.
   Then runs the checkout's `install.sh` again on the same directory, as an operator who upgrades with
   the installer: it must keep the existing install (no new workspace, no new operator link), keep the
   compose file it replaces as `compose.yaml.bak`, write one that no longer passes moved settings, import
   through `--stdin` a setting `.env` holds that the database lacks (`INTERNAL_SIGNUP_EMAILS`) while
   keeping a group the database already holds (SMTP), and serve the inbox again. The rollback that
   follows puts `compose.yaml.bak` back, as the guide says.
8. Restores one workspace with `restore-workspace.sh` after deleting some of its conversations and
   files: refused without `--replace`, then twice with it. The workspace must equal the backup as the
   script's scratch database has it, have its conversations back and no valid session, while the
   other two workspaces do not change.
9. Rolls back: the backup into a recreated database, the attachments from the tar, the previous
   image, and the snapshot of step 4 again.

`UPGRADE_TEST_RESTORE=before` runs steps 8 and 9 on the previous release before step 6, so they
need no candidate image: CI starts the test before the image build and the restores run while it
builds. The default (`after`) runs them on the upgraded install, which also restores a backup of the
previous release into the candidate's schema; releases use it.

The previous release is the highest stable `vX.Y.Z` tag below the candidate
(`.github/scripts/previous-release.sh`, read from the remote because CI checkouts have no tags).
Only that one upgrade is tested; the guide tells self-hosters to upgrade one release at a time.

## Grow the fixture with the schema

A migration that adds a table or a column needs fixture data, or
`TestUpgradeFixtureLoadsAfterEachMigrationAndFillsEveryColumn` (Go, `server/internal/database`) fails:
it applies every migration with its fixture file and requires a non-NULL value in every column.

1. Create `tests/upgrade/fixture/<the migration's name without .up.sql>.sql`.
2. Write it against the schema right after that migration: insert rows into a new table, set a new
   column on some existing rows. It runs once, after that migration, so it never needs to change
   when later migrations change the schema. Do not edit an older fixture file to fit a new schema;
   the new migration must carry the old rows forward, and that is what the test checks.
3. Use the three workspaces (`md5('ws-acme')`, `md5('ws-beta')`, `md5('ws-cobalt')`) and readable
   ids (`md5('<kind>-<workspace>-<n>')::uuid`). Put rows in more than one workspace when the table
   holds workspace data, and give links to agents, tokens or other rows, so the isolation checks
   have something to check.
4. A stored file's content follows from its row (`tests/upgrade/files.sql`): an attachment holds
   `'colliedesk upgrade fixture: ' || filename`, a cached mail image
   `'colliedesk upgrade fixture image: ' || url_hash`. A new kind of stored file needs a line there.
5. Never put customer data in the fixture.

A migration that changes existing data on purpose (drops a column, rewrites values) fails the
upgrade test until `tests/upgrade/allowed-changes.tsv` declares it: table, column (or `*`),
migration and reason. A declaration counts only in an upgrade that runs that migration; the other
columns of the table are still compared one by one. Read the migration before declaring anything:
a dropped column that held data is lost data, which the guide's backup can only bring back by
rolling the whole install back.

## Traps

- The check compares over the previous release's columns, so a new column never fails it, and a
  new table is not compared at all (only its isolation is checked).
- Rows a migration creates get new random ids each time it runs, so a restore of an old backup does
  not reproduce the upgraded database's ids. The restore test compares with the script's scratch
  database, not with the upgraded install.
- `restore-workspace.sh` turns triggers off (`session_replication_role = replica`) while it copies,
  so counters such as `workspaces.counted_storage_bytes` and `conversations.last_public_*` keep the
  backup's values instead of being counted twice; its own foreign key check replaces the skipped
  ones. Tables without a foreign key to `workspaces` (`email_provider_domain_trash`) are emptied of
  the workspace explicitly. `cloud_signups.signup_workspace_id` is cleared when a replaced workspace
  is deleted and is not restored.
- A full rollback brings back sessions as they were at the backup; the guide tells operators to
  revoke again what they revoked since. The one-workspace restore revokes them all instead.
- Local runs on an ARM Mac pull the amd64 release image and run it under emulation; it is slower
  but behaves the same.
