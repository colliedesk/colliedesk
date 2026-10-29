# Operator console internals

The server's configuration lives in the database (`internal/serverconfig`), so an operator changes it
without a deploy. The environment keeps only the bootstrap: what opens the database, the origin, where
the server listens and stores files, and `MAILBOX_TOKEN_KEYS`, which seals the secrets stored here and
cannot be sealed by itself. `DISABLE_TELEMETRY` and `DO_NOT_TRACK` stay readable, only to turn the
usage report off.

## Decisions

- **Versions are snapshots.** Each save writes every key set (about 50 at most) as a new
  `server_config_versions` row; restoring copies an old snapshot, secrets still sealed, into a new
  version. No diff chain to replay, nothing lost by a restore.
- **Old variable names stay the parsers' interface.** `Snapshot.Getenv` answers with the names the
  parsers always read (`polar.ConfigFromEnv`, `email.ConfigFromEnv`, …), so no parser changed and their
  tests did not move. `cmd/collie/config_build.go` `buildConfig` is the one place that turns a snapshot
  into what serve runs; the CLI checks every change with it before saving.
- **Secrets are sealed with their key name as context** (`server-config:<key>`), so a sealed value moved
  to another key does not open. `redactSecrets` replaces a validation error that repeats a secret's
  value with one that names the key: several parsers quote what they reject.
- **The first import is by group, all or nothing,** under one advisory lock
  (`colliedesk-server-config`), so overlapping deploys import once. A group the database already
  holds is skipped (the database wins); a group holding only the old compose file's defaults
  (`SMTP_PORT=587`…) imports nothing. A secret the database lacks is imported as soon as a key ring
  exists, even into a held group: before the ring it could not be stored (D8), and skipping it then
  would lose it when the operator removes the environment.
- **The sign-up hash key is imported before it is ever generated,** in the same transaction: a fresh key
  would forget who had a trial. A key shorter than 32 characters is left out (sign-up stays off) rather
  than replaced.
- **Polar products are add-only** (`polar_products`, trigger): `PlanOf` falls back to `Products.Retired`,
  every product off sale, so an old subscription keeps its plan.
- **`trusted_proxies` unset trusts no proxy,** as the unset variable did. The installer saves the
  value its compose file used to pass (loopback and the Docker bridge, `InstallerTrustedProxies`) when
  `.env` does not set the variable, through the same `collie config import`; an explicitly empty
  `TRUSTED_PROXIES=` imports nothing and keeps trusting none. `TRUSTED_PROXIES` is therefore not a
  "compose default" to ignore on import.
- **`collie config import FILE|--stdin` never generates the sign-up hash key.** Only the server's start
  (and `--from-env`) reads the real environment, where the key may still be.
- **The installer probes the image (`collie help` lists `config`) before replacing `compose.yaml`:** an
  older image would run without the settings the new compose file no longer passes.
- **Secret variables stay in the shipped compose files** (`${X:-}`): without a key ring they are still
  read from the environment (D8); once imported and removed from `.env` they pass empty.
- **The server never refuses to start over its configuration.** A version that does not build is marked
  `rejected` (once, with an audit line) and the last good one runs, else every default.

## Operator realm

Operators (`operators`, `operator_links`, `operator_sessions`, `operator_requests`, `operator_idempotency`;
`PrivilegeOperator`, `auth/operator.go`) are a realm apart from every workspace:

- Routes of the console carry `auth.PermissionOperator`; `authorize` hands them to `authorizeOperator`,
  which reads only the operator cookie (`colliedesk_operator`, `__Host-` over https, SameSite=Strict,
  12 hours) or an operator bearer. Sessions and tokens live in `operator_sessions`, workspace ones in
  `sessions`, so a credential of one realm finds no row in the other: 401 both ways
  (`TestOperatorRealmIsSeparate`). A browser session is never a bearer and a token never a cookie.
