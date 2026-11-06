# Authentication internals

## Hashed-token lookup

Pairing links and sessions use high-entropy random bearer tokens. Colliedesk displays or returns the
raw token once, then stores only its SHA-256 digest. Authentication hashes the supplied token and
uses an indexed PostgreSQL `bytea =` lookup against that digest.

This SQL comparison is acceptable for this threat model. The compared value is a fixed-length
one-way digest, not a password or another low-entropy secret. A timing difference cannot reveal a
useful token prefix: an attacker must still guess the complete random token, while rate limiting and
transport security remain separate controls. Constant-time comparison is required if a future auth
path compares raw secrets or low-entropy values in application memory.

## Roles and permissions

Decided with the roles refactor (branch `feat/roles-permissions`): what a person or an agent token may do is a set of
permissions, `resource:action` (`auth/token-permissions.go`, 62 of them), and a **role** is a named
set of them. Code: `auth/principal.go`, `auth/permissions.go`, `auth/legacy.go`, `store/roles.go`,
`httpapi/token-scope.go` (`routeAccess`), `httpapi/permission-errors.go`. User page:
`docs/user/roles-and-permissions.md`.

- **Tables.** `roles` (per workspace; `system` is `admin`, `agent` or null; soft delete with
  `deleted_at`), `agent_roles` (a person holds one or more; `granted_by_agent_id`), `invite_roles` (what
  an invite gives on joining, added to what the person holds). `sessions.role_id` is the role a session
  is limited to: the role a token follows (`scope='role'`), or the Agent role for a person's session
  from a pairing link that asked for it (`requested_role=agent`, shown "Limited to Agent"). The trigger
  `workspaces_system_roles` makes the two built-in roles for every new workspace, and
  `agents_starting_role` gives a new person the built-in role matching `agents.role`.
- **Admin is every permission, stored as none.** The Admin role's `permissions` is null and is read
  as the whole catalog (`roleRow.permissions`), so a permission a later release adds is Admin's at
  once and never Agent's: Agent's list is fixed (`agentRolePermissions`, the same list the migration
  writes; `TestTheAgentRoleStartsAsTheMigrationMadeIt`). Admin cannot be renamed, changed or deleted;
  Agent can be changed and reset, not deleted. Nothing decides on `system` except these two rules,
  the seat order and the last-admin rule (`Principal.IsAdmin`); every access check is a permission.
- **One table for people and tokens.** Every route of `Routes()` has one `routeAccess` row: `need(p)`,
  `byHandler(p)` (the handler decides by the body: a note needs `messages:note`, a reply
  `messages:send`; a status that would tell a customer needs `messages:send`, `refuseStatusThatTells`),
  `anyone`, `personOnly(why)`/`personOnlyNeeding(p, why)`, or `notForPeople`. `refuseByAccess` runs
  right after `Authenticate`, before CSRF and the lock: a route missing from the table is closed to
  everyone. There is no second, role-based table any more, and no handler checks `Role == "admin"`.
  `kb_articles:publish` is still checked in the handlers (`refuseUnlessHeld`) on every change the help
  center shows.
- **A call may do what person ∩ token allows.** `Principal.person` is the union of the person's roles
  (cut to `LimitedTo` for a limited session); `given` is what a token was given; `Can(p)` needs both.
  Both are read on every call, so a role change, a role taken away or a deactivation applies to every
  session and token at once. A token that follows a role gets the role's permissions as they are now
  (`given = role.permissions()`), and nothing while its person does not hold every permission of the
  role (`RoleHeld` false: "Stopped" in Sessions; giving the role back starts it again). The role need
  not be one the person holds: an admin's token may follow Agent. Creating and running use the same
  rule (person ⊇ role), so a token never starts in a state it would be stopped in. A role edit that
  adds a permission its token's person lacks stops that token. A custom token is a fixed list
  (`scope='custom'`, `permissions text[]`), still cut by what the person holds now.
