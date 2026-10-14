# Workspace isolation

Two layers keep one workspace's data away from another:

1. **Application filter.** Every query filters `workspace_id`, composite foreign keys tie child rows
   to their parent's workspace, and every endpoint has a cross-workspace HTTP test.
2. **Row-level security (RLS).** PostgreSQL itself hides other workspaces' rows. A query that
   forgets `WHERE workspace_id = …` still sees only the signed-in workspace. A query without a
   workspace sees nothing.

Layer 1 stays mandatory. Layer 2 catches the query that layer 1 missed.

## How the database enforces it

Migration `20261014135950-workspace-row-level-security` does this for `workspaces` (keyed by `id`)
and every table with a `workspace_id` column:

- `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY`, so the table owner is also subject
  to the policies.
- Policy `workspace_isolation`, `TO colliedesk_app`:
  `workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid)`, for reads
  and writes. An unset or empty setting compares with `NULL` and matches no row. There is no error,
  so nothing leaks through error text.
- Policy `privileged_access`, `TO` the role that ran the migration (the `DATABASE_URL` owner):
  `(SELECT current_setting('app.privileged', true) = 'on')`.
- Any other role has no policy and sees no row.

The settings sit in scalar subqueries on purpose. PostgreSQL then reads them once per query (an
InitPlan), not once per row. The app role's qual has no `OR`, so the planner can still use the
`workspace_id` indexes. Before this change, inline `current_setting()` plus an `OR` branch for the
privileged policy cost about 28 ms p95 on the inbox gate. Measured on cook-01 with four alternating
runs against main: status p95 38.6 → 40.9 ms, search p95 49.3 → 56.7 ms (budget 100 ms).

Referential integrity checks and `ON DELETE CASCADE` run as the table owner without RLS, so
composite foreign keys keep working across the scoped tables.

## Roles and connections

| Role | Who | Sees |
|---|---|---|
| `DATABASE_URL` role (owner) | Opens the pool, runs migrations | Superuser: all rows (PostgreSQL skips RLS for superusers). Other owner: only rows of a transaction marked `app.privileged`. |
| `colliedesk_app` | Every request-path transaction switches to it | Only the workspace in `app.workspace_id` |

`colliedesk_app` is `NOLOGIN NOSUPERUSER NOBYPASSRLS`. It has `SELECT, INSERT, UPDATE, DELETE` on
the schema's tables (also future tables through default privileges) and nothing on
`schema_migrations`. The migration creates the role when it is missing and grants it to the
migrating role.

Design choice: one `DATABASE_URL`, role switch per transaction. A second login role would need a
second password in every install. The installer and `compose.yaml` would change, and existing
installs would need a manual step. `SET ROLE` inside the transaction needs neither.

`colliedesk serve` calls `database.DB.Verify` at start and refuses to run in these cases:

- `colliedesk_app` is missing.
- `colliedesk_app` is a superuser or has `BYPASSRLS`.
- The `DATABASE_URL` role cannot `SET ROLE colliedesk_app`.

## The two ways to query

All queries go through `internal/database.DB` (`scope.go`):

- `InWorkspace(ctx, workspaceID, fn)` / `BeginWorkspace(ctx, workspaceID)` start a transaction,
  then run `SELECT set_config('role','colliedesk_app',true), set_config('app.workspace_id',$1,true)`.
  Both settings are transaction-local. Commit or rollback ends them, so a pooled connection never
  carries a workspace to the next request. An empty or malformed workspace id returns
  `ErrNoWorkspace` before any SQL runs.
- `Privileged(ctx, reason, fn)` runs as the owner with `app.privileged = 'on'` for that transaction.

A plain query on the pool is not workspace scoped. The store, auth, importer and email code have no
access to the pool. Their constructors wrap it in a `database.DB`.

## Privileged allowlist

Each reason is a `database.Privilege` constant in `internal/database/privileges.go`. Each privileged
transaction only resolves the workspace; the work after that runs in `InWorkspace`.