- Operator tokens (`cdop_`, hashed with their own domain) hold `auth.ScopeOperator`, which is not in
  `ScopeNames`: the workspace tiers are ordered, and `ScopeAllows`/`ValidScope` must never hand this one to
  a workspace token. Its permissions (`operator:read`, `operator_requests:cancel`, `operator_config:write`,
  `operator:manage`) preset to `operator`. The parity gate lists `operator` beside the workspace tiers: a
  vocabulary, no ceiling moved.
- `/mcp/operator` (`mcpserver.NewOperator`) registers only `Tier: operator` tools and takes operator
  tokens; `/mcp` never registers them, and `toolOpen` refuses them to any workspace principal.
- Writes with the cookie need `Origin` equal to the console's origin (`OPERATOR_ORIGIN`, else
  `APP_ORIGIN`): `app.` and `ops.` are the same site, so SameSite alone does not stop a cross-subdomain post.
- `OPERATOR_ORIGIN` is a bootstrap variable, not a setting: it decides the cookie, Origin and the way in,
  and a wrong value saved from the console would lock the console out. `operatorHost` sits outside the help
  center (which takes every unknown host, and would read `ops.` as a workspace slug); `ops`, `operator`
  and `console` are reserved slugs.
- Sign-in: a link is spent only by `POST /operator/auth/redeem` (the page's Continue), never on load. With
  an authenticator it opens a ticket (`operator_links` purpose `totp-ticket`, 10 minutes) that carries
  the key being enrolled, sealed, until a code proves it. Codes are RFC 6238 with the used step stored.
  Limits: 5 tries a minute per link or ticket, 60 per client address (only a second layer: wrong trusted
  proxies put every client behind one address), and 10 wrong codes lock the operator for 15 minutes.
- Cloud sign-in (`POST /operator/auth/cloud`, `OperatorService.CloudSignIn`): the bearer is the identity
  provider's session token, checked by the app's own verifier (`cloudauth`) built a second time with
  `Config.ForParty(operatorOrigin)`: the same issuer, keys and email claim, but `azp` must be the console's
  origin. `cloud_auth.authorized_parties` stays the app's, so a token issued to `app.` never opens the
  console and one issued to `ops.` never the app. Without `OPERATOR_ORIGIN` the two origins are one and so
  is the token; the hosted cloud cannot run so (`config doctor` error), and a self-hosted server in path
  mode still needs an operator's email and its authenticator. Every refusal (no operator, revoked, bad
  token of any kind) is the same 401 `operator_sign_in_refused`, and only a log line: anyone can make a
  verified identity, and `operator_audit` keeps every row for good, so a refusal never writes one.
- A cloud sign-in always ends at the authenticator, even with `operator.require_totp` off: `Reauth` lets a
  session without one through for 15 minutes because it assumes a fresh link from the shell, which a
  mailbox is not. It never replaces an authenticator (`--reset-totp` stays shell-only). The ticket's
  `created_by` (`cloud-sign-in`) makes the session's `operator.sign_in` line say `via: cloud`. Trap: from
  `collie operator add` (or an invite) until the first sign-in, whoever holds the mailbox enrolls the
  authenticator (trust on first use, as with a link sent by mail); add operators just before they sign in.
  A ticket ends the operator's tickets not used yet, and signing in ends every other: a ticket kept from
  that window could otherwise replace the authenticator the operator set up later (`endTickets`).
- The console's content policy forbids every other host and inline style; Clerk's form needs its Frontend
  API host, bot protection and inline styles. Only `/sign-in`, and only with a publishable key that names a
  host (`cloudauth.FrontendHost`), gets `signInCSP`, and the page names the key in
  `<meta name="colliedesk-operator-clerk">` (the console's host serves no `/api/v1/config`). A cloud sign-in
  ends with a full load and sign-out goes to `/sign-in?signed_out=1` by a full load, so no signed-in screen
  runs with the provider's scripts. The form is the app's `ClerkSession` in a lazy part with its own strings.
- The QR code is drawn on the server (`internal/qrsvg`, byte mode, level M, versions 1–10) so the console's
  first bundle carries no QR library; the key is always shown as text too.
- An operator token's call to an `alwaysAsk` route is filed in `operator_requests` (`fileOperatorRequest`,
  never `agent_requests`); a body with a secret's value is refused (`secret_in_request`) before anything
  is stored.
