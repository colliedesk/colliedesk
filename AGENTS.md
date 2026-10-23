# Colliedesk

Colliedesk is an open-source helpdesk for small support teams (1–10 agents) leaving Freshdesk.
A Go server (PostgreSQL) serves a JSON API under `/api/v1` and an embedded React + TypeScript
web app. HC Labs sells the hosted cloud and migrations; self-hosting is free.

## What we never compromise on

1. **No data loss.** A migration or an edit never drops a ticket, reply, attachment, tag or
   timestamp silently. Every import ends with a reconcile report. A partial success is a failure.
2. **Workspace isolation.** No query, filter, search, id or file can cross from one workspace to
   another. Every new endpoint ships with a cross-workspace test.
3. **Fast.** A person waits at most 200 ms from action to usable screen. Budgets and gates are in
   `docs/internals/performance.md`. Never loosen a budget to pass CI.
4. **No lock-in.** The data model and the export format stay open and documented. Never add a
   feature that only works on the hosted cloud when it could work self-hosted.
5. **Open at the core.** Roadmap, decisions and code are public. Write as if a stranger will read
   it tomorrow.
6. **Agents can do what people can do.** Every action in the web app is available to an AI agent:
   through an API route classified for a token tier in `server/internal/httpapi/token-scope.go`
   and an MCP tool, or as a request a person approves. A new feature ships its route
   classification and its MCP tool in the same PR; a gap is listed with its tracking issue
   (closed rows in `tokenTiers`, `mcpToolPending`) and those lists only shrink (ceilings in the test).
   `TestEveryRouteIsClassifiedForAgentsAndCoveredByAnMCPTool` fails otherwise, and
   `TestEveryAPIPathTheWebAppCallsIsARoute` keeps the web app on those routes.

## Glossary

- **workspace**: one customer team; every row belongs to exactly one.
- **agent**: a person on the support team who signs in. Roles: `admin`, `agent`.
- **contact**: an end customer who writes in.
- **conversation**: a ticket; has status, priority, assignee, tags.
- **message**: an item on a conversation timeline: `outgoing` reply or private `note`
  (`incoming` arrives with the email channel).
- **pairing link / invite link**: a one-time sign-in URL; stored only as a hash.
- **AI agent**: an external model client (Claude, Codex) working the queue through MCP. Not the
  same as a support agent.

## Three ways to hurt yourself

1. Writing a query without `workspace_id` in the filter. The tests will catch most of these;
   do not rely on that.
2. Changing the API without changing `contracts/openapi.yaml` first. The contract is the single
   source; web types are generated from it and CI fails when they drift.
3. Adding a one-way door: an action with no reverse (close without reopen, add without remove,
   invite without revoke) or no way to see its state.

## Hit every surface

- **Entry points:** a feature reachable in the UI is also reachable through the API and through
  MCP with the right token tier (item 6 above; the gate test names what to edit).
- **Reverse states:** every way in has a way out and a way to see it.
- **Contracts:** anything crossing the wire is typed in `contracts/openapi.yaml`.
- **Docs:** a user-visible change updates its page in `docs/user/` (server operation: `docs/self-host/`). Internal docs record decisions
  and traps only (`docs/internals/`).

## Verifying

- Tests first (red → green), at the public seams: HTTP API, CLI, web components.
- Go tests use a real PostgreSQL (`TEST_DATABASE_URL`); never mock the database.
- `make test` before pushing; `make e2e` for user-visible flows.
- Market is international: timestamps are stored and returned in UTC; the UI formats them in the
  agent's IANA timezone. No country-specific defaults.

## Onboarding journeys

Customers leaving Freshdesk judge Colliedesk in their first hour. These end-to-end tests run the
real server, CLI, PostgreSQL and a browser; only external services are local stand-ins (Freshdesk
API, identity provider JWKS, Mailpit as the SMTP relay). They run on every PR in the CI jobs
`onboarding` and `install-smoke`. A change to an onboarding surface (`install.sh`,
`create-workspace` / `pair`, redeem, invites, sessions, cloud sign-in, the email channel,
`import freshdesk` / `import undo`, the empty inbox) must keep them green. Never skip, weaken or
untag one; fix the product instead. `corepack pnpm test` fails when this list and the
`@onboarding` tests in `web/e2e/` differ.

