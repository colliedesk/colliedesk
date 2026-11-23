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
| `colliedesk_app_<database>` (app role) | Every workspace-scoped transaction switches to it; owns the three help center definer views (below) and nothing else | Only the workspace in `app.workspace_id` |
| `colliedesk_reader_<database>` (help center reader) | Every help-center-scoped transaction of the public help center switches to it | Only the rows the help center in `app.help_center_id` shows, of the workspace in `app.workspace_id`; no grant on any other table |
| `DATABASE_REQUEST_URL` login (optional, `colliedesk_request` in installs) | Opens the request pool | Nothing by itself; it can only `SET ROLE` to the app role, and through it to the reader |

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
- The reader is missing, is a superuser or has `BYPASSRLS`.
- The app role inherits the reader (`pg_has_role(..., 'USAGE')`). Its RESTRICTIVE policies would then
  bind the app role too, and staff would lose every article no help center shows.
- The request connection cannot `SET ROLE` to the reader, or a separate request login inherits it.

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

A third way serves the public help center:

- `InHelpCenter(ctx, workspaceID, helpCenterID, fn)` / `BeginHelpCenter` / `BeginHelpCenterTx` start
  a transaction on the request pool as the help center reader. They set `role`,
  `app.workspace_id` and `app.help_center_id`, all transaction-local. A workspace or help center id
  that is not a UUID returns `ErrNoWorkspace` or `ErrNoHelpCenter` before any SQL runs.

## The public help center reader

A workspace can have several help centers. A visitor of one must not read a row only another one
shows, even through a query that forgets a filter. The reader role enforces this in the database.

### The role

- `NOLOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT`, one per database, named like the app role
  (`colliedesk_reader_` plus the database name, cut with an md5 suffix past 63 bytes). It owns
  nothing.
- It is granted to the app role `WITH INHERIT FALSE, SET TRUE`. The owner and the request login reach
  it through the app role, and its policies never bind the app role. Leaving it (`RESET ROLE`,
  `SET LOCAL ROLE NONE`) returns to the request login, which has no rights
  (`TestRequestLoginCannotLeaveTheHelpCenterScope`).
- The server reads its name back from `roles[1]` of `help_center_isolation` on `kb_articles`
  (`DB.ReaderRole`). `pg_policies.roles` is sorted by name, and `colliedesk_app_…` sorts first.
