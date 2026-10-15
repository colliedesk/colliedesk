# Usage report (telemetry)

Once a day, a running Colliedesk server sends one small usage report to the Colliedesk team. It
tells us how many installs exist, which versions they run and how much they are used, so we know
what to fix and which versions still need security updates. It carries counts only. It never
contains a name, an email address, a subject, a message or anything else your customers wrote.

The report is on by default. You can turn it off at any time.

## Turn it off

Set one of these in the server environment (for an `install.sh` install, add the line to `.env`
next to `compose.yaml`) and restart the server:

```sh
DISABLE_TELEMETRY=true
```

`DO_NOT_TRACK=1` works too. At start the server logs `usage report is off` with the reason, or
`usage report is on` with a link to this page.

## What is sent

One event, `instance heartbeat`, one minute after the server starts and then every 24 hours.

| Field | Example | Meaning |
|---|---|---|
| `distinct_id` | `5f0c…-…` | A random id created once for this install (table `instance`). It is not derived from anything about you. |
| `version` | `0.2.0` | Colliedesk version |
| `realm` | `self-hosted` | `cloud` on Colliedesk Cloud, `self-hosted` everywhere else |
| `domain` | `help.example.com` | Host name of `APP_ORIGIN`. Not sent for `localhost`, IP addresses, single-label names or private names (`.local`, `.lan`, `.internal`, `.home.arpa`, `.localdomain`). |
| `os`, `arch` | `linux`, `amd64` | Platform the server runs on |
| `workspaces` | `1` | Number of workspaces |
| `agents_active_30d` | `4` | Agents with a session used in the last 30 days |
| `replies_30d` | `812` | Replies sent in the last 30 days (notes not counted) |
| `email_channel` | `true` | Whether any workspace has an email channel |
| `web_form` | `false` | Whether any workspace has the web form on |
| `freshdesk_import` | `true` | Whether a Freshdesk import has ever run |

## Where it goes

The report goes to PostHog (`us.i.posthog.com`), the product analytics service the Colliedesk team
uses. The key in the code can only add events; it cannot read anything. PostHog sees the IP
address the request comes from, as every web service does, and derives a country from it.

The code is in `server/internal/telemetry/`. The counts come from one SQL query in `usage.go`;
read it to check this page.