- Every operator POST takes an Idempotency-Key (`operator_idempotency`, a day; `operatorOnce`): the
  workspace `createOnce` keys by workspace, which an operator has none of. A repeat answers the first
  result without what was shown once (a token).

## Re-serve

`collie serve` is a process (`cmd/collie/generation.go` `process`: pools, attachment store, the in-app
import keys, one Cursor client, `httpapi.Carry`) running one generation at a time (`generation`: the API
and every worker built from one version). The `http.Server` and its listener never change; its handler is
a `swapHandler` (atomic pointer). `reloader` reads `server_config_state.desired_version` every 5 seconds
and on `LISTEN colliedesk_config`:

1. Same version and same products fingerprint: heartbeat only. A product change makes no version, so the
   poll compares `Fingerprint(products)` too.
2. The new version does not build: `rejected` (once), `last_error` on the process, the generation keeps
   running.
3. Only `Hot` keys changed: `httpapi.Hot.Store` and done (the API reads them through `API.hot()`).
4. The generation runs an in-app import (`importjobs.Worker.Running`): only the hot settings apply,
   `waiting_for` says why, and the next poll tries again (a red log after 6 hours). Stopping the
   generation would cancel the import.
5. Otherwise: build the new generation (starts nothing), stop the old one's workers (30 seconds at most),
   start the new ones, swap the handler, `config.apply` in the audit log.

`httpapi.Carry` keeps what a rebuilt API must not reset: every rate limiter, the export limiter and links,
the alert dedupe, the video upload gate, the Founding state, and the WaitGroup of GitHub sends (shutdown
waits for every generation's). Config-independent loops (daily sweep, AI retrieval, the image cache sweep,
the reloader) run at process level (`process.Go`); `TestServeStartsWorkersOnlyThroughAGenerationOrTheProcess`
fails on a bare `go` statement elsewhere in serve. The environment is imported only at process start.

AI prices and the monthly allowance, retention and telemetry apply by re-serve, not at once: the AI
service and the retention sweeper take them at construction. A few seconds later than "hot", but correct.

## Guards and trials

Guards are worked out from what a change does, by the server (`httpapi/operator-config-guards.go`
`configGuards`): turning billing on asks to type `billing on`, rotating its token does not. `POST
/config/check` returns them so the review shows them; PATCH computes them again and refuses a phrase that
does not match, so the console, the API and an approved token's request pass the same gates. A phrase never
comes from a secret's value. A count (workspaces billing would lock, workspaces retention would delete) is
read at PATCH time through `PrivilegeOperatorPreview` (counts only) and `retention.Service.Plan`; if it
moved, the phrase moved with it and the save is refused with the new number.

A token's request never carries what only the approver gives: `confirm` and `reauth_code` are dropped when
it is filed, and the approver types the phrases at approve time (checked before the request is claimed, so a
wrong phrase leaves it waiting).

