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

## Agent tokens

An agent token is a `sessions` row with a non-null `scope` (`read`, `draft`, `send`, `admin`, or
`custom` with its own `permissions`); a null scope is a person's full session. It reuses the session machinery on purpose: `label`, `last_seen_at` (the
"last used" time, written at most once a minute), revoke. Code: `auth/token.go`, `auth/scope.go`,
`auth/tokens.go`, `httpapi/token-scope.go`.

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
- **Role.** A `read`, `draft` or `send` token has `role_ceiling='agent'`, whoever creates it; an
  `admin` token has `role_ceiling='admin'`. The CHECK `sessions_scope_role_ceiling` ties the two.
  `Authenticate` computes the effective role from the agent's current `role` and the ceiling
  (`CASE WHEN a.role='agent' OR s.role_ceiling='agent'`), and refuses a deactivated agent, so an
  admin token of a demoted creator acts as an agent and one of a deactivated creator is a 401, the
  same as their browser sessions. Only a session whose effective role is admin can create an admin
  token (`ErrAdminTokenNeedsAdmin`, 403 `admin_token_needs_admin`); the CLI checks the agent's role.
  A token never has more rights than the person, as they are now.
- **Every UI action is available to agents.** Every route in `Routes()` has a row in `tokenTiers`
  (`httpapi/token-scope.go`): a tier (`read`, `draft`, `send`, `admin`), `Approval` (an agent
  request for every tier), `PersonOnly` (deciding an agent request; `Why` says why) or
  `NotForPeople` (routes no session reaches: public pages, sign-in, provider webhooks). No other
  route is closed to tokens. Every route a token may call, directly or as a request, is in some MCP
  tool's `Spec.REST`, or in `mcpToolPending` (`mcpserver/agent_parity_test.go`) with its tracking
  issue. `TestEveryRouteIsClassifiedForAgentsAndCoveredByAnMCPTool` fails on an unclassified or
  stale row, a closed row of any other reason, a `PersonOnly` row without `Why`, an uncovered
  callable route, a stale `mcpToolPending` entry, and a tool that calls a closed route; its message
  names the file to edit. Ceilings: `maxClosedToTokens` and `maxMCPToolPending` are 0;
  `maxPersonOnly` is 2. Raising one needs the maintainer's written approval.
- **Permissions, with the tiers as presets.** A token holds permissions, `resource:action`
  (`auth/token-permissions.go`). Each permission names its preset, the lowest tier that holds it; a
  tier holds every permission of its preset and below, worked out on every call (`Principal.Allows`),
  so old tokens needed no data change and a later route of the send tier opens for every send token.
  A custom token is stored as `scope='custom'` with its own `permissions text[]` (CHECK
  `sessions_custom_permissions`); the list is validated in Go, not by the CHECK, so a new permission
  needs no migration. A custom token with an admin-only permission (preset `admin`) gets the admin
  ceiling and only an admin may create or approve one (`auth.NeedsAdmin`). Actions beyond CRUD name a
  different risk: `send`, `publish`, `export`, `share`, `purge`, `manage_credentials`, `manage_roles`,
  `revoke`, `invite`. The rule that keeps presets faithful: when routes of one resource had different
  tiers, they are split into actions, and `TestEveryRoutePermissionIsHeldByTheTierTheRouteHad` holds
  each route's permission to the tier it had. `kb_articles:publish` is checked in the handlers
  (`askUnlessHeld`, `kb-handlers.go`) on every change to what the help center shows; accepting or
  undoing an AI triage suggestion also needs `conversations:update`. A request keeps what its token
  held (`agent_requests.token_permissions`). The contract's `TokenPermission` enum is the Go list in
  its order (`TestTokenPermissionEnumOfTheContractIsTheServersList`), and the web form's copy is held
  to the contract by `token-permissions.test.ts`.
