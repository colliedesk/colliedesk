# Workspace isolation

Two layers keep one workspace's data away from another:

1. **Application filter.** Every query filters `workspace_id`, composite foreign keys tie child rows
   to their parent's workspace, and every endpoint has a cross-workspace HTTP test.
2. **Row-level security (RLS).** PostgreSQL itself hides other workspaces' rows. A query that
   forgets `WHERE workspace_id = …` still sees only the signed-in workspace. A query without a
   workspace sees nothing.

Layer 1 stays mandatory. Layer 2 catches the query that layer 1 missed.

## What RLS does not protect against

RLS here is a defence against a **forgotten filter**, not against **SQL injection**.

- **Single-login mode** (only `DATABASE_URL` is set; every install created before the request
  login). The request connection's session user is the owner,
  and that is a superuser in the default compose setup. Injected SQL can run `RESET ROLE` or
  `set_config('role', '<owner>', true)` and leave the app role. It then sees every workspace.
  With a non-superuser owner it can still set `app.privileged` itself. The test
  `TestRequestLoginCannotLeaveTheWorkspaceScope` pins this behaviour.
- **Request-login mode** (`DATABASE_REQUEST_URL` is also set; fresh installs from `install.sh`).
  Requests connect as a separate login that holds the app role with `INHERIT FALSE`. After
  `RESET ROLE` it has no rights on any table. It cannot become the owner. The same test proves
  "permission denied" for `RESET ROLE`, for switching to the owner, and for setting
  `app.privileged`.
- In both modes, injected SQL inside a scoped transaction can still set `app.workspace_id` to
  another workspace's id. RLS cannot stop that, because the request path sets this value itself.
  Ids are random UUIDs, and the policy never reveals another workspace's id. Parameterized queries
  remain the defence against injection.

## How the database enforces it

Migration `20261014135950-workspace-row-level-security` does this for `workspaces` (keyed by `id`)
and every table with a `workspace_id` column:

- `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY`, so the table owner is also subject
  to the policies.
- Policy `workspace_isolation`, `TO` the app role:
  `workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid)`, for reads
  and writes. An unset or empty setting compares with `NULL` and matches no row. There is no error,
  so nothing leaks through error text.
- Policy `privileged_access`, `TO` the role that ran the migration (the `DATABASE_URL` owner):
  `(SELECT current_setting('app.privileged', true) = 'on')`.
- Any other role has no policy and sees no row.

The settings sit in scalar subqueries on purpose. PostgreSQL then reads them once per query (an
InitPlan), not once per row. The app role's qual has no `OR`, so the planner can still use the
`workspace_id` indexes. Before this change, inline `current_setting()` plus an `OR` branch for the
privileged policy cost about 28 ms p95 on the inbox gate. See `performance.md` for the gate.

A table added after that migration repeats the same two policies and the app role grant in its own
migration (`workspace_billing` and `billing_webhook_events` do; see `billing.md`). Skipping this leaves
the app role without access, and every request on that table fails.

Referential integrity checks and `ON DELETE CASCADE` run as the table owner without RLS, so
composite foreign keys keep working across the scoped tables.

## Roles and connections

| Role | Who | Sees |
|---|---|---|
| `DATABASE_URL` role (owner) | Runs migrations, privileged lookups, the advisory lock, `ANALYZE` through `DB.RefreshStatistics` before an import undo (owner-pool maintenance: statistics only, no rows returned; `pg_stats` hides RLS tables from the app role) | Superuser: all rows (PostgreSQL skips RLS for superusers). Other owner: only rows of a transaction marked `app.privileged`. |
| `colliedesk_app_<database>` (app role) | Every workspace-scoped transaction switches to it | Only the workspace in `app.workspace_id` |
| `DATABASE_REQUEST_URL` login (optional, `colliedesk_request` in installs) | Opens the request pool | Nothing by itself; it can only `SET ROLE` to the app role |

