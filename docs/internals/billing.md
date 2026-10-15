# Billing internals

Decisions and traps only. Code: `server/internal/polar` (the Polar boundary: signature check, event
parsing, checkout and portal calls, env config), `server/internal/domain/billing.go` (plans, limits,
what counts as paid), `server/internal/store/billing.go`, `server/internal/httpapi/billing-*.go`.
User page: `docs/user/billing.md`.

## Switch

Billing is on only when every `POLAR_*` variable is set. None set means self-hosted: the billing
routes are not registered (404), `/api/v1/config` says `billing.enabled=false`, the UI hides the page.
Some set but not all stops `serve` at startup and names the missing keys (never the values).
All set on a build without the locked screen (`lockedScreenShipped` false) starts `serve` with billing
off and logs why, so an image older than its configuration never stops the inboxes.

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
  that is imported history (`origin IS NOT NULL`) or inbound mail that came through the platform
  forwarder (`email_meta.via = 'platform'`) and whose authentication verdict is not `fail`.
  Refusal: HTTP 403 `trial_outbound_restricted`.
- **Mail posted to a workspace's own inbound secret never qualifies.** The admin writes that whole
  message, `Authentication-Results` included, so it proves nothing about the sender. `IngestEmail`
  records the door in `email_meta.via` (`platform` or `workspace`). Mail stored before `via`
  existed has none and does not qualify; no trial users existed when this shipped.
- **Spoof-From trade-off.** A trial user can send a forged `From: victim@...` through the hosted
  address and then reply to the victim through our relay. `fail` is refused. `none` is allowed for
  now because many small senders publish no DMARC and refusing it would block real customers; the
  200/day cap, the hourly 300 worker limit and the signup controls bound the abuse. "Require
  `pass`" is not the hardening: without a DMARC record, any `spf=pass` or `dkim=pass`, even for an
  unrelated domain, makes the verdict `pass`. The real step is requiring an *aligned* `dmarc=pass`,
  which needs the parser to record the DMARC result on its own.
- **200 replies per rolling 24 h per workspace**, counted from `email_outbox.created_at > now-24h`
  with `status <> 'failed'` (a failed delivery never left). A row exactly 24 h old no longer counts.
  Refusal: HTTP 429 `trial_outbound_limit`, with `Retry-After` and `retry_after_seconds` (time until
  the 200th newest counted reply leaves the window). The existing 300/hour worker limit stays.
- Both a new reply and the retry of a failed delivery pass `checkTrialCap`, because a retried row
  counts again once it is queued (429 on retry too).
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
- Imported history qualifies. Imports run only through the operator CLI today; if import becomes
  self-service for trial workspaces, this gate must be revisited (a Freshdesk trial could invent
  tickets for any address).
- There is no Cc support yet, so nothing to restrict. When reply Cc lands, it must be refused in
  trial unless the domain is verified, with the same `trial_outbound_restricted` code.

### Storage cap

Trial and locked (`none`) workspaces may hold 2 GiB (`domain.TrialStorageBytes`, 2147483648) of
attachments; paid plans and self-hosted have no cap. Used bytes are `sum(attachments.size)` over
messages with `origin IS NULL`: files of imported (Freshdesk) messages never count, because an import
is history the customer already owned. The number is `workspaces.counted_storage_bytes`, kept by triggers on
`attachments` (migration `storage-counter`; `attachments.counted` is false for files of imported messages
and is decided when the file is stored). It covers every way a file is added, resized or removed, so
reading it costs one row however many files exist; `TestStorageUsageStaysFastWithAHugeAttachmentTable`
holds 300k files under the 200 ms budget. Code: `Store.CheckStorage` in `store/plan-limits.go`.

- Reaching the cap exactly is allowed; one byte over is refused with `*domain.StorageLimitError`
  (HTTP 403 `storage_limit_reached`, nothing stored).
- Inbound mail checks `used + raw size + decoded file sizes` before anything is written. This is an
  upper bound, because the stored `.eml.gz` is smaller than the raw message. The 403 is a permanent
  failure for the Worker and the pipe, like `mailbox_closed`: the mail bounces.
- No lock is taken, so inbound is never serialized: messages that arrive at the same moment can pass
  the cap together by at most one message each.
