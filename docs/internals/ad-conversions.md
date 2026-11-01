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

### When to run a test

1. Once after the first deploy that sets the variables.
2. After a new dataset or a new access token.
3. After a release that changes sign-up, onboarding, billing webhooks or `GraphVersion`.
4. When ads get clicks but Events Manager shows no `StartTrial` for 3 days.

### Steps (the owner can run them; an agent changes the server)

> Warning: while `META_CONVERSIONS_TEST_EVENT_CODE` is set, every real event also goes to Test
> events only, so Meta stops learning from real trials. Unset it on the same day.

1. In Events Manager, open the `Colliedesk` dataset, then **Test events**. Copy the test code
   (`TEST…`). It is not a secret.
2. An agent sets `META_CONVERSIONS_TEST_EVENT_CODE` to that code on Cloud and restarts the server.
3. Open a private browser window.
4. Open `https://app.colliedesk.com/sign-in?intent=trial&fbclid=test123`.
5. Sign up with a team address (see AGENTS.md "Testing on Colliedesk Cloud"), for example
   `chinh+meta1@hclabs.co`. Name the workspace `test-meta-1`. A workspace sends `StartTrial` once,
   so each new test needs a new address and workspace.
6. Within one minute, Test events shows one `StartTrial`. Its event id is the workspace id.
7. An agent unsets the variable and restarts the server.

`Subscribe` needs a real payment, so it is not part of this test. After the first paid workspace that
came from an ad, check its row instead. If Test events stays empty, `SELECT event_name, attempts,
sent_at, last_error FROM meta_conversion_events` shows what the worker did.

### Ad links

The sign-in page reads `fbclid` from its own address. colliedesk.com carries an ad link's `utm_*`,
`ttclid` and `fbclid` to its sign-in and Founding links (`public/attribution.js` in the site repo,
colliedesk.com#131; kept in the tab's `sessionStorage` at most a day, no cookie). The Founding link's
redirect to sign-in keeps them too. A visitor who closes the tab and comes back later without the ad
link signs up without a click ID, so no event is sent for them.