A trial (D12) is a version with `status = 'trial'` and `trial_expires_at`. Only a console or approved
change of a trial key starts one; the CLI and imports never do (the installer's `collie config set
trusted_proxies=…` must not revert itself). While one runs the console and approved requests are refused
(`ErrTrialPending`); the shell (`set`, `reset`, `apply`, `restore`) and an import at start end it instead
(`trial.superseded`), because the shell is the way back when a trial locks operators out. Every process calls `RevertExpiredTrial` on each reload and before it serves; the
store's advisory lock lets exactly one write the `trial-auto-revert` version. The evidence the band shows
(distinct client addresses, cloud sign-ins) is counted in memory per process, only while a trial runs.

## Feature switches

`serverconfig/features.go` lists the features workspaces see that need the server's settings, each with a
switch `features.<name>` (no environment variable, not hot: a switch rebuilds the generation like the
settings it rides with).

- **Unset is "on once its settings are complete".** Nothing is written at upgrade, so no version, no
  migration and no import: what ran before still runs. Effective state = switch not `false` and nothing
  `missing`.
- **"Complete" is read from the snapshot's keys** (and `Ring()`), not from the parsers: `serverconfig` must
  not import `httpapi`, and whether a value is valid is `buildConfig`'s job already. Keep each feature's
  `missing` in step with its parser's `Enabled()`.
- **Turning on while incomplete is refused at the save, never in `buildConfig`** (`CheckSwitches`, called by
  the operator PATCH and check, the approve replay and `saveChecked`). A check in `buildConfig` would reject
  the whole version when a setting of a switched-on feature is removed, and the last good version would keep
  running: the change would silently not apply. Restores and imports are not checked; the effective state
  covers them.
- **Off closes entry points, never what keeps data whole.** `featureOfRoute` maps routes to features and
  `featureGate` answers `409 feature_off` before `authorize`, so an agent token is refused at once instead of
  filing an approval that could only fail. Reads, the ways out (disconnecting a mailbox, DELETE routes of
  Autofix, the billing portal), consent callbacks and the webhooks (GitHub, Polar) stay open; inbound mail
  is never refused. Billing closes only checkout and Founding.
  Parsed configs are not emptied: `Missing()` stays right and the key ring stays shared.
- **Gmail and Microsoft 365 off defer, not fail.** `emailoutbox` answers `channels.Defer` (no attempt spent,
  10 minutes, the reason in `last_error`, `reason_code` `mailbox_switched_off`) when the provider is switched
  off, and the same (`mailbox_not_configured`) when the server lost a setting a connected provider needs:
  a connection exists only where the provider once worked, so a missing setting is a provider off, and no
  reply fails for a setting. Queuing (`notReadyReason`) no longer fails such a reply either, except on a
  server that runs no email outbox (`OutboundPolicy.Worker` false: no SMTP relay and no provider set up, the
  same condition serve starts the worker on): nobody would keep it waiting, so it fails at once with the
  reason and `mailbox_not_configured`, and Retry sends it later. The worker is not started always: it would
  fail SMTP replies queued before the relay was removed, which wait today. The Microsoft
  keep-alive keeps running so grants outlive the switch.
- **Workspaces never read setting names.** `missing_settings` of the Gmail, Microsoft 365 and Autofix
  answers is always empty (kept for the contract); `off_reason` (`not_set_up`, `switched_off`) says why. The
  names are for the console, `collie config features` and the log.
- **Autofix off closes only what starts something** (`autofixStarts`: sending or retrying a fix, adding an
  installation or a repository, a connector or Cursor's key). Fixes already sent are still followed by the
  workers and can be cancelled, marked deployed or given their pull request.
- **Hosted addresses off withdraw the offer, not the address.** `hosted_address` stays for a channel whose
  support address is its hosted address (its setup reads it); new workspaces get none.
- **Billing off is limits off, not the cloud off.** `generation` and `httpapi.New` read
  `billing.Enabled() && !off`, for plan limits, retention, Founding and the AI allowance; the Polar client
  stays (its webhook keeps subscriptions in step). `hostedCloud()`, telemetry's realm and the console's
  `path_mode` severity read the configured settings, so a switch never changes what the server is.
- **Cloud sign-in off keeps the console's own verifier** (`operatorCloud`): only the app's exchange and
  sign-up close. The guard makes it a trial with a phrase, and the one-time link always opens the console.

## Traps

- A moved variable left in the environment is never read. It is logged at every start (error when it
  differs from the database, warning when equal) and `collie config doctor` exits 1; that is the only
  signal, by design (D9).
- `serverconfig` must not import `httpapi`: the operator routes in `httpapi` use the store. Validation
  that needs `httpapi` parsers lives in `cmd/collie` and reaches the API as a function.
- Tests that set `POLAR_*` and friends with `t.Setenv` test nothing now: write settings with
  `configure` (`cmd/collie/config_helpers_test.go`), which runs `collie config import --stdin --replace`.
- The end-to-end servers each get their own database and their settings through `collie config import`
  (`web/e2e/support/cli.ts`, `settingsFile`), the path an operator uses.