- **Permissions are a route table.** `tokenTiers` gives each route its permission (`grant`), a
  permission decided by the handler (`byHandler`), `alwaysAsk`, or `anyToken`. `gateToken` lets a
  token through when it holds the permission; without it, and on every `alwaysAsk` route, the call
  becomes an agent request (below). A route missing at runtime is still closed to every token.
  `TestEveryRouteIsOpenToTokensOnlyFromItsTier` keeps its own hand-written copy of the table, fails
  until both agree, and checks that an allowed tier gets the route's success status and a lower one
  gets 202. Posting a message is gated by the handler: a note needs `draft`, an outgoing reply
  `send`; below that the JSON call becomes a request (a reply becomes a reply draft). The multipart
  form (a message with files) is refused with 403 `files_need_approval` instead: a token below
  `draft` before a file is read, a reply below `send` once the kind is read (the stored files are
  discarded).
- **Order of checks.** Authenticate (a token presented as a cookie is a 401) → `gateToken` (202
  agent request, or 403 `scope_insufficient` / `person_only`) → role permission → CSRF → locked
  workspace. The tier check runs before the role check because a token below `admin` has the agent
  role: a request for an admin route must be filed for an admin to approve, not refused as
  `forbidden`. An `admin` token of a demoted creator passes the tier check and is then refused by
  the role (403 `forbidden`).
- **What each tier means.** `read`: conversations, messages, time entries, attachments, contacts,
  tags, saved replies, the caller's inbox activity and the team list. `draft`: also status, priority,
  assignee, applying and removing a tag (on one conversation or in bulk), creating a tag, and notes. `send`: also outgoing replies, retrying a reply, renaming and deleting
  tags, creating contacts and conversations, and writing saved replies (create, edit, delete to the
  trash, restore a version, undelete). Saved replies are text a person later sends to customers, so
  they sit at `send`; `read` and `draft` propose through agent requests. Their routes on one reply
  snapshot the reply into `agent_requests.saved_reply_before` when filed (and refuse a missing or
  wrong-state reply with 404 instead of filing), so the Requests card shows a correct before/after
  even after the reply changes; the replayed PATCH still carries the token's `updated_at`, so a
  stale proposal fails with 409 instead of overwriting. Each change is a row in
  `saved_reply_versions` naming the person (the approver for a request), the token and the request. Changing a tier is one edit to `tokenTiers`.
  `read` also reads the session and invite lists and the email channel and web form settings: none
  of those answers holds a token, an invite link or the inbound secret (`TestTokensReadSettingsWithoutLinksOrSecrets`
  scans them). The role still applies: an agent-role token lists only its owner's sessions.
  `draft` also sets the owner's own timezone. `admin`: workspace timezone, team member role and
  active state, revoking invites and sessions, the web form, the email
  channel and domain verification, and `GET /billing` (admin-only by role, so it is classified
  `admin` and a lower tier files a request instead of a role refusal).
- **Approval for every tier, `admin` included:** creating tokens, creating invites, export, billing
  checkout and portal, disconnecting the email channel, rotating its secret, changing the slug
  (`Approval` rows). The handlers file the rest themselves when the caller is a token
  (`fileGrantRequest`): `updateAgent` when the change asks for the admin role or the target is an
  admin now (`touchesAdmin`: promoting, demoting, deactivating, and reactivating an admin, which
  would hand the role back; a `display_name` change alone is not about the role, so an admin token
  renames an admin directly), and `revokeSession` when the target is a person's sign-in (not a token)
  of anyone other than the token's own person.
- **Credentials never reach a token.** Creating an invite is `Approval`, so a token never gets a
  `201`; the approved call's answer is shown once to the approver and not kept (`withheldResults`),
  so the token reading its request does not get the link either. A settings request
  (`settingsFields` in `agent-request-settings.go`) is refused with 400 when its body is not a JSON
  object or has a field the route does not take, so nothing pasted beside the real fields is stored
  for days. Its summary says what changes, from what to what (`summarizeSettingsRequest`); a session
  revoke names the session only when the token's role could list it.
- **Owner requests.** A `/me` route (`ownerRoutes`: `PATCH /agents/me/timezone`, `PATCH /agents/me/display-name`) replays as the
  approver, so only the person who owns the token may approve it (`approvable_by: owner`, 403
  `owner_only` for anyone else); otherwise an admin approving would change the admin's own timezone or name.
