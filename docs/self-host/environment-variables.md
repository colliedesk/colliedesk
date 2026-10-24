# Environment variables

The server reads its settings from environment variables. With the installer, add a variable to
`.env` next to `compose.yaml`, then run `docker compose up -d`. From a clone, set it in the `server`
service of `compose.yaml`.

## Variables

| Variable | Default | Meaning |
|---|---|---|
| `APP_ORIGIN` | `http://localhost:8080` | Public address of the server, with scheme. Sign-in links and email use it. |
| `DATABASE_URL` | set by the installer | PostgreSQL connection that owns the data and runs migrations. See [backups and database](database.md). |
| `DATABASE_REQUEST_URL` | set by the installer | PostgreSQL connection that serves requests. See [backups and database](database.md#database-requirements-and-hardening). |
| `ATTACHMENTS_DIR` | `/data/attachments` in the compose files | Where attachment files are stored. Keep it on a volume. |
| `TRUSTED_PROXIES` | loopback and the Docker bridge range | Proxies whose `X-Forwarded-For` the server trusts. See [running behind a proxy](running-behind-proxy.md). |
| `DISABLE_TELEMETRY`, `DO_NOT_TRACK` | empty | Turn off the usage report and product analytics. See [usage report](#usage-report). |
| `COLLIE_DESK_IMAGE` | set by the installer | Image the installer's `compose.yaml` runs. See [upgrading](README.md#upgrading). |
| `HELP_CENTER_DOMAIN` | empty | Give each workspace's public help center its own host name, `<slug>.<domain>`. Empty serves it on `APP_ORIGIN/help/<slug>`. See [help center host names](running-behind-proxy.md#help-center-host-names). |
| `KB_VIDEO_MAX_BYTES` | `209715200` (200 MiB) | The largest knowledge base video the server takes. Videos go up in parts of 8 MiB (see [running behind a proxy](running-behind-proxy.md#large-uploads)) and their files stay in `ATTACHMENTS_DIR` like attachments. |
| `PROBLEM_REPORT_WORKSPACE` | empty | Send problem reports to a workspace on this server. See [problem reports](#problem-reports). |
| `SMTP_*`, `EMAIL_FROM_POLICY`, `MAIL_FROM_*`, `EMAIL_TRUSTED_AUTHSERV_ID` | | Outgoing and incoming email. See [configuring email](email.md). |
| `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET` | empty | Replies sent through a connected Gmail mailbox. See [send replies through Gmail](email.md#send-replies-through-gmail). |
| `MAILBOX_TOKEN_KEYS` | set by the installer on new installs; empty on older ones | Key ring that encrypts the stored Gmail access and the Freshdesk API key of an import running in the app: `k1:` followed by 32 random bytes in base64 (`k1:$(openssl rand -base64 32)`). Without it an import keeps its key in memory only, and after a restart someone enters the key again. Keep a copy; to change it, put the new key first (`k2:<new>,k1:<old>`), never replace it. It also encrypts the API keys of the AI providers a workspace adds; without it no workspace can add one. See [send replies through Gmail](email.md#send-replies-through-gmail). |
| `AI_SERVER_PROVIDER`, `AI_SERVER_API_KEY`, `AI_SERVER_BASE_URL`, `AI_SERVER_MODELS`, `AI_SERVER_LABEL`, `AI_SERVER_PRICES`, `AI_SERVER_MONTHLY_ALLOWANCE` | empty | One AI provider for every workspace on this server. See [AI provider](#ai-provider). |
| `AI_ALLOW_PRIVATE_BASE_URLS` | empty | `1` lets a workspace's own AI provider use a private or local address and plain `http`. See [AI provider](#ai-provider). |

## Usage report

Once a day the server sends a small usage report with counts and its public host name. What it
contains is listed in [usage report](../user/telemetry.md). To turn it off, set:

```sh
DISABLE_TELEMETRY=true
```

`DO_NOT_TRACK=1` works too. Any value other than empty, `0`, `false`, `no` or `off` turns the
report off. At start the server logs `usage report is off` with the reason, or `usage report is
on` with a link to the usage report page.

An install made before this setting existed has a `compose.yaml` that does not pass it to the
server: run `install.sh` again with the same `--dir` first (it keeps your `.env` and data).

Development builds (version `dev`, for example `go run` or a local `docker compose up --build`)
never send the report.

To have the reports of your install deleted, send its id to support@colliedesk.com with "Usage
report" in the subject. The id is in the `install_id` of the `usage report sent` log line:

```sh
docker compose logs server | grep 'usage report sent'
```

## AI provider

Workspaces can add their own AI provider with a key in **Settings → Built-in AI** (see
[AI providers and features](../user/ai.md)). You can also give every workspace on this server one
provider that you pay for, the way Colliedesk Cloud does:

```sh
AI_SERVER_PROVIDER=anthropic            # or openai_compatible
AI_SERVER_API_KEY=sk-ant-...            # optional for an openai_compatible server without a key
AI_SERVER_BASE_URL=                     # openai_compatible: required, with the version path, e.g. http://ollama:11434/v1
AI_SERVER_MODELS=triage=claude-haiku-5-5,draft=claude-sonnet-5-5|claude-opus-5-5
AI_SERVER_LABEL=Company AI              # optional; the name workspaces see (default "Server provider")
```

`AI_SERVER_MODELS` lists, per feature (`triage`, `draft`), the models a workspace may pick, separated
by `|`; the first is the default. A feature left out cannot use the server's provider. If some of
these variables are set but not all that are needed, the server does not start and says which are
missing. The server's provider can reach any address, your own network included: you chose it. No
workspace uses it until one of its admins turns a feature on.

To bound what the server's provider spends, price its models and give each workspace a monthly
allowance in US dollars:

```sh
AI_SERVER_PRICES=claude-haiku-5-5=0.1:0.5,claude-sonnet-5-5=2:10:0.2,claude-opus-5-5=4:20:0.4
AI_SERVER_MONTHLY_ALLOWANCE=10                 # per workspace, or agent=3,trial=2 (per active agent, and for a trial)
```

A price is `input:output[:cache_read[:cache_write]]` dollars per million tokens, as your provider bills
them. Each call's cost is computed with the prices of the day it ran and stored, so changing a price
never rewrites a past month. An allowance needs a price for every model in `AI_SERVER_MODELS` (the server
says which are missing); a model that answers without a price (a provider's fallback) is charged at
the highest price you declared. Allowances reset on the first day of each UTC month. Without
`AI_SERVER_MONTHLY_ALLOWANCE` there is no limit. A call counts only when the provider reports its
tokens: an OpenAI-compatible server that reports none (the server logs a warning at start) and a call
cut off by a crash cost nothing against the allowance.

A provider a workspace adds can only reach public `https` addresses, so a workspace cannot make your
server call into your network. To let workspaces use a model server on your network (an Ollama next to
Colliedesk), set `AI_ALLOW_PRIVATE_BASE_URLS=1`; a provider without a key is then accepted too. Never
set it on a server whose workspaces you do not trust.

## Problem reports

By default a [problem report](../user/problem-reports.md) on a self-hosted server sends nothing to
HC Labs; the dialog offers to open a GitHub issue. To receive the reports in your own support desk,
set `PROBLEM_REPORT_WORKSPACE` to the address (slug) of a workspace on the same server and restart.
Reports from every other workspace on that server then arrive there as conversations tagged
`problem-report`. The installer's `compose.yaml` does not pass this variable: add it to the
`environment` of the `server` service.
