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

**Operators and sessions.** `collie operator list`, `collie operator revoke <email>` (every session and
token stops at once; `pair` lets them in again) and `collie operator sessions [--revoke <id>]`.

## Its own address

By default the console is at `APP_ORIGIN/operator/`, the same address as the workspace app. Give it its
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
| `list [--group G]` | Every setting with its value, its default and who set it in which version. Groups: `ai`, `email`, `mailboxes`, `billing`, `signups`, `security`, `limits`, `operator`. |
| `set KEY=VALUE…` | Saves settings as a new version. A setting that changes what the server does for every workspace (billing, retention, help center domain, trusted proxies, the problem report workspace, video size) needs `--yes`. |
| `set-secret KEY` | Saves a secret read from standard input, so it never sits in your shell history. |
| `unset KEY…`, `reset KEY…` | Removes settings: they go back to their default. |
| `import FILE`, `import --stdin`, `import --from-env` | Imports lines written like `.env` (the variable names). `--only-missing` (the default) keeps a group the database already holds; `--replace` replaces every group the file sets. |
| `export [--format env\|json] [--include-secrets]` | Writes the configuration out. Secrets only with `--include-secrets`, with a warning: keep that file like a password. |
| `apply FILE [--dry-run]` | Makes the configuration match a JSON file (as `export --format json` writes it) and prints the difference. Settings the file leaves out are unset; secrets it leaves out are kept. Running it again changes nothing. |
| `history` | Lists the versions: number, time, how (`env-import`, `cli`, `apply-file`, `restore`…), who, status, settings changed. |
| `restore VERSION` | Saves an older version again as the newest one. |
| `doctor` | Checks the configuration. Exit code 0: clean. 1: warnings (a setting still in the environment, a variable no release reads, a secret sealed with an older key, no key ring). 2: errors (a version the server refused). |
| `reseal` | Seals every secret again with the first key of `MAILBOX_TOKEN_KEYS`, after you add a new key in front. |

Every command that writes takes `--by NAME` for the audit log (default: your user name). A key is named
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
