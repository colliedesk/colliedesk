# Operator console and settings

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
