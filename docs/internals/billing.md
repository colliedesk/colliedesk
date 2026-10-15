# Billing internals

Decisions and traps only. Code: `server/internal/polar` (the Polar boundary: signature check, event
parsing, checkout and portal calls, env config), `server/internal/domain/billing.go` (plans, limits,
what counts as paid), `server/internal/store/billing.go`, `server/internal/httpapi/billing-*.go`.
User page: `docs/user/billing.md`.

## Switch

Billing is on only when every `POLAR_*` variable is set. None set means self-hosted: the billing
routes are not registered (404), `/api/v1/config` says `billing.enabled=false`, the UI hides the page.
Some set but not all stops `serve` at startup and names the missing keys (never the values).

| Variable | Meaning |
|---|---|
| `POLAR_ACCESS_TOKEN` | Organization access token. Scopes: checkouts write, customer sessions write. |
| `POLAR_WEBHOOK_SECRET` | The `whsec_...` secret of the webhook endpoint. |
| `POLAR_API_URL` | Optional. Default `https://api.polar.sh`; sandbox `https://sandbox-api.polar.sh`. |
| `POLAR_PRODUCT_TEAM_MONTHLY`, `POLAR_PRODUCT_TEAM_YEARLY`, `POLAR_PRODUCT_FOUNDING` | Polar product ids. |

Webhook URL to register in Polar: `https://<host>/api/v1/webhooks/polar`, event types
`subscription.*`. The path is under `/api/` because the web handler forwards only that prefix to
the API.

## Decisions

- **The workspace id is the Polar external customer id.** Before a checkout the server finds or
  creates the Polar customer with `external_id` = the session's workspace id, then creates the
  checkout with that `customer_id`. Events come back with `customer.external_id`, which names the
  workspace. The client sends only the plan name; any other field is rejected, so a browser can
  neither pay for nor link to another workspace.
- **Polar customers are unique by email too.** The customer is created with the admin's real email,
  so receipts reach them. If Polar refuses the create (422, the email belongs to another customer:
  the same person pays for a second workspace), the server retries once with the address tagged
  `+cd<8 hex of the workspace id>` before the `@` and logs a WARN with the workspace id (no
  address). Identity is the external id, never the email. A mail provider without subaddressing
  would not deliver receipts to the tagged address; the portal and the page still work.
- **Nothing is read before the signature verifies.** The webhook route is public in the permission
  table, but the handler checks the Standard Webhooks signature first. A bad, missing or stale
  (more than 5 minutes either way) signature is 401 and touches no table.
- **Two key derivations.** Polar secrets issued before 2026-09-08 use the whole `whsec_...` string
  as UTF-8 key; newer ones follow Standard Webhooks (base64 after the prefix). Both are accepted.
- **Replay and order.** The webhook id is stored in `billing_webhook_events` in the same
  transaction as the state change, so a redelivery is a no-op and a failure rolls both back (Polar
  retries a 5xx). The id is unique per workspace. Events of one subscription are ordered by the
  subscription's `modified_at`; an older one is ignored (`stale`). The ordering check is inside the
  upsert (`ON CONFLICT ... WHERE source_modified_at <= EXCLUDED.source_modified_at`), so two events
  racing for a subscription with no row yet cannot overwrite each other with the older state.
- **One row per subscription.** `workspace_billing` is keyed by (workspace, subscription). Two
  checkouts can leave two subscriptions; the plan in effect comes from the best one that is still
  paid, so cancelling or revoking the spare never drops the plan while the other still charges.
  `GET /api/v1/billing` returns `active_subscriptions`, and the page tells the admin to cancel the
  extra one when it is above 1.
- **Unknown means 200, and a warning.** An unknown workspace, a non-UUID or missing external id, a
  product we do not sell and a non-subscription event all return 200 `ignored` and store nothing.
  Polar stops retrying, and the endpoint reveals nothing about which workspaces exist. A
  subscription event that is ignored is logged at WARN with event type, subscription id and
  webhook id (no customer data), because it may be a payment nobody sees.