- **Revoking sessions from a token** is allowed at `admin` for agent tokens and the token person's
  own sign-ins (security-positive: a leaked token can be cut off by an agent). The session list shows
  the revocation; `revokeSession` also logs the token session that did it.
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
  returns it as `agent_token` (`id`, `label`, `scope`, `agent_name`). The web shows "by Ada via
  Claude Code". The column stays after the token is revoked, so what a revoked token wrote can still
  be told from what a person wrote. **Known limit:** only notes and replies are attributed. Status,
  priority, assignee and tag changes, the creation of conversations, contacts and tags, tag rename and
  delete, and retrying a reply record the agent but not the token. The repo has no conversation
  change log (a person's change is not attributed either); adding one is the way to close this.
- **Rollback.** An image older than the migration needs nothing from the operator any more: it
  cannot find a token (see the first item). Do not rely on a manual `UPDATE` of `sessions`. The down
  migration of the admin tier revokes every admin token and stores it as `send` with the agent
  ceiling, and records the admin token's requests under `send`, so the older constraints hold. This
  loses the audit fact that those tokens and requests were `admin`; it is an accepted rollback cost
  (rollback is not the production recovery path, and the tokens are revoked so nothing they could do
  survives).
- **MCP.** `/mcp` accepts a bearer only, authenticated by the same `Authenticate`, and each tool call
  runs through the REST handler with that bearer, so these tiers bind MCP tools without a second
  check (`docs/internals/mcp.md`). A new route a tool uses needs a `tokenTiers` row first.
- The CLI (`collie token create`) resolves the workspace by slug through
  `PrivilegeWorkspaceBySlug`, like `pair`.

## Agent requests

A call an agent token may not make alone is stored as an agent request a person approves (#127);
a reply from a token below `send` is the first kind, the reply draft (#114). Code:
`httpapi/agent-request-*.go`, `store/agent-requests.go`, table `agent_requests` (RLS like every
workspace table). User page: `docs/user/agent-requests.md`.

- **Filing.** `gateToken` (or `createMessage` for a message) answers 202 with `request_id`,
  `kind`, `status`, `summary`, `expires_at`. The row keeps method, route pattern, concrete path,
  the JSON body (at most 64 KiB), the token id, label and tier, and the person who owns the token.
  Only JSON or empty bodies are filed; multipart is 403 `files_need_approval` (files would sit in
  storage for days, and a request must replay byte for byte). A request on a conversation route
  checks the conversation exists in the workspace first, so another workspace's id is 404, not a
  request. The summary is built from the route and the body the token sent, never from customer
  text, because it reaches the model too; a reply draft's summary is the exception that names every
  recipient (contact and cc), so a copy is never hidden behind the text. A token below `send` may
  copy only addresses already on the conversation (`store.ConversationParticipants`: contact, mail
  senders and copies, earlier reply copies), so a prompt-injected draft cannot add an outside
  reader that a busy approver skims past. A token may have 50 pending requests and file 100 in any
  rolling hour (429 with `Retry-After`; `agent_requests_token_idx`). A locked workspace files
  nothing (402): its requests could not be approved.
- **Approving is a replay as the approver.** Before anything changes, the approver's role is checked
  against the permission of the route the call runs on (`approvalPermission`); a person below it gets
  403 and the request keeps waiting for someone who may (`approvable_by` in the API, the UI hides
  Approve). The whole approval (claim, replay, result) runs on `context.WithoutCancel` with a
  two-minute timeout, so a closed tab never leaves a request claimed without a result
  (`TestApprovalFinishesWhenTheApproverLeavesMidway`). `ClaimAgentRequest` locks the row (`FOR UPDATE`),
  refuses anything not pending (409 `already_decided`) or from a revoked token (409
  `token_revoked`), and commits `approved` before the call runs, so a double click runs it once
  (`TestDoubleApproveRunsTheCallOnce`). `replayApproved` then builds an in-process request and
  serves it through the API's own mux with an unexported context key that carries the approving
  principal: `authorize` skips credential, gate and CSRF (all done on the approve call itself) and
  applies the role permission and the lock as for that person. A non-2xx answer marks the request
  `failed` with `result_status` (an agent approving an admin action: 403). The trade-off: a crash
  between the claim and the result leaves `approved` with no `result_status`; we prefer that to
  holding a row lock across a call that may send an email.
- **What is kept.** The answer is stored up to 64 KiB, except for routes in `withheldResults`
  (new token, inbound secret, export link, billing pages): the approver gets it once in the approve
  answer, the row keeps `result_status` and a `result_note`. A credential or a payment page must not
  reach the token that asked, nor sit in the database. `GET /api/v1/export` is replayed as
  `POST /api/v1/export/link` (`approvalRunsAs`): a streamed archive in a request record would be
  useless; the approver gets the same download the settings page uses.
- **Edits.** The approve call may carry a reply draft's new text and the subset of its copies to
  keep (`editedReply`; a copy the draft did not ask for is 400). The edit is stored in `edited_body`
  and is what runs; `body` stays the agent's draft.
- **Attribution.** A reply sent by approving a draft has `author_agent_id` = the approver,
  `agent_token_id` = the token that drafted it (only when the approver did not change the words: an
  edited text is the person's, so the token is not shown as its writer) and `agent_request_id` = the
  request (composite FK,
  added `NOT VALID` and validated by the next migration). Other approved actions are recorded on the
  request only (`requested_by`, `decided_by`); they are not attributed in a timeline, the same known
  limit as above.
- **Retention and payload.** A decided request keeps its `body`, `edited_body` and `result` for 90
  days; `serve` clears them at start and daily (`PruneAgentRequests`, privileged path
  `PrivilegeAgentRequestPrune`), keeping the audit row. The list every tab polls returns no bodies
  (`detail=full` adds them; `GET /agent-requests/{id}` always has them). Export writes
  `agent-requests.ndjson` (the codebase's NDJSON naming, like `saved-replies.ndjson`) without
  `result`, and messages carry `agent_request_id`.
- **Expiry.** Seven days. Reads (list, get, every decision) first mark the workspace's overdue
  pending rows `expired`, in the same transaction; there is no sweeper. A decided request never
  runs again.
- **Who sees what.** A token lists and reads only its own requests (another token's id is 404); a
  person sees the workspace's. Approve and reject are `PersonOnly`: no token can decide, so an agent
  never approves its own request. Cancel is open to the token that asked and to the person who owns
  it; another person rejects instead.
- **Rollback.** The down migration refuses while any request exists, like saved replies: requests
  are the record of what agents asked and who approved it.

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

Demoting an admin to agent also revokes the unused admin-ceiling invites sent to that address, so a pending
invite cannot hand the role back. Inviting an existing agent still changes their role on redeem, as before.

Lock order for anything that assigns conversations: the agent row (`store.ValidateAssignee`, `FOR SHARE`;
deactivation uses `FOR UPDATE`), then the conversations in id order (`FOR NO KEY UPDATE`), then
`conversation_tags`. A bulk path must follow the same order.

The last-admin rule (409 `last_admin`) counts other active admins inside the transaction, under the
agent-seats advisory lock, so two admins demoting each other at once cannot both succeed. A role change takes
effect for new requests at once, but an existing session keeps the role ceiling its link gave it; the person
signs in again to use a higher role.

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
  kind `baseline` with no person (who wrote the current text is unknown). A restore request snapshots
  the version it applies (`saved_reply_before.restores`) so the card shows the target text.
- **Optimistic writes everywhere.** PATCH, DELETE (`?updated_at=`), restore-version and undelete all
  compare `updated_at`; delete and undelete bump it. A stored agent request keeps the token's
  `updated_at` in its path or body, so approving it after anyone changed the reply answers 409 and
  changes nothing. A delete proposal without `updated_at` is refused with 400 before it is filed.
  Side effect: a Freshdesk-imported reply that was trashed and restored counts as edited, so `import
  undo` keeps it.
- **Trash.** Delete sets `deleted_at`; the title index is partial (`WHERE deleted_at IS NULL`) so a
  trashed title can be reused, and undelete then answers 409 `duplicate_title` instead of hiding the
  clash. The 500-reply limit counts trashed replies too, so create-and-delete loops cannot grow the
  trash without bound; versions and trash are listed in pages (at most 200). The daily `serve` sweep purges replies deleted
  more than 30 days ago (`PrivilegeSavedReplyPurge`); versions go by cascade, and the server logs the
  count. `deleted_at` comes from the store clock so tests move one clock.
- **Rollback.** The down migration refuses while a reply is in the trash, a reply has more than the
  backfilled version, or a request keeps a snapshot: rolling back would drop that history silently.