- Grants: `SELECT` on the knowledge base tables and on `help_centers`, `help_center_languages`,
  `kb_collection_help_centers`. `SELECT, INSERT, UPDATE` on `kb_article_votes` and
  `kb_article_vote_versions` (a visitor's vote), with their sequences. `SELECT` on the views listed
  below. Column `SELECT` on `import_items` (`workspace_id`, `source`, `entity`, `source_id`,
  `target_id`, `status`, `updated_at`), so an old helpdesk address finds its page; never the
  import's errors or source data (`TestTheHelpCenterReaderReadsOnlyTheLedgerColumnsARedirectNeeds`).
  Nothing on conversations, messages, contacts, channels, tokens, sessions, billing or
  `help_center_domains`.

### The policies

`secure_help_center_table` keeps every policy of a help center table on the reader:

- `workspace_isolation` is extended to the app role and the reader.
- A RESTRICTIVE `help_center_isolation` policy for the reader only adds the help center check. A
  RESTRICTIVE policy narrows the permissive ones, so the reader needs both.

Each table reads the one above it through its own policy, so one chain decides everything:

| Table | The reader sees a row when |
|---|---|
| `help_centers` | `id` is `app.help_center_id` |
| `help_center_languages`, `kb_collection_help_centers` | `help_center_id` is `app.help_center_id` |
| `kb_collections` | its `top_collection_id` is placed on that help center |
| `kb_collection_translations`, `kb_article_collections` | its collection is visible |
| `kb_articles` | its home collection is visible, or it is listed in a visible collection |
| `kb_article_translations`, `_media`, `_attachments`, `_tags`, `_vote_resets`, `_votes`, `_vote_versions` | its article is visible |
| `kb_media` | an article, attachment, logo or favicon visible to the reader names it |
| `import_items` | its `target_id` is an article or collection visible to the reader |

An unset or empty `app.help_center_id` is `NULL`, so the reader sees no row
(`TestAHelpCenterReaderWithoutAHelpCenterSeesNothing`). The app role keeps seeing every article of
its workspace, unplaced and agents-only ones too.

`kb_collections.top_collection_id` holds the top-level collection a collection sits under (itself at
the top). The policy on `kb_collections` is then one indexed `IN`, not a walk up the parents. Two
triggers keep it:

- `kb_collections_top` (BEFORE INSERT or UPDATE of `parent_id`) copies the parent's top.
- `kb_collections_children_top` (AFTER UPDATE) carries a new top down one level per update and stops
  where the value already matches.

A collection moved under another tree moves its articles to that tree's help center
(`TestMovingACollectionMovesItsArticlesToTheOtherHelpCenter`). The migration fills the column for
existing collections (`TestRowSecurityMigrationGivesExistingCollectionsTheirTopCollection`).

**EXISTS or a maintained table.** The policies use `IN (SELECT …)` over the table above, which the
planner runs as a semi-join on the primary keys. A trigger-maintained table of visible articles per
help center was the other option. `EXPLAIN (ANALYZE, BUFFERS)` as the reader, on the 2,000-article
knowledge base of the latency budget tests, put every statement of a page together at 19 to 28 ms
(article 19 ms, collection 23 ms, search 22 ms, French article with related articles 20 ms, French
collection 25 ms, French search 26 ms). The policies stay as they are, and the only maintained value
is `top_collection_id`.

A translation in a list of articles is checked against `kb_published_translations`
(`shownArticleKBTranslation`), once the statement checks the article with `publicKBArticle`. Checking
`kb_help_center_public_translations` there as well makes the planner expect one row, and it runs the
whole join once per collection: the last page of a French collection took 4.5 s instead of 25 ms.

### Definer views

Whether an article is public depends on every collection it is listed in. A listing may sit on
another help center, which the reader cannot see. Read as the reader, `kb_public_articles` would miss
that listing and could show an article an agents-only collection hides.

Three views decide this for the whole workspace: `kb_help_center_collection_ranks`,
`kb_help_center_public_articles` and `kb_help_center_public_translations`.

- They are owned by the app role and are not `security_invoker`. They read the tables as the app role
  (`workspace_isolation` only), whoever selects from them.
- Each also filters on `workspace_id = app.workspace_id` itself. Restored with `--no-owner`, or owned
  by a superuser, they would otherwise return every workspace's ids
  (`TestHelpCenterViewsKeepToTheWorkspaceWhoeverOwnsThem`). `DB.Verify` refuses to serve when a view
  is missing or owned by any role but the app role, and names the `ALTER VIEW … OWNER TO` to run
  (`TestVerifyRefusesAReaderThatSkipsHelpCenterIsolation/view_owner`).
- They are not `security_barrier`. The barrier stops the planner from pushing the article id into
  the view: the French article page after an edit went from about 53 ms to 101–108 ms as the reader,
  and a barrier on only some of the views from about 50 ms to 1.7 s. The views return ids only, and
  the explicit workspace filter is what keeps them inside the workspace.
- They read only tables and immutable functions. A function that reads a table runs as its caller, so
  the parent walk is a correlated recursive subquery (one key lookup per level, at most nine levels),
  not `kb_parents_visibility_rank`.
- They match `kb_public_articles` and `kb_public_translations` row for row
  (`TestHelpCenterPublicViewsMatchTheWorkspaceViews`,
  `TestAnAgentsOnlyCollectionSixLevelsUpKeepsTheArticleOffEveryHelpCenter`).
- Reviewed exception: through them the reader can learn the ids of every public article of its
  workspace, on any help center. It never reads the article itself, because `kb_articles` stays
  behind `help_center_isolation`. Public articles are public on their own help center already.

The reader also has `SELECT` on `kb_collection_visibility`, `kb_published_articles` and
`kb_published_translations`, which are invoker views over tables it is limited on.

### Store methods

The public site reaches the store only through these methods. Each runs through `helpCenterScoped`
(`internal/store/help-center-scope.go`) and never through the app role's scope:

`HelpCenterArticleIn`, `HelpCenterArticlesIn`, `HelpCenterBrandImage`, `HelpCenterCollectionsIn`,
`HelpCenterFile`, `HelpCenterImage`, `HelpCenterIndex`, `HelpCenterLegacyTarget`, `HelpCenterRelatedArticlesIn`,
`HelpCenterTranslationIndex`, `HelpCenterVideo`, `KBArticleVotesVersion`, `SearchHelpCenterIn`,
`VoteKBArticle`.

`helpCenterScoped` resolves `""` to the workspace's primary help center as the app role first (the
reader reads only its own `help_centers` row). A workspace without one is read as the all-zero UUID,
which no row has. The callback gets the resolved id, so its own `$H` filters never call
`primary_help_center_id` as the reader.

Staff, MCP and AI paths stay on the app role. They see every help center of the workspace.

### Reviewed cross-help-center routes

These public-site calls stay on the app role, each for a stated reason. Adding one is a security
review; `appRoleStoreMethods` in `internal/helpcenter/store_reader_guard_test.go` holds the same list.