| Privilege | Call site | Why it crosses workspaces |
|---|---|---|
| `PrivilegeMigrate` | `database/migrate.go` | Schema changes and data migrations |
| `PrivilegeCreateWorkspace` | `auth/service.go` | Inserts the workspace, its first admin and pairing link (CLI `create-workspace`) |
| `PrivilegeWorkspaceBySlug` | `database/scope.go` (`WorkspaceIDBySlug`) | CLI `pair`, `import freshdesk`, `import undo` name a workspace by slug |
| `PrivilegeSessionToken` | `auth/sessions.go` | Session token hash → workspace; session checks then run scoped |
| `PrivilegeLinkToken` | `auth/links.go` | Pairing/invite link token hash → workspace; redemption then runs scoped |
| `PrivilegeCloudIdentity` | `auth/cloud.go` | Lists the workspaces where a verified cloud email is an agent |
| `PrivilegeReplyToken` | `store/email-inbound.go` | Inbound mail reply token → workspace (`MAIL_FROM_DOMAIN` routing) |
| `PrivilegeInboundSecret` | `store/email-channels.go` | Inbound email secret key id → workspace |
| `PrivilegeOutboxClaim` | `store/email-outbox.go` | The email worker leases due rows of all workspaces; each row is then loaded and finished scoped |

The Freshdesk importer and undo resolve the slug once, then run every read and write in
`InWorkspace` for that workspace. Their advisory lock (`DB.TryAdvisoryLock`) reads no table.

## Guards

- `TestEveryWorkspaceTableEnforcesRowLevelSecurity` reads the catalog. It fails when a table with a
  `workspace_id` column, or `workspaces`, lacks ENABLE, FORCE or the `workspace_isolation` policy.
  A new table must add all three in its own migration, together with `privileged_access`. Copy the
  statements from the migration above.
- `TestPrivilegedPathsMatchTheReviewedAllowlist` parses the server source. It fails when a
  `Privileged(...)` call site is added or removed, or when a new file imports `pgxpool`. To add a
  privileged path, update the test's allowlist and the table above in the same change. Explain in
  the review why the path must cross workspaces.
- `TestUnfilteredQueryInWorkspaceSeesOnlyThatWorkspace`, `TestQueryWithoutWorkspaceContextSeesNoRows`
  and `TestWorkspaceCannotWriteRowsOfAnotherWorkspace` check the policies themselves.
- `TestNonSuperuserOwnerIsAlsoFailClosed` runs migrations and queries as a non-superuser owner,
  the shape of a managed PostgreSQL. Forced RLS hides all rows from its plain queries.

## Traps

- **Tests connect as a superuser.** Raw `pool.Query` calls in tests bypass RLS on purpose; they
  seed and inspect data. Production code must never copy that pattern. The pgxpool guard catches
  a new file that tries.
- **One transaction, one workspace.** Do not call a public `Store` method from inside a scoped
  callback. It opens a second transaction on a second connection, and a small pool can deadlock.
  Use the package-level helpers that take the `pgx.Tx` (`getConversation`, `getMessage`, …).
- **Do not hold a scoped transaction across network calls** (DNS, SMTP, Freshdesk). Read, end the
  transaction, then call out. `VerifyEmailDomain` and the importer follow this.
- **Roles are cluster-wide.** Several CollieDesk databases on one PostgreSQL share
  `colliedesk_app`. The down migration removes grants and policies in its schema and keeps the role.
- **Performance.** A scoped call adds `BEGIN`, one `set_config` round trip and `COMMIT`. The inbox
  p95 gate in `performance.md` runs through this path and must stay within budget.

## Operator notes

- Upgrades need no manual step: `colliedesk migrate` (run by the compose command) creates the role
  and grants it.
- A `DATABASE_URL` role that is not a superuser and has no `CREATEROLE` cannot create
  `colliedesk_app`. The migration then stops with a hint. Run once as a superuser:
  `CREATE ROLE colliedesk_app NOLOGIN; GRANT colliedesk_app TO <database_url_role>;`, then run
  `colliedesk migrate` again.
- `privileged_access` names the role that ran the migration. If you move to another
  `DATABASE_URL` role that is not a superuser, make it the owner of the tables and recreate that
  policy for it. A superuser role needs nothing.
- Backups: `pg_dump` does not include roles. Run it as a superuser (the compose default
  `POSTGRES_USER` is one). With FORCE RLS, a non-superuser owner stops with "query would be
  affected by row-level security policy". Before you restore into a new cluster, create the role:
  `CREATE ROLE colliedesk_app NOLOGIN; GRANT colliedesk_app TO <database_url_role>;`.
