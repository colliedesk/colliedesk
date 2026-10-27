# Autofix (bridge version)

A person sends a conversation to a coding agent the workspace already uses, on a private GitHub
repository connected through the Colliedesk GitHub App, and follows it to a pull request, its merge and
the release. Colliedesk runs no agent and keeps no model key; the only vendor key it keeps is Cursor's,
sealed. This page records decisions and traps; what people see is in `docs/user/`.

## One fix attempt, one set of rules

- States, connectors and outcome reasons live in `server/internal/autofix/states.go`. The migration's
  CHECKs and the contract's `FixState`, `FixConnector` and `FixOutcomeReason` hold the same lists;
  `TestTheDatabaseAndTheContractNameTheSameStatesAndReasons` fails when one drifts. A branch that adds a
  reason edits all three.
- Every signal (GitHub webhook, Cursor poll, the reconcile pass, the worker's clock) goes through
  `store.ApplyFixObservation`, which locks the row, asks `autofix.Decide` and records the event. A
  signal never fails: it moves the fix, is only noted on the timeline, or is ignored. Signals move
  forward only; an old one arriving late is ignored.
- A person's change (cancel, link or unlink a pull request, mark or unmark the release) goes through
  `store.ActOnFix` and `autofix.Act`, which refuse a move the state does not allow (409). The vendor
  side of a cancel (closing the issue, cancelling the Cursor run) runs after the transaction commits.
- One fix in progress per conversation (`fix_attempts_one_in_progress`). A reopened issue or pull
  request brings an ended fix back only while the conversation has no newer one in progress;
  otherwise it is only noted.
- `authorization_abandoned`, `authorization_denied` and `connector_disabled` (an admin turned the agent
  off, withdrawing the consent) end only a fix still `sending`, whose content has not left Colliedesk;
  once sent they are only noted, because the vendor already has it.
- A person's cancel is never undone by a signal; an issue closed while a pull request is open is not a
  cancellation (the pull request decides); an issue closed by a merged pull request whose own webhook
  was missed is a merge.
- `stale` (sent, no sign of the agent for 30 minutes) and the Fixes screen's `group` are computed in
  SQL on read; nothing stores them.

## Data

- `fix_attempt_events` is append-only: a trigger refuses UPDATE and the app role holds no UPDATE or
  DELETE. Events go only by cascade with their fix (cascades run as the owner).
- Conversations have no trash: they are deleted only by a Freshdesk import undo, and their fixes go
  with them by cascade, `sent_body` included. `conversation_deleted` stays a reason for a later trash.
- Disconnecting a repository keeps its fixes: `repo_id` becomes null and `repo_full_name` keeps the
  name it was sent to. Removing the installation keeps its repositories, paused.
- `autofix_webhook_deliveries` is platform level (no workspace, no grant to the app role); only a
  privileged transaction reads it.
- The export carries installations, repositories, connectors (only whether Cursor's key is set) and
  every fix with exactly what it sent and its timeline.

## GitHub

- One GitHub App per server (`server/internal/github`, plain `net/http`, no SDK). Its private key stays
  in the environment; installation tokens live in memory for under an hour. `githubtest` is the
  stand-in every test and the e2e build use (`COLLIEDESK_E2E_GITHUB_URL`); `cmd/github-stand-in` serves it
  for a browser, with `POST /stand-in/*` to arrange GitHub's side and deliver signed webhooks.
- Installing: GitHub's redirect names `installation_id`, and anyone can type another one. The callback
  takes the state first (one use, the same person in the same workspace), trades the code for the
  person's user token, keeps the installation only if `GET /user/installations` of that token lists it,
  and revokes the token before anything else happens. `installation_id` is unique on the server, so
  one installation never joins a second workspace (`installation_in_use`).
- Unchecked against real GitHub (smoke P0): the flow relies on GitHub handing the `state` of
  `/apps/<slug>/installations/new?state=` back to the callback when the App requests user authorization
  during installation. If it does not, every install answers `expired`. The fallback keeps every check:
  the callback, finding no state, sends the admin through `/login/oauth/authorize` with a fresh state
  and PKCE (purpose `install`, the `installation_id` carried in that state), and the same
  `GET /user/installations` check runs on the token that comes back.
- `repository_selection = all` is stored `blocked_all_repositories`: GitHub cannot make an admin pick
  selected repositories, so Colliedesk refuses to connect any until the installation is narrowed, and
  unblocks it when the webhook or the reconcile pass sees `selected`.
- The webhook has its own verifier (`github/githubsig`: hex HMAC-SHA256 of the raw body), not
  `standardwebhooks`. GitHub signs no timestamp, so a replay is stopped by `X-GitHub-Delivery`, recorded
  only after the event was applied (a failed delivery is applied again when redelivered; every move is
  idempotent). An event acts in the workspace that owns its installation, and on a repository only when
  it is connected there through that same installation.
- A pull request finds its fix by its number (every connector), then by branch (Cursor), then by the
  issues it closes (`closingIssuesReferences`), then by Claude's branch `claude/issue-<N>-`. An issue
  closed while its fix waits asks GitHub which pull requests close it: a merged one is the merge whose
  own webhook was missed.
- `githubsync.Service.Reconcile` runs every ten minutes for a server GitHub cannot reach: installation
  gone, suspended or narrowed; repositories left the App or turned public; the issue or pull request of
  every fix past `sending` still in progress. `sending` belongs to the senders (the issue connectors'
  expiry, the Cursor worker).
- A repository that turns public is paused and its fixes still `sending` fail (`repo_public`); a fix
  whose issue exists already keeps going (the issue was created while it was private).
- `githubsync.Service.CheckRepo` (`autofix.RepoChecker`) reads a repository from GitHub now, before a
  fix leaves; it pauses a repository no longer private on the way.
## Cursor and the worker

- Everything Colliedesk knows of Cursor's Cloud Agents API v1 (public beta) is in
  `server/internal/cursor/client.go`; `cursor/cursortest` stands in for it in tests (and e2e). Cursor
  v1 has no webhook yet, so fixes sent to Cursor are polled.
- The key is sealed with `MAILBOX_TOKEN_KEYS` under the context `autofix:cursor:<workspace id>`: a
  sealed key copied into another workspace's row does not open. It is tried (`GET /v1/me`) before it
  is kept, so a wrong key never replaces a working one; nothing of it is shown again, not the last
  characters. `PUT …/cursor_api/key` is a credential route: a token below admin is refused, never filed,
  because an agent request keeps its body.
- `server/internal/autofixworker` runs the fixes that wait on Colliedesk. `store.ClaimDueFixes`
  (`PrivilegeAutofixClaim`) leases due fixes in progress by moving `next_check_at` a lease (2 minutes)
  ahead, fixes still to send first; each is then handled in its own workspace with no transaction
  across the vendor call. A fix leaves Cursor's list (`next_check_at` null) once its run ends; GitHub's
  webhooks and the reconcile pass follow the pull request from there.
- Sending: the repository is read on GitHub right before (`autofix.RepoChecker`, implemented by the
  GitHub connection: private, still reached; a repository turned public is paused by the checker). The
  agent id is `bc-<fix id>`, so a second create (a process that died after it, a lease that ran out)
  answers 409 `agent_id_conflict` and is read back: a fix is never sent twice. No `envVars` (Cursor
  refuses them with a client id). The repository address always comes from `autofix_repos`, never from
  the prompt. A repository unreachable only because its installation is suspended makes the fix wait
  (it resumes when unsuspended); any other unreachable repository fails it `no_repo_access`.
- Reading: every 15 seconds for 5 minutes, every minute until 30, then every 5 minutes. A pull request
  counts only when it is one of the fix's repository (`autofix.ParsePRURL`). Cursor finished without a
  pull request, or failed, is `failed/agent_error` with Cursor's reply cut for the timeline.
- Refusals: 401 fails the fix `vendor_auth` and marks the key `key_invalid`; 403/404 or
  `repository_access` on the create `vendor_repo_access`; another 4xx `vendor_rejected`; 429 waits what
  Cursor asks without counting; 5xx and the network are tried 5 times with a growing wait, then
  `provider_unavailable`. A key removed while a run goes on is waited for (every 5 minutes); the run
  goes on at Cursor.
- Budget: Cursor allows a key 20 calls a minute by default; the worker keeps to 10 per workspace, so a
  person's check or cancel always has room. The budget is per process (`autofixworker.Cursor`, shared
  by the API and the worker of one process through `httpapi.Config.Cursor`).
- `collie serve` builds one `autofixworker.Cursor` for the API and the worker. The worker runs only with
  the GitHub connection's `RepoChecker`: without one nothing is sent to Cursor (the repository cannot be
  checked private), the fix waits. A person's calls from the settings (check, set the key) are limited to
  5 a minute per workspace (429).