| Store method | Decision |
|---|---|
| `HelpCenterHost`, `HelpCenterClaim`, `GetHelpCenterDomain` | Host lookup: a host name decides which help center serves the request, before there is one to read as. The reader has no grant on `help_center_domains`. |
| `OpenHelpCenter`, `OpenHelpCenterAt`, `OpenHelpCenterWorkspace` | Resolve the help center row. `content_version` (the page cache key) is read here. `last_article_number` lives on the primary row and only the staff path increments it (`store/kb-articles.go`). |
| `OpenHelpCenterHandle` | Resolves a handle among the workspace's help centers. |
| `WorkspaceForMailSlug` | Resolves a workspace from its slug before any help center is chosen. |
| `HelpCenterLegacyItems` | Staff only. It has no help center filter: it lists every imported article and collection of the workspace for the redirect snippet, and only the session route `GET /api/v1/help-center/freshdesk-redirect` (`PermHelpCenterRead`) calls it. The public lookup of one old address, `HelpCenterLegacyTarget`, runs as the reader. |
| `HelpCenterPreview` | Staff only: the theme preview of a session route (`httpapi/help-center-theme-handlers.go`). It shows the public articles of the help center it names (`help_center_id`, the primary without one; `onCenter`, `shownKBArticle`), never drafts or agents-only content; another workspace's or a deleted id is 404. It stays on the app role because it checks the candidate logo and favicon against the whole media library, trash included, as a save does; the reader sees only media its help center already uses. |
| `LogKBSearch`, `OpenKBSearch` | The search log. The per-minute cap (`KBSearchesPerMinute`) counts the whole workspace's searches; read as the reader it would count one help center's. A search written before the workspace has a help center makes the primary one (`kb_searches_on_primary`). The reader has no grant on `kb_searches`, so it never reads stored search words. The open is signed with the workspace and the row id; the foreign key keeps `help_center_id` inside the workspace. |
| `OpenKBArticleFeedback` | A reader's "No" that leaves a line and an address opens a conversation, when the knowledge base turned `feedback_conversations` on (read on the primary row, like `require_approval`). The vote itself is written first, as the reader. Then this call reads that vote back by its reader hash and opens the conversation, contact and message as the app role, because the reader has no grant on `conversations`, `contacts` or `messages`. It is capped per client address and per workspace, and it links the vote (`kb_article_votes.conversation_id`), so a resend opens nothing more. |

Other routes that touch more than one help center:

- **`movedTo`** (`helpcenter/site.go`): an old address redirects to the help center's custom domain.
  It uses the host lookup above and reads no article.
- **An article link to another help center:** the article page answers 404 for an article this help
  center does not show (`helpcenter/pages.go`). The reader cannot read it, so a link never renders
  another help center's text.
- **`primary_help_center_id()`:** called only as the app role (staff, import, export, help chat, and
  `helpCenterScoped` itself). As the reader it would see only its own row.
- **Votes:** written as the reader; a vote through a help center that does not show the article is
  refused by row-level security (`TestAHelpCenterVisitorVotesOnlyOnItsOwnHelpCenter`).
- **Article views:** counted in memory and written by `PrivilegeKBArticleViews` (privileged list
  below); no row is read back.
- **Web form:** `store.OpenWebForm` runs through `PrivilegeWorkspaceBySlug`, then in the app role's
  scope; it writes a conversation, which the reader must never reach.
- **Help chat (known exception):** `ChatKBArticlesIn` (`store/help-center.go`) runs as the app role
  through `DB.InWorkspace` (`ai/chat-context.go`). It reads `helpChatKBArticle`
  (`kb_help_chat_articles`), which has no help center filter: every published article of the
  workspace that is public or kept for help chat, on whichever help center it sits, or on none. Translations are checked
  against the primary help center's languages. The guard test scans only the `helpcenter` package,
  so this path and the channel routes that read the knowledge base (`httpapi/help-chat-handlers.go`,
  `helpChatAdditions`) are not covered by it.

## Workspace hosts

With `HELP_CENTER_DOMAIN`, each workspace's desk runs on its own host `<slug>.<HELP_CENTER_DOMAIN>`
(`internal/workspacehost`). `helpcenter.Site.Wrap` marks every request there with the host's slug, and
the request then acts on that workspace only:

- `auth.Service.Authenticate` refuses a session or agent token of another workspace on that host
  (`ErrInvalidCredential`); the API answers 401 and clears the cookie. The session cookie has no
  `Domain` attribute, so the browser sends it only to the host that set it. On `APP_ORIGIN` every session
  works as before.
- A one-time link (pairing, invite, hand-off) redeems on a workspace host only when it belongs to that
  workspace; another workspace's link stays unused.
- Cloud sign-in takes the workspace from the host, never from the request body, and accepts the
  identity token's authorized party `https://<slug>.<domain>` only on that host.
- Writes with the cookie must come from the workspace host's own origin (`httpapi.requestOrigin`).
- A hand-off from `APP_ORIGIN` is a one-time link of kind `handoff` for the same agent and role that
  lives one minute; its token travels in the address's fragment, which no server receives or logs. It
  redeems only on its workspace's host and only as a cookie session, never as a bearer token. It is no
  pairing link: it does not prove the agent's address. Making one deletes up to 100 of the workspace's
  expired hand-offs.
- A sign-in on `APP_ORIGIN` that continues on a workspace host keeps no session on `APP_ORIGIN`: the
  session it made is revoked and only the hand-off crosses over, so signing out on the workspace host
  ends it. `APP_ORIGIN` keeps the cookie `colliedesk_workspace` with the slug, which is no credential: a
  page load there redirects to the same page on that host. A session on `APP_ORIGIN` from before the
  move hands off once; the page load that hands it off revokes it and clears its cookie.
- The hint is kept only for a cloud email of one workspace. When the verified email is an active agent
  of another workspace too (`Redeemed.OtherWorkspaces`, from the same lookup as the 409 `workspaces`
  chooser), the sign-in clears the hint instead, so `APP_ORIGIN` asks again next time rather than sending
  the person into the last workspace (`TestAPersonWithSeveralWorkspacesIsNotSentIntoTheLastOneFromTheAppHost`).
  A sign-in by link and the hand-off of a pre-move session still set it: their email is not verified, so
  the server does not look it up in other workspaces.