- `self-host first run: fresh database, create-workspace, pairing link, empty inbox that explains the next step` (`web/e2e/onboarding-self-host.spec.ts`)
- `invite teammate: admin invites, teammate joins under the agent ceiling, admin revokes, revoked session refused` (`web/e2e/onboarding-invite.spec.ts`)
- `email channel: forwarded mail opens a conversation, the reply is delivered by SMTP with threading headers, the customer reply threads back` (`web/e2e/onboarding-email.spec.ts`)
- `freshdesk import: reconcile OK with exact counts, tickets searchable with original timestamps, undo removes exactly the import` (`web/e2e/onboarding-freshdesk-import.spec.ts`)
- `freshdesk import: an account without canned responses still migrates its tickets and says so` (`web/e2e/onboarding-freshdesk-import.spec.ts`)
- `freshdesk import: a wrong API key stops before writing anything and says why` (`web/e2e/onboarding-freshdesk-import.spec.ts`)
- `freshdesk import: an archive refused with 403 is reported UNVERIFIED, never OK` (`web/e2e/onboarding-freshdesk-import.spec.ts`)
- `in-app freshdesk import: an admin reads the account, imports it in the background, reconcile OK with exact counts, and undoes it` (`web/e2e/onboarding-in-app-import.spec.ts`)
- `cloud sign-in: a verified identity of an existing agent gets a Colliedesk session; every other token is refused` (`web/e2e/onboarding-cloud-sign-in.spec.ts`)
- `cloud signup: a verified identity starts a trial, the same email cannot start a second one, the expired trial is locked and still exports` (`web/e2e/onboarding-cloud-signup.spec.ts`)
- `golden path: a new team signs up on the cloud, imports Freshdesk in the app, reconcile OK, answers its first conversation by email` (`web/e2e/onboarding-golden-path.spec.ts`; #99 adds its step to this test)
- `link errors: expired, reused, revoked, mistyped, missing and another workspace's link explain themselves` (`web/e2e/onboarding-link-errors.spec.ts`)

Also: `tests/install-smoke.sh` (real `install.sh` with Docker against the image built from the
commit; the printed link must sign in) and `tests/install-test.sh` (installer with stubbed Docker).

Ports and temp files derive from the checkout path (`web/e2e/support/checkout.ts`), so two
checkouts can run them at once; `COLLIEDESK_E2E_PORT_BASE` overrides the ports.
Run locally: `docker compose --profile test up -d postgres mailpit`, then
`make e2e E2E_ARGS='--grep @onboarding'`; for the installer,
`docker build -t colliedesk:smoke -f server/Dockerfile . && COLLIE_DESK_IMAGE=colliedesk:smoke sh tests/install-smoke.sh`.

## Testing on Colliedesk Cloud

Colliedesk Cloud (`app.colliedesk.com`) is production. Every sign-up there counts as a customer
trial in the funnel and emails the team. Test locally or with the e2e suite first.

- **Sign up on Cloud only with a team address.** The server reads `INTERNAL_SIGNUP_EMAILS`
  (`@colliedesk.com`, `@hclabs.co`, `@hclabs.colliedesk.com` on Cloud) and sends `trial_started` with `internal: true` for a
  match. Never use a personal mailbox: it is counted as a customer. If you must, add that exact
  address to `INTERNAL_SIGNUP_EMAILS` first (HC Labs `infra` repo,
  `hosts/collie-01/colliedesk/compose.yaml`) and wait for the deploy.
- **Name a test workspace for what it is** (`e2e-…`, `test-…`), so a person reading the database
  sees it at once.
- **Every count of trials, activation or payments leaves out internal sign-ups.** Only
  `trial_started` carries the flag: drop a workspace's later events (`email_channel_saved`,
  `reply_sent`, `teammate_invited`, `subscription_activated`, `workspace_snapshot`) by its
  workspace id from an internal `trial_started`. A new query, alert or report that skips this
  inflates the funnel.
- `TestInternalSignupIsMarkedOnTrialStarted` guards the flag. Never weaken it; a new place that
  starts a trial sets the flag through `Config.InternalSignups` too.

## Releases

Colliedesk is **alpha**: releases are `v0.0.N`. Merging to `main` never makes a release, and
nothing makes one on a schedule. A release is cut only when Chính (`hiimchinh`) or Long
(`buiduclong0511`) asks: run `gh workflow run release.yml -R colliedesk/colliedesk`, or they press
Run workflow themselves (it needs write access to this repository). It
releases the commit Colliedesk Cloud runs (`version` in `GET /api/v1/config`), never the head of
`main`, as the next `v0.0.N`, and fails when Cloud runs nothing newer than the last release.
Release notes are generated from merged pull request titles, so write titles a user can read.
A run that failed after creating its tag is finished by running Release again.
`latest` follows the newest release.
Never call a release, page or doc "beta", and never tag `v0.1.0` or higher, until the HC Labs
owner announces beta. Two gates refuse other tags: the repository tag ruleset
`alpha-release-tags-only` (no bypass, so GitHub rejects the push even from an admin) and
`.github/scripts/release-tag-check.sh` in `release.yml`. Never edit either one without the
owner's announcement quoted in the PR or ruleset change.

## Issues and the roadmap

ROADMAP.md is read by teams deciding whether to move to Colliedesk, so it must only promise what
the issue tracker backs. CI (`roadmap` job, part of `ci-ok`) enforces this; follow it as you work:

- **Labels mean state:** `needs-triage` (new, added automatically), `accepted` (belongs in
  Colliedesk, not scheduled), `planned` (listed under Next or Then). Only a maintainer's request
  moves an issue to `accepted` or `planned`; say who asked in the issue.
- **Shipping a planned issue:** put `Closes #N` in the PR description **and** remove that line
  from ROADMAP.md in the same PR (or move it under Now without the link). CI fails otherwise.
- **Adding to the roadmap:** link the issue and label it to match the section:
  `gh issue edit N --add-label planned` for Next/Then, `accepted` for Later.
- **New ideas from users** belong in Discussions → Ideas, not Issues. When a maintainer accepts
  one, open an issue that links the discussion.
- Check before pushing: `python3 -I .github/scripts/roadmap-check.py ROADMAP.md` (needs `gh`
  logged in). Add `ROADMAP_CLOSING="N"` to check a PR that closes issue N.

## Clean room

Do not open, copy, translate or paraphrase the source code of other helpdesks, especially
copyleft-licensed ones. Product ideas come from public descriptions only. Code copied from
MIT-licensed projects needs its notice in `THIRD-PARTY-NOTICES.md`.