- Turning a coding agent off withdraws its consent: a fix still `sending` (nothing left yet) ends
  `cancelled/connector_disabled` and is not sent, also when its consent is to an older text. Fixes already
  sent go on. The issue connectors apply the same rule before they create the issue.
- A person cancelling while the fix is being sent: the worker's `sent` signal is only noted (a person's
  cancel stands) and the worker cancels the run it just got.

### Sending and cancelling a Cursor fix

- `POST …/fixes` and `…/retry` for `cursor_api`: the agent on with the current consent and a key set
  (`cursor_key_missing`), `store.CreateFix`, then `store.ScheduleFixCheck(now)`; `authorize_url` is null and
  the worker sends it. The same `client_key` answers the same fix without scheduling it again.
- `POST …/cancel`: `store.ActOnFix`, then, after it committed, `autofixworker.Cursor.Cancel`; a run that
  could not be stopped (`ErrStopOnCursor`) is logged, and the fix's `vendor_url` is where to stop it.
- The worker handles only `cursor_api` fixes; the issue connectors' `sending` expiry runs in the reconcile
  pass (`githubsync`).

## Sending as the person (issue connectors)

- The send stores the fix `sending` with exactly the reviewed title and body, masked again on the server
  (`autofix.Mask`, the same code as the preview; masking twice is masking once). It answers an
  authorization address with a one-use state and a PKCE S256 verifier (`autofix_oauth_states`, purpose
  `dispatch`). The same `client_key` while the fix still waits answers a new address: the person may
  come back from GitHub.