- **Refusals say how to fix them.** A missing permission is 403 `permission_missing` with
  `missing_permissions`, `missing_from` (`token` when the person holds it, `person` otherwise) and, for
  `person`, `ask` (people who hold `team:manage_roles`). A person-only route answers 403 `person_only`
  with `where`. MCP passes the same fields back in the tool error.
- **Safety rules, all in `store/roles.go` under one lock.** A workspace keeps one active person with
  Admin (`ErrLastAdmin`, 409 `last_admin`); nobody gives, assigns, invites with or edits a role beyond
  what they hold (`ExceedsYoursError`, 403 `role_exceeds_yours`); nobody changes the roles, name or
  active state, or signs out the sessions, of someone who holds a permission they do not
  (`member_outranks_you`; Admin always may); a role in use (a member, an unused invite, a following
  token) is not deleted (`role_in_use` names them). A token's call is checked against the token's
  permissions, not only the person's.
- **Creating a token is person-only** (`personOnlyNeeding(team:create_tokens, …)`): a token that made
  tokens would leave them behind after it was revoked. That is why `maxPersonOnly` is 5 in
  `mcpserver/agent_parity_test.go` (the two agent-request decision routes, the mail image proxy, token
  creation and allowing an AI app with OAuth, raised from 4 for #477; raising it needs the maintainer's
  written approval). A token is created with
  `role_id` (any role whose permissions the caller all holds, else 403 `role_exceeds_yours`; 404 when
  there is no such role) or `permissions` (each held, else
  `role_exceeds_yours`), never both; a `scope` is 400. The CLI's `--scope` maps `read`/`draft`/`send`
  to that tier's old list and `admin` to the Admin role (`checkTokenRequest`).
- **Tokens made before roles** keep `scope` as their tier and `legacy=true`. `legacyGiven` gives them
  their old tier's list (or their custom list), minus `legacyNeverDirect`: the 14 permissions that
  used to always wait for a person (invite, export, billing, channel credentials…). Keeping those
  would let an old token export, invite or pay with nobody deciding. A tier below admin also keeps the
  old agent ceiling (`legacyAgentCeiling`). `TestLegacyTokensLoseExactlyWhatAlwaysAskedAPerson`
  checks this against the frozen tables in `httpapi/testdata/legacy-token-access.json`.
- **Agent requests are gone.** Nothing waits for approval: a token with the permission acts, and gets
  back the invite link, the new inbound secret, the export link or the share link itself. What needs a
  person in a browser (Google/Microsoft/GitHub consent, a fix's GitHub confirmation, billing checkout
  and portal) answers an address only the token's person can finish while signed in. A reply a token
  may not send is a written draft (`POST /conversations/{id}/reply-drafts`, `ai_assist:create`): an
  `ai_drafts` row with `source='written'` and its `cc`, shown as the draft card; without
  `messages:send` its `cc` may name only people already on the conversation
  (`store.ConversationParticipants`), so a prompt-injected draft cannot add an outside reader.
  `agent_requests` stays, read-only history: the upgrade closed pending requests as `expired` with a
  reason and copied pending reply drafts into `ai_drafts`; the export keeps `agent-requests.ndjson`.
  `agent_requests:read`/`:cancel` were removed (62 permissions, not 64).
- **Old columns are written, not read.** `agents.role` (`admin` when the person holds Admin,
  `syncLegacyRole`), `pairing_links.role_ceiling`, `sessions.role_ceiling` and the tier in
  `sessions.scope` stay in sync so a rollback from a backup works. Nothing decides on them; dropping
  them is for a later release.

### Traps

- **One lock for every rule.** Role, member-role, seat and last-admin changes take the advisory lock
  `agent-seats:<workspace>` (`lockRoles`), so two changes at once cannot both pass a rule. Taking a role
  from someone locks their unused invites first, by id, before that lock and their row: a redeem in
  flight holds its link and then waits for the seat lock, so the other order deadlocks.
