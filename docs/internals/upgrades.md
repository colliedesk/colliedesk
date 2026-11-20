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
columns of the table are still compared one by one. A `*` declaration also lets the table gain rows
(a migration that queues work, such as rebuilding AI retrieval), never lose them. Read the migration before declaring anything:
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

## A migration in several steps

A backfill of a large table is its own migration, between the one that adds what it fills and the one
that makes it required (conversation numbers: `20261031150000` adds nullable columns and the trigger,
`20261031150100` fills the existing rows under row locks only, `20261031150200` adds NOT NULL and the
unique key). Each file is one transaction (`database.applyMigration`), so only the short steps take the
table's ACCESS EXCLUSIVE lock; at a million conversations the fill took 18 to 70 s while reads went on.

Between the steps a row can still be unfilled. Self-host keeps the old server running while
`collie migrate` runs and starts the new one after it (docs/self-host/upgrade.md), and Colliedesk Cloud
runs `collie migrate && exec collie serve` in one container, so no new server reads the table between the
steps. The readers tolerate an unfilled row anyway (COALESCE; a code names only numbered conversations),
held by `TestAConversationNotNumberedYetReadsListsAndExports`.

## A new PostgreSQL major

`upgrade-postgres.sh` moves a bundled 16, 17 or 18 database to 19 by dump and restore into a new
volume (`postgres19-data`), never `pg_upgrade`: the upgrade only dumps from the old volume, so
`--rollback` is only a compose edit back to the old image and volume. `tests/upgrade-postgres-test.sh` runs it on an
install made by the v1.0.1 installer (PostgreSQL 17, no `PGDATA` line), for both database login
layouts, in the background of the CI `image` job and in the release.

- **Compare data only while the server is stopped.** The server writes on its own (AI indexing,
  inbound and outbox retries, snapshots, the settings import), so a snapshot of a running install
  differs from the next one for no fault of the upgrade. The test stops the server, compares,
  takes the next baseline in the same stop and starts it again; each `up --wait` costs about 3 s.
- **`compose.yaml` switches last.** PostgreSQL 19 runs from a staging copy,
  `compose.postgres-19.yaml`, until the restore and the analyze are done; only then is
  `compose.yaml` replaced, with `mv`, so it is never half written. A run killed at any point leaves a
  `compose.yaml` that serves complete data: the old major before the switch, 19 after it. The next
  run reads `compose.yaml`, not the state file, to choose between finishing on 19 and putting the
  install back, and only on a positive match: the data volume it names is either the 19 one or the
  old one. A third key (a hand edit) is refused, since the script cannot tell where the server
  wrote. After each switch of `compose.yaml` the script runs `sync`, so after a power loss the file
  on disk names the volume the server wrote, not the one before.
- **After the switch, never point away from 19 or remove its volume.** Once `compose.yaml` names 19
  the server may have written there, and the 19 volume may be the only copy of those writes. A
  failure or a signal after the switch leaves `compose.yaml` on 19 and names the two ways on (run
  again, `--rollback`). The new volume is removed only while `compose.postgres-19.yaml` still exists,
  which proves `compose.yaml` never named it.
- **The rollback is resumable.** It records `rolling_back` before it edits `compose.yaml`. Until
  PostgreSQL runs on the old volume, a stop puts `compose.yaml` back on 19; after that, the server
  may have written to the old volume, so a stop leaves `compose.yaml` on the old major and the next
  `--rollback` finishes it. An upgrade refuses while a rollback is unfinished. A rollback stopped
  before it edited `compose.yaml` (still on 19) starts over. The state of before the rollback is
  kept in `prior`, so a rollback that starts over and then fails still puts back `done` or `started`.
  If the failure leaves `compose.yaml` on the old major (its copy gone), the state stays
  `rolling_back` and nothing starts: a server on the old volume under a state that says 19 would
  write where the next run does not look.
- **One compose project.** The volumes belong to the project recorded in the state file. Docker
  Compose takes `COMPOSE_PROJECT_NAME` from the shell or `.env` over `name:`, and in another project
  it would start an empty database beside the install. A run that finds an upgrade or rollback under
  way, or done, compares `docker compose config`'s `name` with the recorded one and refuses a
  difference.
- **An undo that cannot list the volumes stops.** Without the list the half-made 19 volume cannot
  be found, so `compose.postgres-19.yaml` and the state `started` stay: the next run still knows the
  volume as one no server ran on and removes it.
- **The compose files keep their mode.** `umask 077` protects the dump; the script copies
  `compose.yaml` with `cp -p` before each edit, so the install's mode (often 664) survives.
- **No lock against two runs at once.** The script is run by hand by one operator on one host; two
  concurrent runs would also fight over the same containers, which a lock file cannot prevent across
  a reboot. Not worth the stale-lock failure mode.
- **The exit handlers ignore signals and write errors.** A closed SSH session sends SIGHUP, and then
  every write to the terminal fails; the handler that puts the install back must still run to its
  end, so it ignores HUP, INT, TERM and PIPE and its messages swallow write errors. The test runs a
  failed upgrade with stderr on `/dev/full`. The signal trap ignores further signals before it exits,
  since a Ctrl-C reaches `docker` and the script at once and a second one must not cut the handler.
  Not covered by a test: two signals within milliseconds cannot be timed reliably.
- **The installer keeps the volume key it finds.** A rerun after the upgrade must keep
  `postgres19-data`, or PostgreSQL 19 starts on the volume 17 wrote. `tests/install-test.sh` holds it.
- **No `start_interval` in the healthchecks.** It would shorten each start, but Compose refuses it on
  Docker Engine before 25, such as Debian 12's `docker.io` (20.10).
