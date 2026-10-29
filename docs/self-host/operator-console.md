# Operator console and settings

The operator console is where the people who run a server (operators) sign in, separately from every
workspace. An operator is not a workspace admin: a workspace session never opens it, and an operator
session never opens a workspace.

## Sign in

The installer prints a one-time link for the admin email when it installs:
`Operator console (one-time link): …`. Print a new one at any time on the server (15 minutes, works once):

```sh
docker compose exec server /app/collie operator pair --email you@example.com
```

Opening the link shows a page with **Continue**; nothing is used until you press it, so a link scanner in
your mail cannot spend it. A browser session lasts 12 hours.

**Authenticator (TOTP).** With `operator.require_totp` on (always on the hosted cloud), the first sign-in
shows a QR code and the key as text for an authenticator app, then asks for its 6-digit code; later
sign-ins ask for the code. Ten wrong codes lock sign-in for 15 minutes. Turn it on with
`collie config set operator.require_totp=true`. Lost the authenticator: print a link that replaces it
(the shell is the root of trust):

```sh
docker compose exec server /app/collie operator pair --email you@example.com --reset-totp
```

A dangerous change (a secret, the billing switch, a new token…) asks for the code again; an operator
without an authenticator passes only within 15 minutes of signing in with a fresh link.

**Sign in with your email (cloud sign-in).** On a server with cloud sign-in configured (the
`cloud_auth.*` settings and `clerk.publishable_key`, as on the hosted cloud), the console's sign-in page
also offers the same email-and-code sign-in as the workspace app. Only operators get in: an email that is
not an operator, or a revoked one, is refused with the same message as any other refusal. After the email
code the console always asks for the authenticator (it is set up on the first sign-in), whatever
`operator.require_totp` says, so a mailbox alone never opens the console. Add an operator without a link:

```sh
docker compose exec server /app/collie operator add --email teammate@example.com
```

The identity provider must issue the console its own session tokens: their authorized party (`azp`) is
the console's origin, `OPERATOR_ORIGIN`. The console accepts only those, and the workspace app keeps
accepting only the origins in `cloud_auth.authorized_parties`: never add the console's origin there. With
Clerk, the console's host must be a subdomain of the instance's domain (`ops.example.com` beside
`app.example.com`), and if the instance restricts which subdomains may call its Frontend API, the
console's host must be on that list. Without cloud sign-in nothing changes: the console opens with a
link. Signing out of the console also signs you out of the identity provider in that browser.

**Operators and sessions.** `collie operator list`, `collie operator add --email <email>` (an operator
without a link), `collie operator revoke <email>` (every session and token stops at once; `add` or `pair`
lets them in again) and `collie operator sessions [--revoke <id>]`.

## Its own address

By default the console is at `APP_ORIGIN/operator/`, the same address as the workspace app. That suits a
self-hosted server run by the team that uses it; a server that hosts other teams (billing and cloud
sign-in on) must give the console its own host: `collie config doctor` reports it as an error otherwise. Give it its
own host to keep a bug in the workspace app from reaching it: set `OPERATOR_ORIGIN` in `.env` (for
example `https://ops.example.com`), point that host at the server through your reverse proxy, and run
`docker compose up -d`. The installer takes `--operator-origin https://ops.example.com` on a new install.
That host then serves only the console, the operator API and `/mcp/operator`; the app host serves none of
them. Over https the session cookie is `__Host-colliedesk_operator`, which no other host can set or read.

## Operator tokens and MCP