- **Taking a role revokes invites that give it back** to the same address, and stops every token of
  theirs that follows it (the web form names them first).
- **A role key shaped like a uuid is always an id** (`findRole`); `/roles/{id}` and the MCP tools also
  take a name, so a role named like another role's id must never answer for it.
- **An invite whose roles were all deleted** while it was being redeemed refuses the redeem
  (`ErrInviteRolesGone`) rather than make a member without a role.
- **Upgrade limits old sessions.** The migration set `role_id` = Agent on every person's session that
  had `role_ceiling='agent'`, so an admin who signed in with an agent link stays limited to Agent in
  that session until they sign in again.
- **Rollback.** The down migration deletes tokens that follow a role (`scope='role'`, which the old
  release cannot read), reopens the requests the upgrade closed and drops the written drafts.
- **Operator requests remain.** `POST /agent-requests/{id}/approve` and `/reject` still serve the
  operator console (`X-Colliedesk-Realm: operator`); for the workspace realm they answer 410.

## Agent tokens

An agent token is a `sessions` row with a non-null `scope` (`role`, `custom`, or a legacy tier); a
null scope is a person's session. It reuses the session machinery on purpose: `label`,
`last_seen_at` (the "last used" time, written at most once a minute), revoke. Code: `auth/token.go`,
`auth/scope.go`, `auth/tokens.go`, `httpapi/token-scope.go`.

- **Own prefix and hash domain.** The raw token is `cdk_agent_` plus 32 random bytes in base64url.
  The stored `token_hash` is `sha256("colliedesk-agent-token\x00" + raw)`; a person's session token
  keeps the plain `sha256(raw)`. `Authenticate` picks the derivation from the prefix
  (`credentialHash`). Code older than the `scope` column hashes every bearer plainly, so it finds no
  row for an agent token and answers 401. That is what keeps an image rollback from turning a
  `read` token into a full agent session; `TestAgentTokenCannotBeFoundByThePlainHashAnOlderImageUses`
  pins it. The down migration still revokes every token, as a second line of defence. The prefix also
  lets a secret scanner (gitleaks custom rule, GitHub push protection) catch a token pasted into an
  agent config: the pattern is `cdk_agent_[A-Za-z0-9_-]{43}`.
- **Last use is written once a minute.** One MCP tool call authenticates about five times in a moment
  (its own gate plus each in-process REST call), and each used to `UPDATE` the same `sessions` row.
  `Authenticate` skips the write while the stored `last_seen_at` is under 60 seconds old; later, the
  write moves `last_seen_at` and slides `expires_at` as before, so the expiry can lag the true last use
  by at most a minute (`TestSessionLastUseIsRecordedAtMostOncePerMinuteAndStillSlidesTheExpiry`).
- **Every UI action is available to agents.** Every route a token may call is in some MCP tool's
  `Spec.REST`, or in `mcpToolPending` (`mcpserver/agent_parity_test.go`) with its tracking issue.
  `TestEveryRouteIsClassifiedForAgentsAndCoveredByAnMCPTool` fails on an unclassified or stale
  `routeAccess` row, a `PersonOnly` row without `Why`, an uncovered callable route, a stale
  `mcpToolPending` entry, and a tool that calls a closed route; its message names the file to edit.
  Ceilings: `maxClosedToTokens` and `maxMCPToolPending` are 0, `maxPersonOnly` 5.
- **The contract's `TokenPermission` enum is the Go list** in its order
  (`TestTokenPermissionEnumOfTheContractIsTheServersList`); the web matrix's copy is held to the
  contract by `token-permissions.test.ts`.
- **Credentials in answers.** `read` permissions never return a token, an invite link or the inbound
  secret (`TestTokensReadSettingsWithoutLinksOrSecrets`); only the creating call (with `team:invite`,
  `channels:manage_credentials`, `workspace:export`, `results:share`) returns one, once.