- Any path that stores a new attachment must call `CheckStorage` in its transaction before writing the
  bytes and map the error through `writeStoreError` (it answers 403 with the same code). Web reply
  attachments, when they land, are one such path.
- **Founding plan: 20 GiB, imports included.** `domain.FoundingStorageBytes` (21474836480). Founding is
  $100 once a year for life and attachments are the one cost that grows without bound (break-even is
  roughly 41-56 GB), so it has a cap. Unlike the trial, imported files count: the number is
  `workspaces.stored_storage_bytes`, the sum of every attachment, kept by the same triggers (migration
  `founding-storage-counter`, additive; the three trigger functions now update both counters). Team has
  no cap. Over the cap nothing is deleted. Inbound customer mail is exempt (`Store.CheckInboundStorage` lets Founding
  through, so it is stored in full and its bytes still count: a bounced customer email hurts the paying
  team's own customers and looks like an outage; the cost is bounded by real mail). The cap still refuses
  what an agent or an import supplies (`Store.CheckStorage`, the importer's guard). Reading, replies
  without new files and export are untouched. The trial and a locked workspace still refuse inbound mail.
- **Import on Founding stops before any write.** The ticket list does not carry attachment sizes, so
  `freshdesk.guardFoundingStorage` runs a read-only pass first: the dry-run listing and id probing, then
  one read of each ticket and its conversations, and adds up the reported sizes of attachments the ledger
  does not hold yet. If `used + needed` passes the cap it returns `*freshdesk.StorageCapError` (bytes
  needed, bytes free, the cap) and writes nothing. It costs a second round of API calls, once, for Founding
  workspaces only. `importAttachment` also checks the room before each download, for a file that appeared
  at the source after the pass; that is the only way a Founding import can stop partway, and a rerun after
  upgrading resumes it. `import undo` deletes the files, and the triggers release the bytes.
  `Options.FoundingStorageBytes` and `Store.WithFoundingStorageBytes` shrink the cap in tests.
- `GET /billing` reports (for every plan) `usage.storage_bytes`, `limits.storage_bytes` (null when uncapped) and
  `limits.storage_warn_bytes` (80%: 1717986918 on the trial, 17179869184 on Founding), the usage at which the web app
  warns. On Founding `usage.storage_bytes` is the stored counter, imports included.

### Clock

`httpapi.Config.Now` feeds the store and the auth service. Tests move it across a month boundary
instead of sleeping. The webhook signature check keeps the real clock.

## Operator commands: trial end and complimentary plan

Run on the server shell (`docker compose exec server /app/colliedesk ...`).

- `colliedesk trial set --workspace <slug> --ends <RFC3339>` sets `trial_ends_at`, prints the old
  and new value and records who changed it, when, and from what to what in `workspace_trial_changes`
  (`--by`, default the OS user). It never touches `trial_extended_at`. Undo a mistake by running it again.
- `colliedesk plan grant --workspace <slug> --plan team --until <RFC3339|forever> [--by <name>]` puts
  a workspace on a complimentary Team plan: no payment, no trial clock. `plan revoke --workspace <slug>`
  ends it, and the workspace is locked again once its trial is over.
- A grant is a row in `workspace_comp_plans` (who, when, until, who revoked and when); at most one is in
  force, and a new grant replaces it. `store.LoadSubscriptions` returns it as a subscription
  (`comp:<id>`; `active` when forever, `canceled` with the end as period end otherwise), so
  `domain.NewAccess` treats it as paid. It is not a Polar customer: do not use checkout or the portal
  for such a workspace. Images older than this table ignore it, so a rollback past it drops the plan.
- `plan grant` reports what happened to the grant in force: replaced (still running) or expired (past
  its end). `workspace_comp_plans.revoke_reason` keeps `revoked`, `replaced` or `expired`. Two grants at
  the same moment: one wins, the other fails with a clear message. `plan revoke` takes no `--plan` or
  `--until`.
- `GET /api/v1/billing` returns `complimentary` and `complimentary_until` for such a workspace. The web
  billing page shows "Complimentary Team plan (until ...)" and hides Manage billing; checkout answers
  409 `already_subscribed` without a portal hint.

