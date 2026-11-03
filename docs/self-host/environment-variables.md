# Environment variables

The environment holds only what the server needs before it can read its database: where the database
is, its own address, where it listens and keeps files, and the key ring that seals the secrets stored
in the database. Every other setting (email, AI, mailboxes, billing, proxies, limits) lives in the
database, as versions you can restore: change it with `collie config` (below) or in the operator
console. With the installer, add a variable to `.env` next to `compose.yaml`, then run
`docker compose up -d`. From a clone, set it in the `server` service of `compose.yaml`.

## Variables

| Variable | Default | Meaning |
|---|---|---|
| `APP_ORIGIN` | `http://localhost:8080` | Public address of the server, with scheme. Sign-in links and email use it. |
| `DATABASE_URL` | set by the installer | PostgreSQL connection that owns the data and runs migrations. See [backups and database](database.md). |
| `DATABASE_REQUEST_URL` | set by the installer | PostgreSQL connection that serves requests. See [backups and database](database.md#database-requirements-and-hardening). |
| `OPERATOR_ORIGIN` | empty | The operator console's own address, such as `https://ops.example.com` (https; http only for localhost). Empty serves the console at `APP_ORIGIN/operator/`. See [operator console](operator-console.md#its-own-address). |
| `LISTEN_ADDR` | `:8080` | Address the HTTP server listens on inside the container. The compose files set `:8080` and map the port; `collie serve --listen` overrides it. |
| `ATTACHMENTS_DIR` | `data/attachments`, relative to the working directory; the compose files set `/data/attachments` | Where attachment files are stored. Keep it on a volume, and give `collie serve` and `collie import` the same one. |
| `MAILBOX_TOKEN_KEYS` | set by the installer on new installs; empty on older ones | Key ring that encrypts what the server stores for later: the secrets among the settings (SMTP password, API keys, client secrets), the Gmail and Microsoft 365 access, the Freshdesk API key of an import running in the app, the API keys of the AI providers a workspace adds, and the secrets and tokens of a Zalo OA channel. `k1:` followed by 32 random bytes in base64 (`k1:$(openssl rand -base64 32)`). Keep a copy; to change it, put the new key first (`k2:<new>,k1:<old>`), run `collie config reseal`, and never drop a key something is still sealed with. Without it, secret settings stay in the environment (see [no key ring](#no-key-ring)), an import keeps its key in memory only, and no workspace can add an AI provider or store a Zalo OA's secrets. |
| `DISABLE_TELEMETRY`, `DO_NOT_TRACK` | empty | Turn off the usage report and product analytics. See [usage report](#usage-report). |
| `COLLIE_DESK_IMAGE` | set by the installer | Image the installer's `compose.yaml` runs. See [upgrading](README.md#upgrading). |

`APP_SECRET`, which older installers wrote to `.env`, is read by no release; you can remove it.

## Settings

Run `collie config` in the server container (`docker compose exec server /app/collie config …`). A
setting is named by its dotted key or by the variable it replaced; both work.

```sh
docker compose exec server /app/collie config list                  # every setting, its value and who set it
docker compose exec server /app/collie config set smtp.host=smtp.example.net smtp.port=587
docker compose exec -T server /app/collie config set-secret smtp.password < password.txt
docker compose exec server /app/collie config doctor                # 0 clean, 1 warnings, 2 errors
```

Each change is checked the way the server reads it at start (a half-configured group is refused,
naming what is missing) and saved as a new version; a running server applies it within seconds
(restart it to apply it at once). `collie config history` lists the versions and `collie config
restore <version>` brings one back. `collie config unset KEY…` returns settings to their default,
`collie config import FILE` (or `--from-env`, `--stdin`) takes settings written as an `.env` file
(by default only the groups of settings the database does not hold yet; `--replace` replaces the
groups the file sets), and `collie config apply FILE`
(`--dry-run` prints what would change) makes the configuration match a JSON file as `collie config export --format json`
writes it. Secrets are write-only: no command, export or log shows them
again, except `collie config export --include-secrets`. More: [operator console and settings](operator-console.md).

| Variable before | Setting | Group |
|---|---|---|
| `AI_SERVER_PROVIDER` | `ai.server.provider` | AI |
| `AI_SERVER_API_KEY` | `ai.server.api_key` | AI |
| `AI_SERVER_BASE_URL` | `ai.server.base_url` | AI |
| `AI_SERVER_MODELS` | `ai.server.models` | AI |
| `AI_SERVER_LABEL` | `ai.server.label` | AI |
| `AI_SERVER_PRICES` | `ai.server.prices` | AI |
| `AI_SERVER_MONTHLY_CREDITS` | `ai.server.monthly_credits` | AI |
| `SMTP_HOST` | `smtp.host` | Email |
| `SMTP_PORT` | `smtp.port` | Email |
| `SMTP_USERNAME` | `smtp.username` | Email |
| `SMTP_PASSWORD` | `smtp.password` | Email |
| `SMTP_STARTTLS` | `smtp.starttls` | Email |
| `EMAIL_FROM_POLICY` | `email.from_policy` | Email |
| `MAIL_FROM_DOMAIN` | `mail_from.domain` | Email |
| `MAIL_FROM_INBOUND_SECRET` | `mail_from.inbound_secret` | Email |
| `EMAIL_TRUSTED_AUTHSERV_ID` | `email.trusted_authserv_id` | Email |
| `HOSTED_MAIL_DOMAIN` | `hosted_mail.domain` | Email |
| `RESEND_WEBHOOK_SECRET` | `resend.webhook_secret` | Email |
| `RESEND_API_KEY` | `resend.api_key` | Email |
| `GOOGLE_OAUTH_CLIENT_ID` | `google.client_id` | Mailboxes & GitHub |
| `GOOGLE_OAUTH_CLIENT_SECRET` | `google.client_secret` | Mailboxes & GitHub |
| `MICROSOFT_OAUTH_CLIENT_ID` | `microsoft.client_id` | Mailboxes & GitHub |
| `MICROSOFT_OAUTH_CLIENT_SECRET` | `microsoft.client_secret` | Mailboxes & GitHub |
| `MICROSOFT_OAUTH_TENANT` | `microsoft.tenant` | Mailboxes & GitHub |
| `GITHUB_APP_ID` | `github.app_id` | Mailboxes & GitHub |
| `GITHUB_APP_SLUG` | `github.slug` | Mailboxes & GitHub |
| `GITHUB_APP_CLIENT_ID` | `github.client_id` | Mailboxes & GitHub |
| `GITHUB_APP_PRIVATE_KEY` | `github.private_key` | Mailboxes & GitHub |
| `GITHUB_APP_CLIENT_SECRET` | `github.client_secret` | Mailboxes & GitHub |
| `GITHUB_APP_WEBHOOK_SECRET` | `github.webhook_secret` | Mailboxes & GitHub |
| `POLAR_ACCESS_TOKEN` | `polar.access_token` | Billing & retention |
| `POLAR_WEBHOOK_SECRET` | `polar.webhook_secret` | Billing & retention |
| `POLAR_API_URL` | `polar.api_url` | Billing & retention |
| `BILLING_ALERT_EMAIL` | `billing.alert_email` | Billing & retention |
| `RETENTION_DELETE` | `retention.delete` | Billing & retention |
| `RETENTION_MAX_DELETIONS_PER_DAY` | `retention.max_deletions_per_day` | Billing & retention |
| `INTERNAL_SIGNUP_EMAILS` | `internal_signup.emails` | Sign-ups |
| `INTERNAL_SIGNUP_NETWORKS` | `internal_signup.networks` | Sign-ups |
| `TRUSTED_PROXIES` | `trusted_proxies` | Security & network |
| `CLOUD_AUTH_ISSUER` | `cloud_auth.issuer` | Security & network |
| `CLOUD_AUTH_JWKS_URL` | `cloud_auth.jwks_url` | Security & network |
| `CLOUD_AUTH_AUTHORIZED_PARTIES` | `cloud_auth.authorized_parties` | Security & network |
| `CLOUD_AUTH_EMAIL_CLAIM` | `cloud_auth.email_claim` | Security & network |
| `CLERK_PUBLISHABLE_KEY` | `clerk.publishable_key` | Security & network |
| `HELP_CENTER_DOMAIN` | `help_center.domain` | Security & network |
| `AI_ALLOW_PRIVATE_BASE_URLS` | `ai.allow_private_base_urls` | Security & network |
| `MAIL_IMAGE_CACHE_BYTES` | `mail_image_cache.max_bytes` | Limits & telemetry |
| `KB_VIDEO_MAX_BYTES` | `kb_video.max_bytes` | Limits & telemetry |
| `PROBLEM_REPORT_WORKSPACE` | `problem_report.workspace` | Limits & telemetry |
| (new) | `telemetry.enabled` | Limits & telemetry |
| (new) | `operator.require_totp` | Operators |
| (new) | `features.gmail`, `features.microsoft`, `features.autofix`, `features.ai_server`, `features.hosted_mail`, `features.cloud_sign_in`, `features.billing`, `features.signup` (unset: on once the feature's settings are complete; [Features](operator-console.md#features)) | Features |
| `POLAR_PRODUCT_TEAM_MONTHLY`, `…_TEAM_YEARLY`, `…_FOUNDING`, `…_{SOLO,SMALL,TEAM25}_{MONTHLY,YEARLY}` | the Polar products list (add-only; a product is taken off sale, never deleted) | Billing & retention |
| `SIGNUP_HASH_KEY` | kept in the database once, never changed (changing it forgets who had a trial); generated when it was not set | Sign-ups |

The first start of a release that keeps settings in the database imports the variables still set in
the environment, a group at a time (a group is valid whole or not imported at all), and from then on
the database wins: a variable left in `.env` is ignored and logged at every start, and `collie config
doctor` exits 1 until you remove it. See [upgrading](upgrade.md#settings-move-into-the-database).

### No key ring

An install made before the installer generated `MAILBOX_TOKEN_KEYS` has none. Secret settings cannot
be sealed then: they stay in the environment and are read from it, `collie config set-secret` refuses,
and the operator console locks their fields. Add a key ring to `.env` (above) and restart: the first
start with it imports the secrets from the environment, and you can remove them from `.env`.

## Usage report

Once a day the server sends a small usage report with counts and its public host name. What it
contains is listed in [usage report](../user/telemetry.md). To turn it off, set:

```sh
DISABLE_TELEMETRY=true
```

or `collie config set telemetry.enabled=false`. `DO_NOT_TRACK=1` works too. Any value other than empty, `0`, `false`, `no` or `off` turns the
report off; the environment can only turn the report off, never on. At start the server logs `usage report is off` with the reason, or `usage report is
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
docker compose exec server /app/collie config set ai.server.provider=anthropic \
  'ai.server.models=triage=claude-haiku-5-5,draft=claude-sonnet-5-5|claude-opus-5-5' \
  'ai.server.label=Company AI'          # optional; the name workspaces see (default "Server provider")
docker compose exec -T server /app/collie config set-secret ai.server.api_key < key.txt
# openai_compatible: also ai.server.base_url, with the version path, e.g. http://ollama:11434/v1
```

`ai.server.models` lists, per feature (`triage`, `draft`, `chat`, `intake`), the models a workspace may pick, separated
by `|`; the first is the default. A feature left out cannot use the server's provider. A change that
sets some of these settings but not all that are needed is refused and says which are missing. The server's provider can reach any address, your own network included: you chose it. No
workspace uses it until one of its admins turns a feature on.

To bound what the server's provider does for a workspace, give each workspace credits. One credit is one
result: a reply draft, a triage suggestion or an intake decision, whatever the model costs. The value is a
number of credits a month, optionally with a number for the whole trial of a workspace on its trial:

```sh
docker compose exec server /app/collie config set \
  ai.server.monthly_credits=1000,trial=500    # or just 1000
```

Credits start again on the first day of each UTC month; a trial's `trial=` credits count from the start
of the trial to its end and do not come back before. A task that is retried, or that intake runs again on
its fallback model, uses one credit. A call that fails, the help center's AI chat and the checks of a
provider use none. A running call holds its credit. Without `ai.server.monthly_credits` there is no
limit. Credits count results, not dollars, so `ai.server.prices` is optional: set it to see what the
server's provider costs in the usage records.

`ai.server.monthly_allowance` (dollars) is no longer read. While the database holds it, or
`AI_SERVER_MONTHLY_ALLOWANCE` is set during the first import, the AI settings are refused with a message
naming `ai.server.monthly_credits`, so a dollar amount is never read as credits. Replace it with
`collie config set ai.server.monthly_credits=…` and `collie config unset ai.server.monthly_allowance`.

`ai.server.prices` is `input:output[:cache_read[:cache_write]]` dollars per million tokens, as your provider
bills them, for example `ai.server.prices=claude-haiku-5-5=0.1:0.5,claude-sonnet-5-5=2:10:0.2`. A cache price
left out is a share of the input price, as Anthropic bills it: a read a tenth, a write (five minutes) 1.25
times; write `0` to make one free. Each call's cost is computed with the prices of the day it ran and
stored, so changing a price never rewrites a past month.

A provider a workspace adds can only reach public `https` addresses, so a workspace cannot make your
server call into your network. To let workspaces use a model server on your network (an Ollama next to
Colliedesk), set `ai.allow_private_base_urls=true`; a provider without a key is then accepted too. Never
set it on a server whose workspaces you do not trust.

## Problem reports

By default a [problem report](../user/problem-reports.md) on a self-hosted server sends nothing to
HC Labs; the dialog offers to open a GitHub issue. To receive the reports in your own support desk,
set `problem_report.workspace` to the address (slug) of a workspace on the same server:

```sh
docker compose exec server /app/collie config set problem_report.workspace=acme --yes
```

Reports from every other workspace on that server then arrive there as conversations tagged
`problem-report`.
