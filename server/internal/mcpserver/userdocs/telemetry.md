# Usage report (telemetry)

Once a day, a Colliedesk install sends one small usage report to the Colliedesk team. It tells us
how many installs exist, which versions they run and how much they are used, so we know what to
fix and which versions still need security updates. It carries counts and the public host name of
your install. It never contains a person's name, an email address, a workspace name, a subject, a
message or anything else your customers wrote.

The report is on by default. You can turn it off at any time.

## Turn it off

Set this in the server environment and restart the server:

```sh
DISABLE_TELEMETRY=true
```

`DO_NOT_TRACK=1` works too. Any value other than empty, `0`, `false`, `no` or `off` turns the
report off. At start the server logs `usage report is off` with the reason, or `usage report is
on` with a link to this page.

For an `install.sh` install, add the line to `.env` next to `compose.yaml`, then run
`docker compose up -d`. An install made before this page existed has a `compose.yaml` that does
not pass the setting to the server: run `install.sh` again with the same `--dir` first (it keeps
your `.env` and data).

Development builds (version `dev`, for example `go run` or a local `docker compose up --build`)
never send the report.

## What is sent

One event, `instance heartbeat`, at most once a day per install. A server checks every hour,
starting one minute after it starts; several servers on one database (a restart, a second replica)
still send one report a day together. Each report sent is also written to the server log as
`usage report sent` with the same values.

| Field | Example | Meaning |
|---|---|---|
| `distinct_id` | `5f0c…-…` | A random id created once for this install (table `instance`). It is not derived from anything about you. |
| `version` | `0.2.0` | Colliedesk version |
| `realm` | `self-hosted` | `cloud` on Colliedesk Cloud, `self-hosted` everywhere else |
| `domain` | `help.example.com` | Host name of `APP_ORIGIN`, in ASCII. Sent only for a name under a public domain suffix. Not sent for `localhost`, IP addresses, names that contain an IP address, `.onion` names, private suffixes (`.local`, `.lan`, `.internal`, `.corp`, `.home.arpa`, …) or private hosting suffixes (a tailnet, `github.io`). |
| `os`, `arch` | `linux`, `amd64` | Platform the server runs on |
| `workspaces` | `1` | Number of workspaces |
| `agents_active_30d` | `4` | Agents with a session used in the last 30 days |
| `replies_30d` | `812` | Replies sent from Colliedesk in the last 30 days (notes and imported replies not counted) |
| `email_channel` | `true` | Whether any workspace has an email channel |
| `web_form` | `false` | Whether any workspace has the web form on |
| `freshdesk_import` | `true` | Whether the install has records of a Freshdesk import |

## Colliedesk Cloud only: trial, activation and payment events

Colliedesk Cloud (`realm` `cloud`) also sends these events, so the team can see how many trials
start, how far a new workspace gets, and how many become paid. A self-hosted server has no trials or billing and never sends
them; `DISABLE_TELEMETRY` and `DO_NOT_TRACK` turn them off too.

| Event | When | Fields |
|---|---|---|
| `trial_started` | A cloud sign-up creates a workspace and signs its admin in | `distinct_id` = the workspace id, `realm`, `version`, and `website_visitor_id` when the browser has the colliedesk.com analytics cookie (set only after the person accepted cookies on the website): the random id PostHog gave that visit, so the team can see which page led to the trial |
| `email_channel_saved` | An admin saves the workspace's support address | `distinct_id` = the workspace id, `realm`, `version` (never the address) |
| `reply_sent` | A reply to a customer is sent from Colliedesk; a note is not a reply | `distinct_id` = the workspace id, `by` (`person`, or `ai_agent` for an agent token), `realm`, `version` (never the text or the recipient) |
| `teammate_invited` | An admin invites a teammate | `distinct_id` = the workspace id, `realm`, `version` (never the invited address) |
| `subscription_activated` | A workspace becomes a paying customer for the first time (its first `subscription.active` from Polar). A redelivered webhook, a past_due subscription paid again and a later plan change are not counted again | `distinct_id` = the workspace id, `plan` (`team`, `team25` or `founding`), `realm`, `version` |

Once a day, with the instance heartbeat, Colliedesk Cloud also sends one `workspace_snapshot` event
per workspace, so the team can see how each customer is doing: `distinct_id` = the workspace id,
`realm`, `version`, the workspace's name and slug, and these counts (never an email address or
anything written in a conversation):