- **App role.** `NOLOGIN NOSUPERUSER NOBYPASSRLS`, one per database. The name is
  `colliedesk_app_` plus the database name. Past 63 bytes (bytes, not characters) it is cut on a
  character boundary and gets an md5 suffix of the database name
  (`TestLongNonASCIIDatabaseNamesGetDistinctAppRolesWithinSixtyThreeBytes`). Two
  installs on one cluster therefore never share a role, and the owner of one cannot act in the
  other's database (`TestAppRoleOfOneDatabaseCannotReachAnother`). It has
  `SELECT, INSERT, UPDATE, DELETE` on the schema's tables, also future tables through default
  privileges, and nothing on `schema_migrations`. The migration creates the role and grants it to
  the owner. The server reads the name back from the `workspace_isolation` policy, so the
  migration is the only place that defines it.
- **Request login.** `collie migrate` creates it when `DATABASE_REQUEST_URL` is set
  (`database.EnsureRequestLogin`). The user and password come from that URL. Go computes the
  SCRAM-SHA-256 verifier (4096 iterations, random 16-byte salt). Only the verifier is sent in
  `CREATE ROLE`, so server logs, `log_statement` and `pg_stat_statements` never see the plaintext
  (`TestEnsureRequestLoginNeverSendsThePlaintextPassword`). The password must be printable ASCII,
  because the verifier skips SASLprep; generated passwords are hex. It is granted the app
  role `WITH INHERIT FALSE, SET TRUE`. The owner needs `CREATEROLE` for this. The installer's owner
  has it; PostgreSQL 16+ limits it to roles that the owner created.
- **Fresh installs** (`install.sh`):
  1. The `postgres` superuser is used only by the image to create the database.
  2. `initdb/10-colliedesk-owner.sh` runs once, when the volume is created. It creates the
     non-superuser owner `colliedesk` (`LOGIN CREATEROLE`) and hands it the database.
  3. `.env` holds `DATABASE_URL` (the owner) and `DATABASE_REQUEST_URL` (`colliedesk_request`).
- **The repository's `compose.yaml`** (development, "From a clone") keeps the superuser owner,
  because the tests need it, and sets `DATABASE_REQUEST_URL` so that requests run in
  request-login mode.
- **Existing installs** keep upgrading with no manual step. Their `.env` has neither variable, so
  `compose.yaml` falls back to the old superuser `DATABASE_URL`, and they run in single-login mode.

`collie serve` calls `database.DB.Verify` at start and refuses to run in these cases:

- The app role is missing, is a superuser or has `BYPASSRLS`.
- The request connection cannot `SET ROLE` to the app role.
- With a separate request login: that login is a superuser, has `BYPASSRLS` or is a member of the
  owner.

## The two ways to query

All queries go through `internal/database.DB` (`scope.go`):

- `InWorkspace(ctx, workspaceID, fn)` / `BeginWorkspace(ctx, workspaceID)` start a transaction on
  the request pool, then run
  `SELECT set_config('role',<app role>,true), set_config('app.workspace_id',$1,true)`.
  - Both settings are transaction-local. Commit or rollback ends them, so a pooled connection
    never carries a workspace to the next request.
  - An empty or malformed workspace id returns `ErrNoWorkspace` before any SQL runs.
  - The rollback is deferred, so a panicking callback also returns its connection and releases its
    row and advisory locks (`TestPanickingCallbackReleasesConnectionAndLocks`).
- `Privileged(ctx, reason, fn)` runs on the owner pool with `app.privileged = 'on'` for that
  transaction.

`database.New(pool)` uses one pool for both. `database.NewSplit(owner, request)` is the
request-login mode. `httpapi.Config.RequestPool` and `store.NewWithDB` / `auth.NewWithDB` carry it.
The store, auth, importer and email code never touch a pool.

## Privileged allowlist

Each reason is a `database.Privilege` constant in `internal/database/privileges.go`. Each privileged
transaction only resolves the workspace; the work after that runs in `InWorkspace`.

