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
- **Polar customers are unique by email too.** One person can administer several workspaces, so the
  customer is created with the admin's email tagged `+cd<8 hex of the workspace id>` before the `@`.
  The tag keeps the emails distinct; identity is the external id, never the email. A mail provider
  without subaddressing would not deliver receipts to that address; the portal and the page still work.
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
- Limits (3 agents, 100 conversations a month on Free) are reported by `GET /api/v1/billing`. They
  are not enforced yet.