| Field | Example | Meaning |
|---|---|---|
| `workspace_name`, `workspace_slug` | `Acme Support`, `acme-support` | The workspace's name and address slug, so the team knows which customer it is |
| `plan` | `trial` | `trial`, `team`, `team25`, `founding` or `none` (trial over, no paid plan), the same plan the billing page shows |
| `trial_days_left` | `6` | Days left of the trial, rounded up; empty unless `plan` is `trial` |
| `days_since_created` | `10` | Whole days since the workspace was created |
| `agents_total` | `2` | Agents that are not deactivated |
| `agents_active_7d`, `agents_active_30d` | `2`, `2` | Agents who signed in (used a session) in the last 7 or 30 days. Agent tokens do not count: a token working is not a person signing in. This differs from the `agents_active_30d` of the instance heartbeat above, which counts every session, tokens included, across all workspaces |
| `replies_7d`, `replies_30d` | `1`, `2` | Replies sent from Colliedesk in the last 7 or 30 days (notes and imported replies not counted) |
| `ai_agent_replies_7d` | `1` | Of the replies in the last 7 days, those written with an agent token |
| `conversations_open` | `1` | Conversations with status `open` |
| `conversations_created_7d` | `1` | Conversations created in the last 7 days |
| `email_channel` | `false` | Whether the workspace has an email channel |
| `web_form` | `true` | Whether the workspace has the web form on |
| `tokens_active` | `1` | Agent tokens that are not revoked or expired |
| `freshdesk_import` | `true` | Whether the workspace has records of a Freshdesk import |

Each batch sent is logged as `workspace snapshots sent`. A failed send is logged as `usage report
not sent` and never stops the server; the day's report is not retried.

The workspace id is a random UUID. The events never carry the email address of the person who
signed up, the workspace name or its address. They are sent in the background; a failure is logged
as `funnel event not sent` and changes nothing in the workspace.

## How the web app is used

The web app also reports which screens agents open and which buttons they click, so the team can
see what is used and what is confusing. It is on by default, on Colliedesk Cloud and self-hosted
alike, and the same switch as the usage report turns it off: set `DISABLE_TELEMETRY=true` (or
`DO_NOT_TRACK=1`) and `GET /api/v1/config` answers `product_analytics.enabled: false`, so the web app
never loads the analytics library. Every event carries `realm` (`cloud` or `self-hosted`).

- The agent is named by their random agent id, with their role (`admin` or `agent`); the workspace
  id and plan come along. Never an email address or a name.
- Every element's text and attributes are masked, so no conversation, message, contact or form
  content is sent. URLs are sent without their query string or fragment, and page titles are dropped.
- Nothing is stored in the browser for it (no cookie, no local storage), and no session is recorded.

Besides page views, the web app sends these named events. Their properties are only yes/no values,
counts and a fixed set of labels; never a search term, a contact, a message, or an id of either.
Each is sent only after the action succeeded.

| Event | Properties |
|---|---|
| `conversation_opened` | `source`: `list` or `link` |
| `inbox_filtered` | `filter`: `status`, `assignee` or `tag` |
| `inbox_searched` | `has_results` (the search words are never sent) |
| `saved_reply_inserted` | none |
| `conversation_status_changed` | `to`: the new status |
| `conversation_assigned` | `to_self` |
| `bulk_action_applied` | `action`: `status`, `assign`, `add_tag` or `remove_tag`; `count` |
| `agent_request_decided` | `decision`: `approved` or `rejected` |
| `settings_section_opened` | `section`: the settings section |
| `workspace_export_requested` | none |

When you sign out, the web app forgets the agent id, so later events no longer carry it.

### Trial question (Colliedesk Cloud only)

In the last 7 days of a free trial, an admin sees one question: "What is still missing for you to
leave Freshdesk for good?" It appears once; "Not now" or "Send" closes it for good in that browser.
The text of the answer is sent only when the admin presses **Send**, as the event
`survey_answered`. This is the one event that carries text a person typed. "Not now" sends
`survey_dismissed` with no text. Self-hosted installs never show the question.

## Where it goes

The report goes to PostHog (`us.i.posthog.com`), the product analytics service the Colliedesk team
uses. PostHog receives the IP address the request comes from; the project is set to discard it
after deriving a country. The project key in the code can add events but cannot read them.
Anyone can send events with a public key, so we treat the reports as estimates.

To have the reports of your install deleted, send its id to support@colliedesk.com with "Usage
report" in the subject. The id is in the `install_id` of the `usage report sent` log line; for an
`install.sh` install:

```sh
docker compose logs server | grep 'usage report sent'
```

The code is in `server/internal/telemetry/`. The counts come from SQL in `usage.go` and, for the workspace snapshots, `snapshot.go`;
read it to check this page.
