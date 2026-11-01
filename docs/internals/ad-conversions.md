# Ad conversions (Meta Conversions API)

Colliedesk Cloud tells Meta which ad led to a trial and to a first payment, so Meta can buy ads for
trials instead of clicks. The user-facing description of what is sent is in `docs/user/telemetry.md`
("sign-ups from Meta ads") and on colliedesk.com/privacy. Code: `server/internal/metaconversions/`.

## Decisions

- **Cloud only, by construction.** `metaconversions.ConfigFromEnv` returns an empty config unless the
  server is the hosted cloud (cloud sign-in with billing, the same test as the funnel's `realm`). A
  self-hosted server reads none of the variables, so there is nothing to turn on.
- **Environment variables, not the operator console.** A key in `serverconfig` would be a setting in
  every self-hosted console. The variables are read once at start, from the process environment:

  | Variable | Meaning |
  |---|---|
  | `META_CONVERSIONS_DATASET_ID` | The Meta dataset (formerly pixel) id. Unset: nothing is recorded or sent. |
  | `META_CONVERSIONS_ACCESS_TOKEN` | The Conversions API token Events Manager generated for the dataset. Unset: nothing is recorded or sent. |
  | `META_CONVERSIONS_TEST_EVENT_CODE` | Optional. Sends every event to Events Manager → Test events only. Unset it after the check. |

- **Minimum payload.** `fbc` (`fb.1.<first-seen ms>.<fbclid>`: no `_fbc` cookie exists, so subdomain
  index 1 and the time the sign-in page first saw the click ID, as Meta's fbp/fbc page says), client
  IP, user agent, `event_id` = workspace id, `action_source` = `website`, `event_source_url` =
  `APP_ORIGIN/sign-in` or `/settings/billing`, and for `Subscribe` `value` + `currency` from Polar's
  `amount` (cents) and `currency`. Never email, name, company or workspace name.
- **Graph API `v26.0`** (`metaconversions.GraphVersion`), the newest version on 2026-11-01 (released
  2026-07-29). Meta retires a version about two years after release; move it before then.

## How an event travels

1. The sign-up (`StartTrial`, only with an `fbclid`) or the first `subscription.active` webhook
   (`Subscribe`, only for a workspace that has a `StartTrial` row) inserts one row in
   `meta_conversion_events`. Primary key `(workspace_id, event_name)`: at most one of each per
   workspace. The request never calls Meta; a failed insert is logged and the sign-up or payment
   stands.
2. `metaconversions.Worker` (every 15 s) leases due rows, sends each one alone, and settles it:
   `sent_at`, or `next_attempt_at` after a backoff (1 min, doubling, at most 6 h) with `last_error`.
   A retry sends the same `event_id` and `event_time`.
3. Meta refuses a request whose `event_time` is more than 7 days old, so a row still unsent after
   7 days less an hour gets `abandoned_at`. The log says `meta conversions given up`.
4. `client_ip` and `user_agent` are cleared when `Subscribe` is sent, when a row is given up, and
   90 days after a sign-up (Meta's own click cookie lasts 90 days).

## Checking it

With `META_CONVERSIONS_TEST_EVENT_CODE` set, sign up from a link with `fbclid=test123` (a team
address, see AGENTS.md "Testing on Colliedesk Cloud"). Events Manager → Test events shows one
`StartTrial` whose event id is the workspace id. `SELECT event_name, attempts, sent_at, last_error FROM
meta_conversion_events` shows what the worker did.