- **A new Polar status never fails the webhook.** A status outside the known list is stored as
  `unknown` (not paid) with a WARN log. Failing would make Polar retry until it disables the endpoint.
- **Row-level security without a privileged lookup.** The workspace id in the event is used as the
  scope of an ordinary workspace transaction. A workspace that does not exist is invisible to it,
  so no cross-workspace query is needed. Both billing tables carry the same policies as the other
  workspace tables.
- **Effective plan is computed on read.** `workspace_billing` mirrors the last event; the plan is
  derived with `Subscription.Entitled(now)`. A canceled subscription with a future period end is
  still paid, so no timer has to flip the plan when the period ends.

## Status mapping

| Polar status | Plan in effect |
|---|---|
| `active`, `trialing` | paid plan |
| `past_due` | paid plan, flagged on the page (Polar retries the card) |
| `canceled` | paid plan until `current_period_end`, then Free |
| `unpaid`, `incomplete`, `incomplete_expired`, `paused`, `unknown` | Free |
| any status with `ended_at` in the past (revoked) | Free |

## Traps

- Polar answers 404 or 422 to a customer session for an external id it has never seen. The portal
  route checks `workspace_billing` first and returns 404 without calling Polar, and maps both Polar
  answers to the same 404.
- A second checkout for a paying workspace would bill twice; the checkout route returns 409.
- Client errors from Polar never include its response body or the token; they are logged as a
  status code only.
- The access token travels only in the `Authorization` header of server-to-Polar calls.

## Free-plan limits

Enforced only when billing is enabled (`Store.WithPlanLimits`, `auth.Service.WithPlanLimits`, both set
from `POLAR_*` in `httpapi.New`); self-hosted instances never hold or refuse anything. The plan is
the effective plan on read (`effectivePlan` in `store/plan-limits.go`), so a canceled subscription
that is still inside its paid period keeps the paid limits. Code: `store/plan-limits.go`,
`auth/invites.go`, `auth/links.go`.

### Agent seats

- Cap: 3 on Free, 10 on Team and Founding (more than 10 is "contact us", so the paid message says
  that instead of offering an upgrade). A seat is an agent, or a person with an unused, unrevoked,
  unexpired invite. Pending invites count by distinct address: re-sending an invite to the same
  address, or inviting someone who is already an agent (a role change), takes no new seat.
- Checked when an invite is created and when one is redeemed. Redeem excludes the invite itself
  and skips someone who is already an agent. Redeem matters after a downgrade: the invite stays
  unused (the transaction rolls back), so it works after an upgrade.
- Both checks take `pg_advisory_xact_lock("agent-seats:<workspace>")`, so concurrent invites see
  each other. Existing agents are never removed.
- Error: HTTP 402, code `plan_limit_reached`, message names the plan, the cap and the way out.

### Monthly conversation quota

- 100 per UTC calendar month on Free (`domain.MonthStart`). Counted: conversations with
  `origin IS NULL` and `created_at >= month start`, held ones included, so "received" is what the
  billing page shows.
- Only the inbound email pipeline holds. Held means `conversations.held_at` is set to the arrival
  time; the row, its messages and its attachments are stored exactly like any other. The check
  runs under `pg_advisory_xact_lock("conversation-quota:<workspace>")` so two deliveries racing
  for the last slot cannot both pass. The lock is taken after the attachment upload and right
  before the insert, so a slow object store never queues other new conversations. Lock order is
  sender, then quota. Replies that thread onto an existing conversation never reach the check.
- **API-created conversations (`POST /api/v1/conversations`) count but are never held** (they take the same quota lock for the length of the insert). An agent
  who just created one expects to see it, and the quota exists to bound inbound volume.
- **The Freshdesk importer is exempt on both sides.** Imported rows carry `origin`, so they are
  not counted, and the importer never sets `held_at`, so they are never held. An import is a
  migration of history (no data loss is the rule that wins); importing 5,000 tickets must not
  hide the first real customer mail of the month.
