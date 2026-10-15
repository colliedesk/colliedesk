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

The code is in `server/internal/telemetry/`. The counts come from one SQL query in `usage.go`;
read it to check this page.