| Privilege | Call site | Why it crosses workspaces |
|---|---|---|
| `PrivilegeMigrate` | `database/migrate.go` | Schema changes and data migrations |
| `PrivilegeRequestLogin` | `database/scope.go` (`EnsureRequestLogin`) | Creates the request login and grants it the app role (`collie migrate`) |
| `PrivilegeCreateWorkspace` | `auth/service.go`, `auth/signup.go` | Inserts the workspace, its first admin and pairing link (CLI `create-workspace`). `signup.go` (`POST /auth/cloud/signup`) also reads and writes `cloud_signups`, the platform-level table of trials already given: it has no `workspace_id`, no workspace policy and no grant to the app role, only the `privileged_access` policy, so only this transaction can touch it |
| `PrivilegeSignupSweep` | `auth/signup.go` (`ClearSignupAddresses`) | Clears the client address hash of old `cloud_signups` rows (the platform-level table above); runs hourly from `serve` |
| `PrivilegeAgentRequestPrune` | `store/agent-requests.go` (`PruneAgentRequests`) | Clears the call, edit and answer (`body`, `edited_body`, `result`) of agent requests decided more than 90 days ago, in every workspace, with one `UPDATE`; the rows stay as the audit record and nothing is read back. Runs at start and daily from `serve` |
| `PrivilegeServerSecret` | `store/server-secrets.go` (`ServerSecret`) | Reads, and on the first start of any process creates, the secrets every server process shares (the image proxy signing key) in `server_secrets`: no `workspace_id`, no grant to the app role, only the `privileged_access` policy (same shape as `cloud_signups`) |
| `PrivilegeMailImageSweep` | `store/mail-image-cache.go` (`SweepMailImageCache`) | Deletes, in every workspace, the cached email images past their 30-day life, then the least recently served while the cache is over its byte cap; only keys and sizes leave the transaction and the bytes go through the scoped `DiscardObject`. Runs at start and hourly from `serve` |
| `PrivilegeSavedReplyPurge` | `store/saved-reply-history.go` (`PurgeSavedReplyTrash`) | Deletes, in every workspace, the saved replies whose `deleted_at` is more than 30 days old, with one `DELETE`; their `saved_reply_versions` rows go by cascade and nothing is read back. Runs at start and daily from `serve` (same ticker as the agent request prune), which logs the count |
| `PrivilegeKBPurge` | `store/kb-images.go` (`PurgeKBTrash`, `BackfillKBMedia`) | Deletes, in every workspace, the knowledge base articles and then the collections whose `deleted_at` is more than 30 days old (versions, translations and media links go by cascade), then the library items in the trash for 30 days or never kept, linked by no version and last uploaded 30 days ago (never one a live article's current text names), and the library's trash batches older than 30 days; only their workspace ids and storage keys (original, thumbnail, poster) leave the transaction, and the bytes go through the scoped `DiscardObject`. `BackfillKBMedia` lists the workspace and item ids and storage keys of images not inspected yet; it reads, thumbnails and updates each in its own workspace. Runs at start and daily from `serve` (same ticker as the saved reply purge), which logs the counts |
| `PrivilegeKBUploadDisk` | `store/kb-media-uploads.go` (`KBMediaUploadsInProgress`) | Counts, over every workspace, the video uploads in progress and sums the bytes they still need (`size - received`), so a new upload's disk check and the server-wide valve of 200 count the other workspaces' uploads; only the count and the total leave the transaction. Called when an upload starts, one start at a time |
| `PrivilegeRetention` | `retention/service.go` | Lists locked workspaces, reads their admins to send the 30-day deletion notice, and hard-deletes a workspace locked for 12 months with its attachment files. Each pass locks one workspace row and acts on it only. `retention_deletions` is a platform-level table like `cloud_signups` (privileged policy only, no grant to the app role) |
| `PrivilegeWorkspaceBySlug` | `database/scope.go` (`WorkspaceIDBySlug`) | CLI `pair`, `token create`, `import freshdesk`, `import undo`, `trial set`, `plan grant`, `plan revoke` name a workspace by slug; hosted mail routing (`store.WorkspaceForMailSlug`) finds the workspace of `support@<slug>.<HOSTED_MAIL_DOMAIN>` from the envelope recipient, then continues only if its email channel is connected; the public web form (`store.OpenWebForm`) finds the workspace of `POST /api/v1/public/forms/{slug}`, then reads and writes only inside that workspace's scope |
| `PrivilegeSessionToken` | `auth/sessions.go` | Session token hash → workspace; session checks then run scoped |
| `PrivilegeLinkToken` | `auth/links.go` | Pairing/invite link token hash → workspace; redemption then runs scoped |
| `PrivilegeCloudIdentity` | `auth/cloud.go` | Lists the workspaces where a verified cloud email is an agent |
| `PrivilegeReplyToken` | `store/email-inbound.go` | Inbound mail reply token → workspace (`MAIL_FROM_DOMAIN` routing, Resend bounce webhook) |
| `PrivilegeVerifiedSender` | `store/email-relay-bounce.go` | Resend event From domain → the one workspace whose support domain the provider verified |
| `PrivilegeProviderDomainSweep` | `store/email-provider-domains.go` | The background sweep lists and settles the email provider domains queued for deletion, across workspaces and including deleted ones; tenants only enqueue their own |
| `PrivilegeBillingReconcile` | `store/billing-reconcile.go` | `collie billing reconcile` reads every workspace id and every stored Polar subscription in one transaction to compare them with Polar's list; every write goes through `ApplySubscription`, scoped to its workspace |
| `PrivilegeInboundSecret` | `store/email-channels.go` | Inbound email secret key id → workspace |
| `PrivilegeOutboxClaim` | `store/email-outbox.go` | The email worker leases due rows of all workspaces; each row is then loaded and finished scoped |
| `PrivilegeMailboxGrantShared` | `store/mailbox-connections.go` (`MailboxAddressHeldElsewhere`) | After a workspace removed its own Gmail connection (disconnect, another account, channel removed), asks whether any other workspace still holds a connection for that address, connected or failed. Google revokes an account's whole grant to the project, so the grant is revoked only when the answer is no. One `EXISTS`; only the yes or no leaves the transaction |
| `PrivilegeTelemetry` | `telemetry/usage.go` | The daily usage report claims the day in `instance` (a platform-level table like `cloud_signups`: install id and last report time) and reads instance-wide counts: workspaces, agents seen in 30 days, replies sent in 30 days, whether any email channel, enabled web form or Freshdesk import record exists. One `SELECT` of aggregates under a 10 s statement timeout; no row content leaves the transaction |

The Freshdesk importer and undo resolve the slug once, then run every read and write in
`InWorkspace` for that workspace. Their advisory lock (`DB.TryAdvisoryLock`) reads no table.

## Guards

- **`TestEveryWorkspaceTableEnforcesRowLevelSecurity`** reads the catalog.
  - It checks every table in the schema that has a `workspace_id` column or that the app role holds
    any privilege on, plus `workspaces`.
  - It fails when one of them lacks ENABLE, FORCE or the `workspace_isolation` policy.
  - Default privileges grant every new table to the app role, so a new table must add all three in
    its own migration, together with `privileged_access`, or revoke the app role. There is no
    allowlist; `schema_migrations` is revoked.
  - `TestSecurityGuardFlagsAnyTableTheAppRoleCanReach` proves the gate catches a table without
    `workspace_id`.
- **`TestPrivilegedPathsMatchTheReviewedAllowlist`** parses the server source. It fails in three
  cases:
  - A `Privileged(...)` call site is added or removed.
  - A new file imports `pgxpool`.
  - Code outside `internal/database` calls `Query`, `QueryRow`, `Exec`, `Begin`, `BeginTx`,
    `Acquire`, `SendBatch` or `CopyFrom` on a `*pgxpool.Pool` (parameter, field, variable, or
    result of `pgxpool.New`).

  To add a privileged path, update the test's allowlist and the table above in the same change.
  Explain in the review why the path must cross workspaces.
- **Policy tests:** `TestUnfilteredQueryInWorkspaceSeesOnlyThatWorkspace`,
  `TestQueryWithoutWorkspaceContextSeesNoRows` and
  `TestWorkspaceCannotWriteRowsOfAnotherWorkspace` check the policies themselves.
- **Non-superuser owner:** `TestNonSuperuserOwnerIsAlsoFailClosed` runs migrations and queries as a
  non-superuser owner. Forced RLS hides all rows from its plain queries.

## Traps

- **Tests connect as a superuser.** Raw `pool.Query` calls in tests bypass RLS on purpose; they
  seed and inspect data. Production code must never copy that pattern; the raw-pool guard fails on
  it outside `internal/database`.
- **One transaction, one workspace.** Do not call a public `Store` method from inside a scoped
  callback. It opens a second transaction on a second connection, and a small pool can deadlock.
  Use the package-level helpers that take the `pgx.Tx` (`getConversation`, `getMessage`, …).
- **Do not hold a scoped transaction across network calls** (DNS, SMTP, Freshdesk). Read, end the
  transaction, then call out. `VerifyEmailDomain` and the importer follow this.
- **Performance.** A scoped call adds `BEGIN`, one `set_config` round trip and `COMMIT`. The inbox
  p95 gate in `performance.md` runs through this path and must stay within budget.

## Operator notes

- **PostgreSQL 16 or newer** is required. The `SET` option of role membership (`pg_has_role(...,
  'SET')`, `GRANT ... WITH INHERIT FALSE, SET TRUE`) appeared in 16. `compose.yaml`, `install.sh`
  and CI use `postgres:17-bookworm`.
- **Upgrades** need no manual step. `collie migrate`, which the compose command runs, creates
  the app role and grants it.
- **Optional hardening for an existing install** (request-login mode):
  1. Add a line to `.env`:
     `DATABASE_REQUEST_URL=postgres://colliedesk_request:<new random password>@postgres:5432/colliedesk?sslmode=disable`
  2. Run `docker compose up -d`. Migrate creates the login.
  3. Optional: to also move off the superuser owner, create a non-superuser owner and reassign the
     database to it. This is a manual DBA step, not automated.
- **Managed PostgreSQL** (owner not a superuser, no `CREATEROLE`): the migration stops with the
  exact statement to run as a superuser. One example is
  `CREATE ROLE colliedesk_app_<db> NOLOGIN; GRANT colliedesk_app_<db> TO <owner>;`. Then run
  `collie migrate` again. The same applies when only the GRANT is missing.
- **Changing the owner role.** `privileged_access` names the role that ran the migration. If you
  move to another `DATABASE_URL` role that is not a superuser, make it the owner of the tables and
  recreate that policy for it. A superuser role needs nothing.
- **Backups.** `pg_dump` does not include roles. Run it as a superuser. With FORCE RLS, a
  non-superuser owner stops with "query would be affected by row-level security policy". Before
  you restore into a new cluster, create the app role:
  `CREATE ROLE colliedesk_app_<db> NOLOGIN; GRANT colliedesk_app_<db> TO <owner>;`.
- **Passwords.**
  - The request login's password never reaches PostgreSQL; only its SCRAM verifier does.
  - The installer's owner password reaches `psql` through its environment (`\getenv`), not argv.
  - psql still expands the owner password into the one-time `CREATE ROLE` that initdb runs. If that
    statement failed, the log of the fresh volume would hold it.
- **Dropping a database.** Roles are cluster-wide, so dropping a Colliedesk database leaves its app
  role, and the request login if there is one.
  1. Before the drop, read the app role's name in that database:
     `SELECT roles[1] FROM pg_policies WHERE tablename='workspaces' AND policyname='workspace_isolation';`
  2. Drop the database.
  3. As a superuser, run `DROP ROLE <app role>;` and `DROP ROLE colliedesk_request;` if no other
     database uses them.

  Tests that create databases or roles drop them in cleanup. The shared test database keeps its
  own `colliedesk_app_<db>` while it exists.
