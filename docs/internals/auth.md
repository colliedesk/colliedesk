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

An agent token is a `sessions` row with a non-null `scope` (`read`, `draft`, `send`); a null scope is
a person's full session. It reuses the session machinery on purpose: `label`, `last_seen_at` (the
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
- **Role.** A token always has `role_ceiling='agent'`, whoever creates it; a CHECK constraint
  enforces it. No tier can reach an admin-only permission.
- **Tiers are a route table.** `tokenTiers` maps `"METHOD pattern"` to the lowest tier allowed. A route
  missing from it is closed to every token, so a new route stays out of reach until someone lists
  it. `TestEveryRouteIsOpenToTokensOnlyFromItsTier` keeps its own hand-written copy of the table,
  fails until both agree, and checks that an allowed tier gets the route's success status. Posting a
  message is gated by the handler: a note needs `draft`, an outgoing reply needs `send`, and the 403
  names that tier.
- **Order of checks.** Authenticate (a token presented as a cookie is a 401) → `refuseToken` → role
  permission → CSRF → locked workspace. The tier check runs before the role check so a closed route
  answers `scope_insufficient`, not a bare `forbidden`.
- **What each tier means.** `read`: conversations, messages, time entries, attachments, contacts,
  tags and the team list. `draft`: also status, priority, assignee, applying and removing a tag,
  creating a tag, and notes. `send`: also outgoing replies, retrying a reply, renaming and deleting
  tags, and creating contacts and conversations. Changing a tier is one edit to `tokenTiers`.
- **Closed for every tier:** revoking sessions (a token could sign its owner out), creating tokens (a
  `read` token could mint a `send` token), invites, and the lists of sessions and invites and the
  email and web form channel settings, which triage does not need and which would widen what a
  prompt-injected agent can paste into a reply. `GET /agents` stays open: assignee names need it.
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
  cannot find a token (see the first item). Do not rely on a manual `UPDATE` of `sessions`.
- **MCP.** `/mcp` accepts a bearer only, authenticated by the same `Authenticate`, and each tool call
  runs through the REST handler with that bearer, so these tiers bind MCP tools without a second
  check (`docs/internals/mcp.md`). A new route a tool uses needs a `tokenTiers` row first.
- The CLI (`colliedesk token create`) resolves the workspace by slug through
  `PrivilegeWorkspaceBySlug`, like `pair`.

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

`CLOUD_AUTH_AUTHORIZED_PARTIES` is a comma-separated list of exact origins. The email claim defaults
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
  `PATCH /workspace/slug` when billing is on.
- **Slug.** Derived from the name (runs of hyphens collapse, so it can never start with the reserved
  `xn--`); every candidate is checked as a hosted label; if it is taken, reserved for mail (`support`, `help`) or empty it
  becomes `<base>-<4 hex>`, tried up to twice more, then 409 `slug_unavailable`.

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