An AI client can help run the server with an operator token (`cdop_…`): `collie operator token create
--email you@example.com --label "nightly check"` (printed once, 30 days) or the console. It connects to
`APP_ORIGIN/mcp/operator` (or `OPERATOR_ORIGIN/mcp/operator`), never to `/mcp`, and a workspace token never
reaches it ([operator tools](../user/mcp.md#operator-tools)). A token reads; every change it asks for waits
as a request an operator approves with their code. A token never sends a secret: it names the secrets a
change needs, and the operator types them when approving.

## The console's screens

A dark band on top is always there: the console's host and the version every process runs
(**Running v12 · 2 of 2 processes**, **Applying v13…** while a save reaches them, **v13 refused** when a
process could not use it). On a phone it keeps the host and the version.

- **Overview**: the release and database, the version to run and how many processes run it, what needs
  attention (a variable that still differs from the database, a secret sealed with an old key, a request
  waiting), each process, and the server environment (read at start; change it in `.env` and restart).
- **Features**: a switch for each feature workspaces see (see [Features](#features) below).
- **Configuration pages** (AI and pricing, Email, Mailboxes and GitHub, Billing and retention, Sign-ups,
  Security and network, Limits and telemetry): each setting with its key, where its value comes from, and
  when a save applies. Changes stay in the tab until **Review and save**; the review lists every change
  and asks for what it needs (your code, a secret entered again, a typed confirmation). **Escape** or
  **Cancel** applies nothing. Email has **Send a test email**, which tries the settings on the page
  before anything is saved. If someone saved another version meanwhile, nothing is saved and your
  changes are kept to apply on the newest one. A session that ends keeps them in the tab too.
- **Requests**: changes an operator token asked for; approve (with your code and any secret it needs)
  or reject.
- **History**: every version, who saved it and which keys changed (secret values never show). Restoring
  one saves it again as the newest version.
- **Audit log**: who changed the server and its operators, filtered by setting, person or date, and
  exported as CSV.
- **Operators and sessions**: invite and revoke operators, see and end sessions, create tokens (shown once).

The billing page also lists the **Polar products** the server sells. A product is added and put on or
taken off sale (one per plan and period is on sale), never removed or changed, so an old subscription
always maps to its plan.

## What a dangerous change asks

What a change asks depends on what it does, and the server checks it again when it saves (a token's
request and the API pass the same gates). The review shows each of these with its numbers before anything
is saved:

| Change | What the review shows | What you type |
|---|---|---|
| Billing on (`polar.access_token` set where it was empty) | how many workspaces whose trial ended without a plan lock at once | `billing on` |
| Billing off | every workspace gets full access, no trial and no lock | `billing off` |
| `retention.delete` on, with billing on | how many workspaces locked for 12 months are due now, and the daily cap (at most 20) | that number |
| `email.from_policy` = any | any workspace may send as any address through your relay | `any` |
| A mail domain already in use changed | replies to mail sent before stop threading | the new domain |
| `ai.allow_private_base_urls` on | workspace admins can point the AI provider inside your network | `allow private addresses` |
| `problem_report.workspace` | reports of every workspace become conversations there (a slug nobody has is refused) | — |
| Gmail or Microsoft 365 switched off | how many email channels in how many workspaces send through it; their replies wait | `gmail off`, `microsoft off` (only when a channel uses it) |
| The server's AI provider switched off | how many workspaces run an AI feature on it | — |
| Billing switched off (`features.billing`) | nobody is billed or locked, retention deletes nothing, subscriptions stay | `billing off` |
| Billing switched on again | how many workspaces whose trial ended without a plan lock at once | `billing on` |
| Cloud sign-in switched off (a trial) | nobody signs in to the app or signs up with it; the console still opens with a one-time link | `cloud sign-in off` |

Rotating the Polar token while billing stays on, turning these back off, and the other settings ask only
for your code when they need it. The authenticator, once required, is turned off on the server only.

## Features

The **Features** page has a switch for each feature workspaces see that needs the server's settings:
Gmail and Microsoft 365 mailboxes, Autofix, the server's AI provider, hosted support addresses, cloud
sign-in, billing and cloud sign-up. A switch is the setting `features.<name>` (`true` or `false`), saved,
versioned and restored like any other.

- **Unset means "on once its settings are complete".** An upgrade sets no switch: every feature that ran
  before still runs, every feature that missed settings stays off.
- **A feature cannot be switched on before its settings are complete.** The switch is greyed out and
  lists what is missing, each item a link to its page; the server refuses `true` the same way
  (`config_invalid`, and `collie config set` exits with the names).
- **Settings that go missing later turn the feature off by themselves**, as if it were switched off (a Gmail
  or Microsoft 365 reply waits with the reason instead of failing). The save is not refused; the
  Overview and `collie config doctor` warn that the feature is switched on but off, and name what it needs.
- **Off hides the feature and refuses its use; it removes nothing.** Workspaces no longer see it, and its
  API routes and MCP tools answer `409 feature_off`. Connections, repositories, fixes, subscriptions,
  hosted addresses and queued replies stay as they are, and switched on again the feature carries on:
  - Gmail and Microsoft 365: replies of a channel connected to it wait in the outbox, with the reason on the
    conversation, and are sent once it is on again. Disconnecting still works. Microsoft's weekly keep-alive
    still runs, so its connections are alive when you switch it on again.
  - Autofix: nothing new is sent and no repository or installation is added; fixes already sent are still
    followed to their end and can be cancelled, marked deployed or given their pull request, and GitHub's
    webhook is still received.
  - The server's AI provider: features set to it stop and say why; a workspace's own provider is not
    affected. The help center hides its chat while its AI cannot answer, as before.
  - Hosted addresses: no channel is offered one (new workspaces get none); a channel already at its hosted
    address keeps it, and mail to hosted addresses still arrives.
  - Billing: no workspace is billed or locked and trials are not enforced, retention deletes nothing, and
    nothing new can be bought (checkout answers `feature_off`). A workspace that has or had a subscription
    still sees **Plan and billing**, read-only with **Manage billing** to change or cancel it in the portal
    (a subscription can always be ended); one that never had one does not see the page. Subscriptions stay as they are at Polar and Polar's webhook is
    still received, so they stay in step. Switched on again, a workspace whose trial ended without a plan
    locks at once (the review counts them first). The server still counts as the hosted cloud
    (an authenticator stays required, the console must keep its own address).
  - Cloud sign-in: nobody signs in to the app or signs up with it; sessions already open keep working. It is
    a trial change. The operator console still opens with a one-time link (`collie operator pair`) and with
    its own cloud sign-in, so switching it off never locks you out.
  - Cloud sign-up: nobody can start a trial; it is also off while cloud sign-in or billing is.
- A switch applies in seconds, like the settings it rides with; while an import runs in the app it waits for
  the import to end.

`collie config features` lists each switch, whether the feature runs, and what it still needs.

The help center's own address (`help_center.domain`), outgoing mail (SMTP and the reply domain) and Resend
have no switch: switching them off would break public links or every email reply at once. Their settings
are the switch.

## Trial changes

A change of **trusted proxies**, **cloud sign-in** or the **help center domain** can lock everyone out, so it
is tried first: saved as a trial, it runs for 10 minutes with a countdown under the band. Confirm it from
the console (that it still answers proves you can reach it) or revert it at once; if nobody confirms in
time, any running server saves the version before it again by itself (`trial-auto-revert` in History),
and a server that starts after the time is up reverts it before serving. While a trial runs, the console
saves nothing else; `collie config` on the server (`set`, `reset`, `apply`, `restore`) always works and
ends the trial (`trial.superseded` in the audit log), so the shell stays the way back if a trial locks you
out. The band shows what the server saw meanwhile: your own address under the new proxies and how
many addresses it saw (one for everyone means the list is wrong), and the cloud sign-ins that worked or were
refused. From the shell: `collie config trial` shows it, `collie config trial confirm` keeps it and
`collie config trial revert` ends it now.

## Settings

The server's settings (email, AI, mailboxes, GitHub, billing, proxies, limits) live in its database,
not in the environment. Every change is a version: nothing is lost, and any version can be brought
back. This page covers `collie config`, the command line for them; it works on any install, from the
server container, whether the server runs or not.

```sh
docker compose exec server /app/collie config <command>
```

## Commands

| Command | What it does |
|---|---|
| `get [KEY]` | Prints every setting, or one. A secret prints `(set)` or `(not set)`, never its value. |
| `list [--group G]` | Every setting with its value, its default and who set it in which version. Groups: `features`, `ai`, `email`, `mailboxes`, `billing`, `signups`, `security`, `limits`, `operator`. |
| `set KEY=VALUE…` | Saves settings as a new version. A setting that changes what the server does for every workspace (billing, retention, help center domain, trusted proxies, the problem report workspace, video size) needs `--yes`. |
| `set-secret KEY` | Saves a secret read from standard input, so it never sits in your shell history. |
| `unset KEY…`, `reset KEY…` | Removes settings: they go back to their default. |
| `import FILE`, `import --stdin`, `import --from-env` | Imports lines written like `.env` (the variable names). Lines are read as Compose reads `.env` (an unquoted value ends at ` #`). `--only-missing` (the default) keeps a group the database holds or once held, so a group you unset stays unset whatever `.env` still says; `--replace` replaces every group the file sets, and keeps a secret the file leaves out (as `apply` does). |
| `export [--format env\|json] [--include-secrets]` | Writes the configuration out. Secrets only with `--include-secrets`, with a warning: keep that file like a password. |
| `apply FILE [--dry-run]` | Makes the configuration match a JSON file (as `export --format json` writes it) and prints the difference. Settings the file leaves out are unset; secrets it leaves out are kept. Running it again changes nothing. |
| `history` | Lists the versions: number, time, how (`env-import`, `cli`, `apply-file`, `restore`…), who, status, settings changed. |
| `restore VERSION` | Saves an older version again as the newest one. |
| `trial [confirm\|revert]` | Shows the trial change that runs (a console change of trusted proxies, cloud sign-in or the help center domain), keeps it, or ends it now. The console saves nothing else while one runs; `set`, `reset`, `apply` and `restore` here end it. |
| `features` | Lists each feature switch: whether the feature runs, the switch (`on`, `off` or `unset`) and what it still needs. |
| `doctor` | Checks the configuration. Exit code 0: clean. 1: warnings (a setting still in the environment, a variable no release reads, a secret sealed with an older key, no key ring, a trial change that runs, a feature switched on that misses settings). 2: errors (a version the server refused). |
| `reseal` | Seals every secret again with the first key of `MAILBOX_TOKEN_KEYS`, after you add a new key in front. |

Every command that writes takes `--by NAME` for the audit log (default: your user name, or `uid-<n>` in the server container, whose user has no name). A key is named
by its dotted name (`smtp.host`) or by the variable it replaced (`SMTP_HOST`); the
[settings table](environment-variables.md#settings) lists them all.

## How a change applies

A change is checked the way the server reads it at start: a group set only in part is refused and names
what is missing, and nothing is saved. A saved version becomes the one to run, and every running server
applies it within a few seconds (it checks every 5 seconds and is woken at once by the database), without
closing its port:

1. Settings read at each use apply at once: internal sign-up addresses and networks, the billing alert
   address, the problem report workspace, the video and email image limits.
2. The others restart the server's workers inside the process (re-serve): requests in progress finish,
   new ones use the new settings, and email, AI and sync pause for a moment (their queues are in the
   database). A long MCP connection keeps the old settings until its client connects again.
3. While an import runs in the app on that server, the restart waits for it to end (the settings of 1.
   apply anyway) and then happens by itself.

A version that does not build (written by an older tool, for example) never stops the server: it is
refused, `collie config doctor` exits 2 and says why, and the server keeps running the version before it
(at start: the last version that ran, else every default). Each running server records the version it
runs; `server_processes` lists them.

## Secrets

Secrets are sealed with `MAILBOX_TOKEN_KEYS` and bound to their setting, so a sealed value copied to
another setting does not open. They are write-only: no command, export, history or log shows them,
except `collie config export --include-secrets`. Validation errors name the setting, never its value.

Without a key ring (installs from before the installer generated one) secrets cannot be sealed: they
stay in `.env` and are read from there, and `set-secret` refuses. Add a key ring and restart; the first
start imports them.

## Products and the sign-up key (hosted cloud)

The Polar products a server sells are a list you add to, never edit: a subscription's webhook names the
product it was bought as, so a product id always maps to the same plan. Taking a product off sale keeps
its subscriptions on their plan. The sign-up hash key is stored once (imported from `SIGNUP_HASH_KEY`,
or generated) and never changes: a new key would forget who already had a trial.

## The JSON export

```json
{
  "format": "colliedesk-config/1",
  "version": 12,
  "settings": {"smtp.host": "smtp.example.net", "trusted_proxies": "10.0.0.5"},
  "secrets": {"smtp.password": "…"},
  "products": [{"product_id": "prod_…", "plan": "team", "period": "monthly", "on_sale": true}]
}
```

`settings` and `secrets` are keyed by dotted name; `secrets` is there only with `--include-secrets`.
`products` is the full list, for reading; `apply` does not change it.

## Rescue

`collie config` reads and writes the database directly: it works while the server is stopped or
refuses its configuration. To undo a change, `collie config history` then `collie config restore
<version>`; to put one setting back to its default, `collie config reset <key>`.