- The callback takes the state first (one use, the same person in the same workspace; another person of
  the workspace gets `wrong_person` and the fix waits), trades the code for the person's user token and
  answers the redirect at once: GitHub calls in a row would break the 200 ms budget. A goroutine of the
  process, detached from the request, with a 60 s timeout, holds the token in memory only and revokes it
  when it ends, whatever happened (`defer`). Nothing of it reaches the database, a log or a job row; the
  API tests scan every row for `ghu_` after each path.
- `githubsync.Service.Dispatch`, in order, before anything is written on GitHub: the person's
  `permissions.push` on the repository read with their token (Claude's workflow and Copilot both need a
  writer to start them); for Copilot in `assign_api` mode, `suggestedActors` with their token holds
  `copilot-swe-agent` [CK S8]; the repository private now (`CheckRepo`, installation token); then
  `store.StartFixDispatch` re-reads, under the row lock, that the fix still waits and that its agent is
  on with the current consent (else `cancelled/connector_disabled`), and records `dispatch_started_at`.
  Only then the issue is created, its body ending with the marker `<!-- colliedesk-fix:<id> -->`.
- Claude: one `@claude` comment as the person starts the workflow (`githubsync.ClaudeTrigger`). It is
  the only live mention a fix carries; a workflow that listens to `issues.opened` or `issue_comment`
  runs once [CK S1]. Copilot: assigned as the person (`copilot-swe-agent[bot]`) [CK S2]; refused or
  left out by GitHub, the issue stays and the fix says `copilot_assignment = on_github`, never failed:
  the issue exists.
- `store.RecordFixIssue` stores the issue and the GitHub login with the move to `sent`. A person who
  cancelled meanwhile keeps the cancel (the move is only noted) and the new issue is closed.
- A failed create: 404, 403 or 410 is `no_repo_access`, 422 `vendor_rejected`; any other answer may have
  created it, so the fix waits for the expiry, which looks for the marker.
- Expiry: the reconcile pass (every ten minutes, scoped per workspace, no privileged lookup of its own)
  ends a fix of an issue connector still `sending` 15 minutes after it was stored, so 15 to 25 minutes;
  a person cancels at once. Never confirmed: `cancelled/authorization_abandoned`. Started
  (`dispatch_started_at`): the repository's issues since then are read as the installation for the
  marker; found, the fix is recorded `sent`; not found, `failed/interrupted`. An issue is never created
  twice.
- A cancel closes the fix's issue as the installation (`issues:write`) with a comment, after the cancel
  committed; the agent may still run, the fix links the issue. GitHub refusing it leaves the cancel and
  an `issue_close_failed` event on the timeline (the person closes it there); nothing retries it. The App cannot write pull requests, so an
  open one stays.
- Marking a release again, or taking back one already taken back, answers the fix as it is (idempotent).
- `[CK]` (no smoke test with real accounts yet, the stand-in follows GitHub's documentation): S1 the
  issue and comment created with a user token pass `claude-code-action`'s human actor check; S2 Copilot
  assignment with a user token of an App with these three permissions; S3 GitHub skips its confirmation
  for a person who authorized before; S4 revoking the token at once leaves the issue and the agent
  running; S5 Claude's pull request names the issue (`closingIssuesReferences`) or its branch
  `claude/issue-<N>-`; S8 `suggestedActors` with a user token.

## How the work was split

The first branch shipped the schema, the rules, the store, the read routes and their MCP tools, the
export and the whole contract of the first release. Each later branch registers its own routes with
their `tokenTiers` rows and MCP tools, and adds its own token permissions with them
(`autofix:update`, `autofix:send`, `autofix_settings:*`): `TestEveryRoutePermissionIsHeldByTheTierTheRouteHad`
refuses a permission that opens no route, and that gate is not loosened. The privileged lookups
(installation → workspace, the worker's claim) come with the code that calls them, with their row in
the table of `workspace-isolation.md`.