- **Revoking sessions from a token** follows the person's rules: its own person's sessions and tokens
  always, another person's with `team:revoke` when the person outranks them. The session list shows the
  revocation; `revokeSession` also logs the token session that did it.
- **Domain verification from a token** is limited to 10 checks per workspace per hour (in-process
  sliding window, 429 with `Retry-After`), so an agent cannot make the server hammer DNS. People are
  not limited.
- **Lifetime.** Thirty days of sliding expiry like a browser session, plus an absolute limit of 365
  days from `created_at`: `Authenticate` clamps the slid `expires_at` to it and refuses from that
  instant, however often the token is used. `insertAgentToken` takes `created_at` from the service
  clock, so tests move one clock. After the limit the owner creates a new token.
- **Attribution.** A message written with a token carries `messages.agent_token_id` (composite FK to
  `sessions(workspace_id,id)`, so a message names only a token of its own workspace; added `NOT VALID`
  and validated by the next migration, because one migration is one transaction and `VALIDATE
  CONSTRAINT` only stops blocking writes in a transaction of its own) and the API
  returns it as `agent_token` (`id`, `label`, `agent_name`). The web shows "by Ada via
  Claude Code". The column stays after the token is revoked, so what a revoked token wrote can still
  be told from what a person wrote. **Known limit:** only notes and replies are attributed. Status,
  priority, assignee and tag changes, the creation of conversations, contacts and tags, tag rename and
  delete, and retrying a reply record the agent but not the token. The repo has no conversation
  change log (a person's change is not attributed either); adding one is the way to close this.
- **MCP.** `/mcp` accepts a bearer only, authenticated by the same `Authenticate`, and each tool call
  runs through the REST handler with that bearer, so `routeAccess` binds MCP tools without a second
  check (`docs/internals/mcp.md`). A new route a tool uses needs a `routeAccess` row first.
- The CLI (`collie token create`) resolves the workspace by slug through
  `PrivilegeWorkspaceBySlug`, like `pair`.

## OAuth grants

Decided in #477 (Long, 2026-11-06): an AI app connects to `/mcp` with OAuth 2.1, the person picks only
the workspace, and the app acts as that person with everything they hold. Code: `auth/oauth.go`,
`httpapi/oauth-handlers.go`, `httpapi/oauth-clients.go`, the consent page `web/src/features/auth/oauth-consent.tsx`.

- **A grant is a session of scope `oauth`**, not a person's session (scope null): `Principal.IsToken()`
  is true, so every person-only route refuses it (it cannot create tokens or allow another app), a
  cookie holding one is refused, and messages it writes carry `agent_token_id` like any token's. Its
  `given` is a copy of what the person holds now (`setAccess`), cut to `role_id` when the session that
  allowed it was limited (an admin's Agent-limited session makes an Agent-limited grant). No one-year
  cap: it lives 30 days after its last use, like a browser session. `role_ceiling` is written for a
  rollback only.
- **The grant's id never changes; its credentials rotate in `oauth_tokens`** (`kind` code, access,
  refresh; RLS like every workspace table). `sessions.token_hash` of a grant is the hash of a random
  `cdk_oag_` value nobody receives, so a grant authenticates only through an access token:
  `Authenticate` finds `cdk_oat_` bearers in `oauth_tokens` (privileged lookup, then the session scoped
  by id and an unexpired access row). Codes (`cdk_oac_`) and refresh tokens (`cdk_ort_`) are refused as
  bearers. All four prefixes share the hash domain `colliedesk-oauth\x00` (`credentialHash`), so an
  older image finds none of them (`TestOAuthAccessTokensCannotBeFoundByThePlainHashAnOlderImageUses`).
- **Lifetimes.** Code 5 minutes, one use; a second use revokes the grant (RFC 6749 §4.1.2). Access token
  1 hour. Refresh token rotated on every use; the one it replaced still works for 2 minutes
  (`oauthRefreshGrace`) and gives another new pair without touching the others, so a client that lost
  the answer, or two processes of Claude Code sharing one credential, never sign each other out
  (`TestTwoRefreshesAtOnceWithOneTokenBothSucceed`). Used after that, it is a replay and revokes the
  grant. Used refresh tokens are remembered a day, then forgotten (`invalid_grant` without a revoke).
- **Clients are not stored.** Dynamic registration signs the app's name and redirects into the
  `client_id` (`cdk_client_<payload>.<HMAC>`, key `oauth_client_key` in `server_secrets`), limited to 1000
  valid registrations per client address an hour (a refused one is not counted). A client ID metadata document (an https `client_id`, Claude Code's) is
  fetched through `netguard` (public addresses after resolution, every dial), no redirect, 64 KiB, 5 s,
  cached 10 minutes; the document must name itself as `client_id`. Anyone can publish one, so the consent
  page always names its host and calls the name verified only on a known vendor's host (`trustedClientHosts`:
  exact or a subdomain at a dot, default port). Redirects: https anywhere, http only
  to `localhost`/`127.0.0.1`/`::1` (any port, RFC 8252), a private scheme with a dot or a known desktop
  app's (`cursor`, `vscode`…). `GET /api/v1/oauth/client` is the only unauthenticated way to make the
  server fetch one, 60 a minute per address.
- **Issuer.** The authorization server is `APP_ORIGIN` for every workspace (Cloud's one `/mcp`).
  `/.well-known/oauth-authorization-server` answers only there (its `issuer` must equal where it was
  fetched); `/.well-known/oauth-protected-resource[/mcp]` answers on every host with that host's `/mcp`
  as `resource`. A token is bound to the `resource` the app asked for (`APP_ORIGIN/mcp` or the
  workspace's own host's), refused otherwise (`invalid_target`); the code exchange and refresh compare it.
  The bound token also works on `/api/v1`, because every MCP tool calls the REST API with it in process.
- **Consent.** No new sign-in. `/oauth/authorize` is a web app page (`frame-ancestors 'none'`, set by
  `webui`) on the session of the host it opens on. On Cloud a browser without a session goes to the
  sign-in page on `APP_ORIGIN` with `next=/oauth/authorize?…`; `ExchangeCloud` asks which workspace
  (409 `workspaces`) and hands the browser off to the workspace's host, which opens the consent page;
  the code it issues is traded at `APP_ORIGIN/oauth/token`. **Allow** (`POST /api/v1/oauth/authorize`)
  is `personOnly` (`maxPersonOnly` 5, Long's approval quoted in `agent_parity_test.go`) and checks
  `Origin` for every caller. A request the app got wrong after the redirect was checked goes back to
  the app as an OAuth error; one with an unregistered redirect is shown on the page and never redirected.
- **Rollback.** The down migration keeps grant rows (a note names them) as revoked legacy `read`
  tokens and drops `oauth_tokens` (`TestOAuthGrantsRollbackKeepsTheNotesAGrantWroteAttributed`).

### Traps

- **A grant is never a person's session.** Making it one (scope null) would let an app allow other apps
  and create tokens that outlive its disconnection. Keep `scope='oauth'`.
- **One access hash per grant breaks real clients**: rotating it on refresh would sign out every other
  process sharing the credential. Access tokens are rows; old ones expire on their own.
- **Pending grants.** A grant whose code was never traded has `oauth_connected_at` null: it lists
  nowhere and the next Allow of the same person deletes those older than an hour.

## Operators

The people who run the server sign in to the operator console in a realm of their own: their own tables,
cookie, token prefix (`cdop_`) and tier. No workspace credential opens an operator route and no operator
credential opens a workspace route. See [operator console internals](operator-console.md#operator-realm).

## Optional cloud sign-in

Cloud sign-in is an OIDC-style bridge, not a second Colliedesk session system. The browser obtains
an RS256 session JWT from the configured identity provider. The server verifies it locally, maps its
verified email to an existing agent, creates an internal one-time pairing link, and redeems that link
through the normal session path. The provider token is never stored. Sign-in never creates a
workspace; the separate sign-up route below does, and only on the hosted cloud.

The bridge is provider-agnostic. Clerk is the current web widget, but the server only depends on an
issuer, a JWKS URL, authorized parties, and claims. All of these values must be present or cloud
sign-in remains disabled:

```text
CLOUD_AUTH_ISSUER=https://<frontend-api-host>
CLOUD_AUTH_JWKS_URL=https://<frontend-api-host>/.well-known/jwks.json
CLOUD_AUTH_AUTHORIZED_PARTIES=https://desk.example.com
CLOUD_AUTH_EMAIL_CLAIM=email
CLERK_PUBLISHABLE_KEY=pk_live_...
```

`CLOUD_AUTH_AUTHORIZED_PARTIES` is a comma-separated list of exact origins. With `HELP_CENTER_DOMAIN`, a
token exchanged on a workspace host may also name that host's own origin (`https://<slug>.<domain>`),
and no other workspace host's: list only `APP_ORIGIN` here. A Clerk production instance on the root
domain works on every subdomain; keep its "allowed subdomains" list off (or wildcard), since workspace
hosts are made at sign-up. Passkeys registered on one subdomain do not work on a sibling subdomain. The
email claim defaults
to `email`. `CLERK_PUBLISHABLE_KEY` is returned only by the public config endpoint and is safe for the
browser; no Clerk Secret Key is required because Colliedesk never calls Clerk's Backend API.

### Cloud sign-up (hosted cloud only)

`POST /api/v1/auth/cloud/signup {workspace_name}` starts a 14-day trial for a Clerk-verified email
(same bearer token, same `Origin` check as the exchange). The route answers 404 unless cloud sign-in
and billing are both configured, so a self-hosted server never offers it. Code:
`httpapi/signup-handler.go`, `auth/signup.go`, `domain/trial-email.go`, `domain/brand-names.go`.

- **One trial per mailbox.** The key is the email lowercased, with the `+tag` of the local part
  dropped; for `gmail.com` and `googlemail.com` dots are removed and the host is `gmail.com`. A
  second sign-up is 409 `trial_already_used` and the message points to sign-in. The record survives
  deleting the workspace (`ON DELETE SET NULL`), so deleting does not reset the trial. Other
  providers' aliases (a catch-all domain, `ada@corp` vs `ada@alias.corp`) are not detected.
- **Three sign-ups per client address in any rolling 24 hours**, then 429 `rate_limited`. A refused
  attempt is not counted. The check, the workspace insert and the record are one privileged
  transaction under an advisory lock (the cheap refusals run once before the lock, so doomed requests do not queue on it), so a failed create burns neither the email nor an IP slot.
- **Stored data is minimal and keyed.** `cloud_signups` holds the HMAC-SHA256 of the normalized email,
  the HMAC-SHA256 of the client address, the time and the workspace id. The key is `SIGNUP_HASH_KEY`
  (at least 32 random characters, kept in Bitwarden, passed through the compose environment). Without
  the key a row cannot be tested against a list of addresses; a plain hash of an IPv4 address or of a
  breached email could. Without a key (or one shorter than 32 characters) `serve` keeps sign-up off, answers it with 503 `signup_unavailable` and logs why; every other route keeps working. The key cannot be
  rotated without losing the trial history (every stored hash would stop matching), so treat it like a
  database secret. The IP hash is set to NULL once the row is 30 days old by a sweep that `serve`
  runs at start and every hour (`Service.ClearSignupAddresses`), not by sign-ups. The email hash
  stays for good, because it is what stops a second trial; it is a pseudonymous fingerprint kept
  under legitimate interest (fraud prevention). The privacy policy must say: we keep a one-way keyed
  fingerprint of your email to prevent repeated free trials. The client address is not logged at
  INFO; the log line carries the new workspace slug only.
- **Client address behind the proxy.** `TRUSTED_PROXIES` is a comma-separated list of CIDRs (or single
  IPs) of the reverse proxies whose `X-Forwarded-For` is believed; the default is empty, meaning the
  peer address is the client. When the peer is trusted, the header is read from the right and the
  first entry that is not a trusted hop is the client (entries to its left were written by the
  client). A malformed list stops `serve` at start. An empty list while sign-up would be enabled (cloud
  sign-in and billing both on) turns sign-up off the same way as a missing key: without it every sign-up shares the proxy's address and the
  fourth of the day is refused for everyone. IPv6 clients are bucketed by /64. On collie-01 Caddy
  runs on the host and reaches the container through Docker, so the peer is the gateway of the
  compose network that publishes `127.0.0.1:8080`. Trust exactly that address as a `/32` (read it
  with `docker network inspect`, for example `172.18.0.1/32`), not the whole network: then no other
  container on the network can forge `X-Forwarded-For`.
- **Catch-all domains and disposable mail defeat the one-per-email rule.** The key collapses `+tag`
  and Gmail dots only, so a custom domain with a catch-all address, or a disposable-mail provider,
  gives unlimited verified emails; the per-IP limit and the caps on mail and storage bound the damage.
  This is closed in Clerk, not in code: before go-live turn on Clerk's block of disposable email
  addresses and its sign-up bot protection, and keep them on. The go-live checklist in the trial plan
  lists both.
- **Workspace name.** 2 to 60 characters after trimming. `domain.DeniedBrand` first folds the name (lowercase, compatibility
  forms such as fullwidth letters, accents dropped, common Cyrillic and Greek look-alikes mapped to
  Latin), then splits it into
  words at every character that is not a letter or digit and refuses it (422 `name_not_allowed`) when
  consecutive words joined together equal a listed brand, so `PayPal Support`, `pay pal` and
  `G-O-O-G-L-E` are refused, while `Pineapple`, `Applebee` and `Googleplex` are not (a brand must be
  a whole word, never a substring). A real business named like a brand (`Apple Orchard`) is
  refused too: it contacts us and an operator creates the workspace with the CLI, which has no
  such check. Digit-for-letter spellings (`paypa1`) are not caught. The same check runs on
  `PATCH /workspace/slug` and on `sender_name` in `PUT /channels/email` when billing is on. For a sender
  name the operator's escape hatch is `collie email sender-name`, which skips only this check.
- **Slug.** Derived from the name (runs of hyphens collapse, so it can never start with the reserved
  `xn--`); every candidate is checked as a hosted label; if it is taken, reserved for mail (`support`, `help`) or empty it
  becomes `<base>-<4 hex>`, tried up to twice more, then 409 `slug_unavailable`.

### Deactivated agents

`agents.deactivated_at` (null = active) turns off every way in at the point where an agent is looked up, so
no path depends on remembering the flag: `Authenticate` joins on it (an old session of a deactivated agent
fails like a revoked one), `Redeem` refuses a pairing link of a deactivated agent and an invite whose address
belongs to one, `Pair` and the cloud sign-in lookup skip them. Deactivating also sets `revoked_at` on the
agent's sessions and unused links, so the data says the same thing as the checks. Reactivation does not
revive either; the agent signs in with a new link. Redeem takes `FOR SHARE` on the agent row, so a redeem
and a deactivation never interleave: whichever starts first finishes first, and a later redeem sees the flag.
The deactivation revokes links before it takes any other lock, because a redeem in flight holds its link and
then waits for the seat lock or the agent row; the other order deadlocks. Code: `store/agents.go`, `auth/links.go`.

Taking a role from someone also revokes the unused invites to that address that give it, so a pending
invite cannot hand the role back. Redeeming an invite as an existing member adds its roles to theirs.

Lock order for anything that assigns conversations: the agent row (`store.ValidateAssignee`, `FOR SHARE`;
deactivation uses `FOR UPDATE`), then the conversations in id order (`FOR NO KEY UPDATE`), then
`conversation_tags`. A bulk path must follow the same order.

The last-admin rule (409 `last_admin`) counts other active holders of the Admin role inside the
transaction, under the agent-seats advisory lock, so two admins taking Admin from each other at once cannot
both succeed. A role change takes effect for every session and token at once; only a session limited to the
Agent role (`sessions.role_id`) stays limited until the person signs in again.

### Verification traps

- Customize the Clerk session token (Clerk Dashboard, Sessions, Customize session token) with
  exactly these claims; the default token carries no email:

  ```json
  { "email": "{{user.primary_email_address}}", "email_verified": "{{user.email_verified}}" }
  ```

  The verifier rejects a token without `email_verified: true`, so omitting the second claim breaks
  every sign-in (it did on the hosted cloud until the claim was added). Use the verified primary
  email only.
- The web widget uses Clerk's combined sign-in-or-up flow (`withSignUp`), so an agent with no Clerk
  identity yet can sign in with an email code. This creates a Clerk user only. The server still
  admits the email only when it belongs to an agent; otherwise the exchange fails and the page shows
  "Cloud sign-in failed" with a retry button. The widget redirects Clerk back to the current page
  (`forceRedirectUrl`) so the exchange runs before any navigation; the default `/` would unmount it.
- JWT time checks allow at most 60 seconds of clock skew around `exp` and `nbf`. Keep host clocks
  synchronized; do not widen this window to hide clock problems.
- Public keys are cached by `kid`. An unknown `kid` triggers one JWKS refresh so normal key rotation
  does not interrupt sign-in. An unresolved key or refresh failure starts a 30-second cooldown to
  prevent repeated network fetches. A bad signature with a known `kid` does not trigger a refresh.
- `iss` and `azp` use exact string comparison. Keep the provider Frontend API URL separate from the
  Colliedesk origin, and list every real Colliedesk origin explicitly.
- The exchange writes an HttpOnly Colliedesk cookie. It therefore requires the same exact `Origin`
  check as other cookie-writing requests.

## Saved reply history and trash

- **Versions.** `saved_reply_versions` is append-only for the app role (`SELECT, INSERT`). Each write
  updates the reply row first, then inserts `max(version)+1` in the same transaction: the row lock
  orders concurrent writes of one reply, so numbers never collide. Restoring a version copies it into
  a new version (`restored_from_version`); history is never rewritten. The migration backfilled
  version 1 for every existing reply: kind `create` by its author when it was never edited, otherwise
  kind `baseline` with no person (who wrote the current text is unknown). Agent requests (removed) used to
  snapshot the version a restore applied (`saved_reply_before.restores`); old rows keep it.
- **Optimistic writes everywhere.** PATCH, DELETE (`?updated_at=`), restore-version and undelete all
  compare `updated_at`; delete and undelete bump it. A token's write carries the `updated_at` it read like a
  person's, so it never overwrites a version it did not see. Side effect: a Freshdesk-imported reply that was trashed and restored counts as edited, so `import
  undo` keeps it.
- **Trash.** Delete sets `deleted_at`; the title index is partial (`WHERE deleted_at IS NULL`) so a
  trashed title can be reused, and undelete then answers 409 `duplicate_title` instead of hiding the
  clash. The 500-reply limit counts trashed replies too, so create-and-delete loops cannot grow the
  trash without bound; versions and trash are listed in pages (at most 200). The daily `serve` sweep purges replies deleted
  more than 30 days ago (`PrivilegeSavedReplyPurge`); versions go by cascade, and the server logs the
  count. `deleted_at` comes from the store clock so tests move one clock.
- **Rollback.** The down migration refuses while a reply is in the trash, a reply has more than the
  backfilled version, or a request keeps a snapshot: rolling back would drop that history silently.