- Visibility is one predicate: `held_at IS NULL OR held_at < <start of this month>`. There is no
  job. A conversation held in an earlier month becomes visible the instant the month turns, and
  nothing runs on any request to release it. The predicate is applied in the inbox list and search
  (`listConversations`) and in the contact view. Direct reads by id (`GET /conversations/{id}`) and
  replies on a held conversation work, so a link from an email or an agent's own notes never 404s.
- Upgrade: `ApplySubscription` clears `held_at` of the workspace in the same transaction that
  records the paid subscription (`releaseHeldIfPaid`), so the release is as idempotent as the
  webhook. A downgrade changes nothing already stored.
- `held_at` has a partial index `(workspace_id, held_at) WHERE held_at IS NOT NULL`. It serves the
  held count and the release, and is empty for a workspace that holds nothing. The list adds one
  cheap `held_at` test per scanned row; `TestInboxListLatencyBudget` still gates the 100 ms p95.
  The `held_count` on the list response is a count over that index, and only runs when plan limits
  are on.
- `ConversationPage.held_count` is on the list response so that agents, who cannot read
  `/api/v1/billing`, still see the banner.

### Trial outbound controls

Anti-abuse for trial workspaces, so a fresh trial cannot be used as a mail relay. Only when plan
limits are on and the plan in effect is `trial`; paid and self-hosted workspaces are untouched.
Code: `checkTrialOutbound` in `store/plan-limits.go`, called from `enqueueReply`, the only place a
reply enters `email_outbox` (composer, a future "start conversation", MCP send all pass it). Notes
and workspaces without an email channel never reach it.

- **Replies only to customers who wrote first**, unless the support address domain is verified
  (`email_channels.domain_verified_at`). A conversation qualifies when it has an `incoming` message
  that is imported history (`origin IS NOT NULL`) or inbound mail whose authentication verdict is
  not `fail`. Refusal: HTTP 403 `trial_outbound_restricted`.
- **Spoof-From trade-off.** A trial user can mail their own hosted address with a forged
  `From: victim@...` and then reply to the victim through our relay. `fail` is refused. `none` is
  allowed because many small senders publish no DMARC; refusing it would block real customers. The
  residual risk is bounded by the 200/day cap, the hourly 300 worker limit and signup controls.
  Requiring `pass` later is a one-line change in `customerWroteFirst`, no schema change.
- **200 replies per rolling 24 h per workspace**, counted from `email_outbox.created_at > now-24h`
  with `status <> 'failed'` (a failed delivery never left). A row exactly 24 h old no longer counts.
  Refusal: HTTP 429 `trial_outbound_limit`, with `Retry-After` and `retry_after_seconds` (time until
  the 200th newest counted reply leaves the window). The existing 300/hour worker limit stays.
- Both checks run under `pg_advisory_xact_lock("outbound-cap:<workspace>")` inside the message
  transaction, so two replies racing for the last slot admit exactly one, and a refusal rolls back
  the whole message (no orphan reply in the timeline).
- `enqueueReply` stamps `email_outbox.created_at` from the store clock, so tests that move
  `httpapi.Config.Now` move the window with it.
- Replies queued before a lock are still sent: the worker does not check the plan.
- Index `email_outbox_workspace_created_idx (workspace_id, created_at)` serves the count;
  `TestTrialCapCountStaysFastOnALargeOutbox` asserts the plan uses it and a reply takes under
  200 ms with 200k outbox rows (measured ~12 ms).
- `GET /billing` reports `usage.replies_last_24h` (0 off-trial).
- There is no Cc support yet, so nothing to restrict. When reply Cc lands, it must be refused in
  trial unless the domain is verified, with the same `trial_outbound_restricted` code.

### Clock

`httpapi.Config.Now` feeds the store and the auth service. Tests move it across a month boundary
instead of sleeping. The webhook signature check keeps the real clock.