- `Authenticate` checks the host before it refreshes `last_seen_at` and `expires_at`, so a session sent
  to another workspace's host is not extended.
- A path a sign-in opens (`next`) is refused when it holds a control character: a browser drops tabs
  and line breaks, so `/\t/evil.com` would open `//evil.com`.
- The help center under `/help` shares the host with the desk session, so its pages run no script:
  `script-src 'none'`.

Tests: `workspace_host_test.go`, `workspace_host_sign_in_test.go`, `workspace_host_sign_out_test.go`,
`workspace_host_switch_test.go`, `workspace_host_hardening_test.go` in `internal/httpapi`.

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
| `PrivilegeServerConfig` | `serverconfig/store.go` (`Store`) | Reads and writes the server's own configuration: `server_config_versions` and `_values`, `server_config_state`, `server_processes`, `polar_products`, `operator_audit`, and the sign-up hash key in `server_secrets`. None has a `workspace_id` or a grant to the app role; only the `privileged_access` policy. No workspace row is read (docs/internals/operator-console.md) |
| `PrivilegeOperator` | `auth/operator.go` (`OperatorService`) | Signs in the people who run the server: `operators`, `operator_links`, `operator_sessions`, `operator_requests`, and their lines in `operator_audit`. No `workspace_id`, no grant to the app role, only the `privileged_access` policy; no workspace row is read |
| `PrivilegeOperatorPreview` | `serverconfig/preview.go` (`Preview`) | Counts what a console change would do across workspaces before it is saved: how many workspaces lock when billing turns on (trial over, nothing paying or complimentary in force), whether the slug `problem_report.workspace` names exists, and, when a feature is switched off, how many email channels and workspaces send through that mailbox provider or run AI on the server's provider. Only a number or a yes or no leaves the transaction, never a row; the retention count comes from `retention.Service.Plan` under `PrivilegeRetention` |
| `PrivilegeMailImageSweep` | `store/mail-image-cache.go` (`SweepMailImageCache`) | Deletes, in every workspace, the cached email images past their 30-day life, then the least recently served while the cache is over its byte cap; only keys and sizes leave the transaction and the bytes go through the scoped `DiscardObject`. Runs at start and hourly from `serve` |
| `PrivilegeIntakeReasonPrune` | `store/intake-ai.go` (`PruneIntakeReasons`) | Erases `reason` of the AI decisions of chat intake made more than 90 days ago, in every workspace, with one `UPDATE`; the decisions stay (the labels of intake's evaluation) and nothing is read back. Runs at start and daily from `serve` |
| `PrivilegeSavedReplyPurge` | `store/saved-reply-history.go` (`PurgeSavedReplyTrash`) | Deletes, in every workspace, the saved replies whose `deleted_at` is more than 30 days old, with one `DELETE`; their `saved_reply_versions` rows go by cascade and nothing is read back. Runs at start and daily from `serve` (same ticker as the agent request prune), which logs the count |
| `PrivilegeKBArticleViews` | `store/kb-article-views.go` (`WriteKBArticleViews`) | Adds the help center article views the server counted in memory, for every workspace, to `kb_article_views` with one `INSERT … ON CONFLICT` every 30 seconds and at shutdown; each row names its own workspace and article (joined to `kb_articles` on both, so a count whose article is gone is skipped) and nothing leaves the transaction |
| `PrivilegeKBSearchRetention` | `store/kb-searches.go` (`DeleteOldKBSearches`) | Deletes, in every workspace, the help center searches older than `kb_searches.retention_days` (180 by default) with one `DELETE`; nothing is read back. Runs at start and daily from `serve` |
| `PrivilegeHelpCenterAnalytics` | `store/help-center-analytics.go` (`HelpCenterAnalyticsTargets`, `RecordHelpCenterAnalyticsResults`) | Lists the workspaces whose help center analytics is turned on, at start and every 30 seconds, so the server knows where to send page views; only workspace ids leave the transaction, and each target with its sealed GA4 API secret is read in its own workspace. Records, in one `UPDATE … FROM unnest` every 30 seconds and at shutdown, when each workspace's page views were last accepted or refused; each row names its own workspace and nothing is read back |
| `PrivilegeKBPurge` | `store/kb-images.go` (`PurgeKBTrash`, `BackfillKBMedia`) | Deletes, in every workspace, the knowledge base articles, then the translations, then the collections whose `deleted_at` is more than 30 days old (versions, translations and media links go by cascade), then the library items in the trash for 30 days or never kept, linked by no version and last uploaded 30 days ago (never one a live article's current text names), and the library's trash batches older than 30 days; only their workspace ids and storage keys (original, thumbnail, poster) leave the transaction, and the bytes go through the scoped `DiscardObject`. `BackfillKBMedia` lists the workspace and item ids and storage keys of images not inspected yet; it reads, thumbnails and updates each in its own workspace. Runs at start and daily from `serve` (same ticker as the saved reply purge), which logs the counts |
| `PrivilegeKBUploadDisk` | `store/kb-media-uploads.go` (`KBMediaUploadsInProgress`) | Counts, over every workspace, the video uploads in progress and sums the bytes they still need (`size - received`), so a new upload's disk check and the server-wide valve of 200 count the other workspaces' uploads; only the count and the total leave the transaction. Called when an upload starts, one start at a time |
| `PrivilegeRetention` | `retention/service.go`, `retention/trial-ended.go` | Lists locked workspaces, reads their admins to send the 30-day deletion notice, and hard-deletes a workspace locked for 12 months with its attachment files. Each pass locks one workspace row and acts on it only. `retention_deletions` is a platform-level table like `cloud_signups` (privileged policy only, no grant to the app role). Each pass first writes, in one statement, the `trial_ended` milestone of every workspace whose trial ran out with nothing paying (`docs/internals/workspace-milestones.md`); each row names its own workspace and nothing leaves the transaction |
| `PrivilegeWorkspaceBySlug` | `database/scope.go` (`WorkspaceIDBySlug`) | CLI `pair`, `token create`, `import freshdesk`, `import undo`, `trial set`, `plan grant`, `plan revoke` name a workspace by slug; hosted mail routing (`store.WorkspaceForMailSlug`) finds the workspace of `support@<slug>.<HOSTED_MAIL_DOMAIN>` from the envelope recipient, then continues only if its email channel is connected; a page of the app on a workspace host name `<slug>.<HELP_CENTER_DOMAIN>` (`helpcenter.serveWorkspaceHost`) and the tls-ask learn only whether the slug exists, to serve the app or the 404 and to allow a certificate; the public web form (`store.OpenWebForm`) finds the workspace of `POST /api/v1/public/forms/{slug}`, then reads and writes only inside that workspace's scope |
| `PrivilegeSessionToken` | `auth/sessions.go`, `auth/oauth.go` | Session token hash, or an OAuth code, access or refresh token hash (`oauth_tokens`) → workspace; session checks then run scoped |
| `PrivilegeLinkToken` | `auth/links.go` (`linkOf`) | Pairing/invite link token hash → workspace and kind; redemption and the invite preview then run scoped |
| `PrivilegeCloudIdentity` | `auth/cloud.go` | Lists the workspaces where a verified cloud email is an agent |
| `PrivilegeReplyToken` | `store/email-inbound.go` | Inbound mail reply token → workspace (`MAIL_FROM_DOMAIN` routing, Resend bounce webhook) |
| `PrivilegeVerifiedSender` | `store/email-relay-bounce.go` | Resend event From domain → the one workspace whose support domain the provider verified |
| `PrivilegeProviderDomainSweep` | `store/email-provider-domains.go` | The background sweep lists and settles the email provider domains queued for deletion, across workspaces and including deleted ones; tenants only enqueue their own |
| `PrivilegeBillingReconcile` | `store/billing-reconcile.go` | `collie billing reconcile` reads every workspace id and every stored Polar subscription in one transaction to compare them with Polar's list; every write goes through `ApplySubscription`, scoped to its workspace |
| `PrivilegeInboundSecret` | `store/email-channels.go` | Inbound email secret key id → workspace |
| `PrivilegeOutboxClaim` | `store/channel-outbound.go` | The outbox worker leases due rows of all workspaces (every channel type); each row is then loaded and finished scoped |
| `PrivilegeChannelInboundClaim` | `store/channel-journal.go` | The channel journal worker lists due events of all workspaces (received, failed, collected past their flush time); each set is then processed scoped, and processing locks the events |
| `PrivilegeAutoCloseClaim` | `store/auto-close.go` | The auto-close worker lists, in every workspace, the resolved conversations whose chat channel has `auto_close_hours` and whose time came (workspace and conversation ids only); each is then closed scoped, only while it is still resolved, so two servers close it once |
| `PrivilegeStatusNoticeClaim` | `store/status-notices.go` | The status notice worker lists, in every workspace, the conversations a bulk status change marked to tell their customers (workspace and conversation ids only); each is then told scoped, in the transaction that takes the mark and only while the conversation still has the marked status, so two servers tell it once |
| `PrivilegeSLAClaim` | `store/sla.go`, `store/sla-alerts.go` | The response clock worker lists the conversations of all workspaces whose clocks are stale, and the reminder and overdue thresholds they reached (ids, clock, threshold, due time only); each is then counted, claimed and sent scoped, the claim a unique row |
| `PrivilegeMentionEmailClaim` | `store/mentions.go` | The mention email sender lists the queued mention emails of all workspaces (workspace id and mention id only); each is then claimed, read, sent and settled scoped, the claim an update of that mention row |
| `PrivilegeKBReviewEmailClaim` | `store/kb-review-mail.go` | The review email sender lists the queued knowledge base review emails of all workspaces, to reviewers and to authors of decided reviews (workspace, review and agent ids only); each is then claimed, read, sent and settled scoped, the claim an update of that reviewer or review row |
| `PrivilegeCallbackRouting` | `store/callback-routing.go` | A provider's callback (Google, Microsoft, GitHub) arrives on the fixed address on `APP_ORIGIN`, where the workspace host's session cookie does not exist. The hash of a live mailbox consent or GitHub authorization state, or an email channel id (Microsoft admin consent), finds only the slug of its workspace; the browser goes to the same callback on that workspace's host, which checks the session, spends the state and finishes inside the workspace. An unknown, spent or expired state finds nothing and the callback refuses it on `APP_ORIGIN` as before |
| `PrivilegeGitHubInstallation` | `store/autofix-github-routing.go` | The Colliedesk GitHub App's webhook is shared by every workspace: the installation id it names (unique on the server) finds the one workspace that owns it, and the same read says whether the delivery id was applied already (`autofix_webhook_deliveries`, a platform-level table like `retention_deletions`: no workspace, no grant to the app role); a delivery is recorded after its event was applied. The ten-minute reconcile pass lists every installation with its workspace, and forgets delivery ids older than 30 days. Only workspace ids, installation ids and yes or no leave the transaction; every event and every check is then applied scoped to its workspace, and acts on a repository only when it is connected there through that installation |
| `PrivilegeIntakeDueClaim` | `store/intake.go` | The intake sweep lists the gatherings of all workspaces past their time to be decided (only their workspace and id leave the transaction); each is then applied scoped, under its own lock |
| `PrivilegeChannelRoute` | `store/channels.go` | A channel webhook shared by every workspace finds the live channel that is the account outside it names (accounts are unique on the server); only the workspace and channel ids leave. `ZaloSiteVerificationHeld` answers whether a live Zalo channel holds the site verification code a public `zalo_verifier<code>.html` names (the code is public: Zalo's checker reads it in that file); one `EXISTS`, only the yes or no leaves |
| `PrivilegeImportJobClaim` | `importjobs/store.go`, `importjobs/people.go` | The import worker leases due in-app import jobs of all workspaces, discards previews left over an hour, and lists the imported Freshdesk agents whose link changed; each job and each link is then applied scoped to its workspace |
| `PrivilegeAutofixClaim` | `store/fix-work.go` (`ClaimDueFixes`) | The Autofix worker leases the fixes in progress of all workspaces whose next check is due (a Cursor run to start, to read, or to stop when its repository was disconnected): one `UPDATE … FOR UPDATE SKIP LOCKED` moves their `next_check_at` a lease ahead, so two servers never check one fix at once. Only workspace ids, fix ids and connectors leave the transaction; each fix is then read, sent to its vendor and moved scoped to its workspace, with no transaction held across the vendor call |
| `PrivilegeAIJobClaim` | `ai/worker.go` | The AI worker leases due AI tasks of all workspaces, closes usage rows a dead process left started, expires AI drafts and triage suggestions undecided for a week and erases draft text and suggestion reasons 90 days after they ended (the sweeps return nothing); each task is then read, run and recorded scoped to its workspace, with no transaction held across the provider call |
| `PrivilegeMailboxKeepAlive` | `store/mailbox-connections.go` (`MailboxesToKeepAlive`) | Lists the connected mailboxes of a provider (Microsoft 365) whose grant was not renewed for a week, in every workspace, so the weekly keep-alive refreshes a quiet grant before Microsoft expires it after 90 days unused. Only workspace and channel ids leave the transaction; each grant is opened, refreshed and stored in its own workspace |
| `PrivilegeMailboxInboxRead` | `store/mailbox-inbox.go` (`MailboxesToRead`) | Lists the connected Gmail mailboxes whose grant includes `gmail.readonly` and whose reading was not refused, in every workspace, least recently read first, for the inbox reader. Only workspace and channel ids leave the transaction; each grant is opened, its new mail read and stored through the inbound pipeline in its own workspace |
| `PrivilegeMailboxGrantShared` | `store/mailbox-connections.go` (`MailboxAddressHeldElsewhere`) | After a workspace removed its own Gmail connection (disconnect, another account, channel removed), asks whether any other workspace still holds a connection for that address, connected or failed. Google revokes an account's whole grant to the project, so the grant is revoked only when the answer is no. One `EXISTS`; only the yes or no leaves the transaction |
| `PrivilegeHelpCenterHost` | `store/help-center-domains.go` (`HelpCenterHost`, `HelpCenterClaim`, `HelpCenterHostPending`, `HelpCenterHostProvenClaims`) | A request's `Host` (or the name the reverse proxy asks a certificate for) is all there is to find the help center a custom domain belongs to: `HelpCenterHost` returns the workspace id of the one active claim of a host name (a partial unique index allows one), `HelpCenterClaim` the workspace of a claim's CNAME target `hc-<claim>.<HELP_CENTER_DOMAIN>`, `HelpCenterHostPending` whether anyone claimed a name, and `HelpCenterHostProvenClaims` the workspace ids of the pending claims one DNS answer proves (claims and tokens are compared in the query). Read-only transactions; only workspace ids or a yes or no leave them; the token, the check and every page read run scoped. Requests to `APP_ORIGIN`'s host and below `HELP_CENTER_DOMAIN` never reach it, unknown names are charged to the client first, and the server caches its answers for 30 s per process |
| `PrivilegeHelpCenterDomainTakeover` | `store/help-center-domains.go` (`takeOverHelpCenterHost`) | A workspace whose claim DNS proves takes a host name over from the workspace active on it when the same DNS answer does not prove the holder's claim. One transaction locks the holder's row, compares its claim and token with the DNS answer inside the database, puts it back to pending (`taken_over`) and activates the new claim (matched by workspace and claim id); only the holder's workspace id leaves it, to refresh the cache |
| `PrivilegeHelpCenterDomainRecheck` | `store/help-center-domains.go` (`ActiveHelpCenterDomainWorkspaces`) | The daily re-check of custom help center domains lists the workspace ids with an active domain; each check then reads its domain and records the result scoped to that workspace. Runs at start and daily from `serve` |
| `PrivilegeAIRetrievalQueue` | `store/ai-retrieval.go` (`BuildAIRetrieval`) | The AI retrieval worker lists the workspaces with rows in `ai_retrieval_pending` (a skip scan of its index); only workspace ids leave the transaction, and each workspace's queued articles and conversations are read, folded, masked and written in its own scope. Runs every few seconds from `serve` |
| `PrivilegeKBGapQueue` | `store/kb-gaps-worker.go` (`BuildKBGaps`) | The knowledge gap worker lists the workspaces with rows in `kb_gap_pending` (a skip scan of its index); only workspace ids leave the transaction, and each workspace's queued searches, votes, chats and published articles are read, masked and grouped into gaps in its own scope. Runs every 30 seconds from `serve` |
| `PrivilegeKBGapTopics` | `store/kb-gaps-topics.go` (`BuildKBGapTopics`) | Once a day the knowledge gap worker lists the workspaces with at least 20 conversations resolved in the last 30 days in `ai_retrieval_docs`; only workspace ids leave the transaction, and each workspace compares its resolved conversations with its published article titles in its own scope |
| `PrivilegeResultsClaim` | `results/worker.go`, `results/weekly_email.go` | The results worker adds a `results_state` row for each workspace without one and leases the workspaces whose numbers are due (a lease column, so two servers never compute one at once); for the weekly email it lists each workspace's timezone and last claimed week. Only workspace ids, timezones and weeks leave the transaction; each workspace's numbers are computed, its week claimed and its email built in its own scope |
| `PrivilegeResultsShareToken` | `store/results-sharing.go` | Results share link token hash → workspace, for the public page `/r#<token>`; the totals are then read scoped to that workspace |
| `PrivilegeTrialOffer` | `store/trial-offers.go` | Trial offer token hash → workspace, to refuse another workspace's offer without naming it; offer id → workspace, for `collie trial offer revoke`; the offer is then read, accepted or revoked scoped to that workspace |
| `PrivilegeTrialReminder` | `trialreminder/trialreminder.go` | The trial-ending email pass lists the workspaces whose `trial_ends_at` falls within the next three days; only workspace ids leave the transaction. Each workspace's plan, admins, timezone and `workspace_trial_reminders` claims are then read and written in its own scope |
| `PrivilegeMetaConversionClaim` | `metaconversions/worker.go` | The Meta Conversions API worker (Colliedesk Cloud only) gives up the queued `meta_conversion_events` of all workspaces that are older than Meta accepts, clears sign-up addresses and browsers no event needs any more, and leases the due events; only workspace ids and event names leave the transaction. Each event is then read, sent and settled in its own scope |
| `PrivilegeTelemetry` | `telemetry/usage.go` | The daily usage report claims the day in `instance` (a platform-level table like `cloud_signups`: install id and last report time) and reads instance-wide counts: workspaces, agents seen in 30 days, replies sent in 30 days, whether any email channel, enabled web form or Freshdesk import record exists. One `SELECT` of aggregates under a 10 s statement timeout; no row content leaves the transaction |

The Freshdesk importer and undo resolve the slug once, then run every read and write in
`InWorkspace` for that workspace. Their advisory lock (`DB.TryAdvisoryLock`) reads no table.

## Securing a new table

Default privileges give the app role SELECT, INSERT, UPDATE and DELETE on every new table. A new
table is open to every workspace until its migration secures it. Do it in the same migration, with
one call:

1. Workspace data (the table has `workspace_id`):

   ```sql
   SELECT secure_workspace_table('notes');
   ```

   This enables and forces row-level security, adds `workspace_isolation` for the app role and
   `privileged_access` for the owner. Default privileges cover tables only, so it also gives the app
   role `USAGE` and `SELECT` on the table's serial sequences. It refuses a table without
   `workspace_id`.
2. Server-wide data (operators, server config, cloud sign-ups):

   ```sql
   SELECT secure_server_table('server_jobs');
   ```

   Only the owner on the privileged path reaches it. The app role loses every grant on the table and
   on its sequences.
3. Fewer privileges for the app role (history that is only added, rows that are marked deleted):

   ```sql
   SELECT limit_app_role('note_versions', 'SELECT', 'INSERT');
   ```

   A plain `GRANT SELECT, INSERT` restricts nothing, because it adds to the default privileges.
   `limit_app_role` revokes first, then grants exactly the listed privileges.
4. A table the public help center reads (after `secure_workspace_table`):

   ```sql
   SELECT secure_help_center_table('kb_article_notes',
       'article_id IN (SELECT id FROM kb_articles)', 'SELECT');
   ```

   The second argument is the reader's `help_center_isolation` condition. Write it over a table
   above it in the chain (here `kb_articles`), never over `app.help_center_id` again, so the table
   follows that table's visibility. The helper extends `workspace_isolation` to the reader, adds the
   RESTRICTIVE policy, revokes every grant of the reader on the table and grants exactly the listed
   privileges (with the sequences when `INSERT` is listed). A table that is not listed stays out of
   the reader's reach: default privileges do not cover it.

The first three helpers are defined in `20261014135949-table-security-helpers`, before row-level
security is set up, so every migration calls them, the oldest ones too.
`secure_help_center_table` and `reader_role()` are defined in
`20261122081247-help-center-row-security`. `migration-check` refuses an up migration that
writes `CREATE`, `ALTER` or `DROP POLICY`, `ROW LEVEL SECURITY`, `pg_policies` or `app.privileged` by
hand (comments do not count); only that migration, `20261014135950-workspace-row-level-security`,
which secures `workspaces` itself, and the help center migration that defines
`secure_help_center_table` spell it out. If a table needs a shape the helpers do not give, add
a helper in a migration and test it. The rest of a migration's layout is in
[migrations.md](migrations.md).

## Guards

- **`TestEveryWorkspaceTableEnforcesRowLevelSecurity`** reads the catalog for every table in the
  schema and compares each one with the exact shape the helpers make.
  - A workspace table must have ENABLE, FORCE and exactly two policies: `workspace_isolation`
    (permissive, all commands, the app role, the workspace check on `workspace_id`, or `id` on
    `workspaces`) and `privileged_access` (permissive, all commands, the owner, the privileged
    check). An extra permissive policy, a widened `USING` or a policy `TO PUBLIC` fails it.
  - A server-wide table (the list in the test) must have only `privileged_access`, and the app role
    must hold no privilege on it.
  - `channel_types` and `schema_migrations` have no row-level security; the app role must not reach
    them.
  - `TestSecurityGuardFlagsAnyTableTheAppRoleCanReach` and
    `TestSecurityGuardFlagsAPolicyThatOnlyMentionsTheWorkspace` prove the gate catches a table
    without `workspace_id`, a widened policy, an extra policy and a public policy.
- **`TestAppRoleHasExactlyThePrivilegesEachWorkspaceTableAllows`** fails when the app role holds
  other than SELECT, INSERT, UPDATE and DELETE on a workspace table, except for the tables listed in
  the test with fewer privileges.
- **`migration-check`** (CI) refuses hand-written row-level security in new migrations; see above.
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
- **Help center tables:** the catalog test above also pins the policy text of every table in
  `helpCenterPolicies` (`help_center_security_test.go`): `workspace_isolation` for the app role and the reader,
  and the exact `help_center_isolation` condition. `TestSecurityGuardFlagsAWidenedHelpCenterPolicy`
  and `TestSecurityGuardFlagsAReaderGrantOnTheInbox` prove it catches a widened condition and a
  reader grant outside the help center.
- **Reader policies:** `TestAHelpCenterReaderSeesOnlyItsOwnHelpCenter` (a raw `SELECT` of every
  help center table as the reader of A returns no row only B shows; the app role still sees unplaced
  and agents-only articles), `TestAHelpCenterReaderWithoutAHelpCenterSeesNothing` and
  `TestTheHelpCenterReaderReachesOnlyThePublicHelpCenter` (permission denied on the inbox, contacts
  and channels).
- **Verify:** `TestVerifyRefusesAReaderThatSkipsHelpCenterIsolation` (a `BYPASSRLS` reader, an app
  role that inherits it) and `TestRequestLoginCannotLeaveTheHelpCenterScope`.
- **Public site store calls:** `TestThePublicSiteReadsTheKnowledgeBaseAsTheHelpCenterReader`
  (`internal/helpcenter/store_reader_guard_test.go`) parses the package. Every `Store` method the
  public site calls must be a reader method whose body runs `helpCenterScoped` and no app-role scope,
  or a reviewed app-role method with its reason. A new call fails it until it is classified.

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
- **The reader cannot find the primary help center.** `primary_help_center_id($1)` reads
  `help_centers`, and the reader sees only its own row. Never bind `""` for `$H` inside an
  `InHelpCenter` callback; call through `helpCenterScoped`, which resolves the id as the app role
  first.
- **A function that reads a table runs as its caller.** A definer view that calls one is limited by
  the caller's policies again. Write the read inside the view.
- **Never grant the reader `kb_public_articles` or `kb_public_translations`.** Read as the reader they
  miss the listings on other help centers and can show an article an agents-only collection hides.
  Use `kb_help_center_public_articles` and `kb_help_center_public_translations`.

## Operator notes

- **PostgreSQL 19 or newer** is required. The `SET` option of role membership (`pg_has_role(...,
  'SET')`, `GRANT ... WITH INHERIT FALSE, SET TRUE`) appeared in 16; the floor is 19 so that queries
  can use what 19 added. `database.MinPostgresMajor` holds it, and every database command checks it first (`CheckPostgresVersion`), so an older server
  stops with a message naming the version found, the version needed and how to upgrade, instead of
  failing on SQL it does not know. `install.sh`, `compose.yaml`
  and CI pin `postgres:19.0-bookworm`, the version Colliedesk Cloud runs; an existing install on an
  older bundled major moves with `upgrade-postgres.sh` (`docs/self-host/upgrade.md#postgresql-versions`).
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
