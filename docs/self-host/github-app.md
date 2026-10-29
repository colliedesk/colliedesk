# Autofix: the GitHub App

Autofix sends a conversation to a coding agent your team already uses (Claude, Copilot or Cursor) on a
private GitHub repository, and follows it to a pull request and its merge. It works through a GitHub
App. Colliedesk Cloud has its own; a self-hosted server uses one you create, and Autofix stays hidden
until the server has its six settings (`GET /api/v1/config` names the missing ones).

How people use Autofix is in the [user guide](../user/autofix.md).

## Create the App

On GitHub, open **Settings → Developer settings → GitHub Apps → New GitHub App** (for an organization:
the organization's settings). Use `APP_ORIGIN` for `https://desk.example.com` below.

1. **GitHub App name**: any name; its slug (the name in the App's address) is `github.slug`.
2. **Homepage URL**: `https://desk.example.com`.
3. **Callback URL**: `https://desk.example.com/api/v1/autofix/github/callback`.
4. Tick **Expire user authorization tokens** and **Request user authorization (OAuth) during
   installation**. Colliedesk checks with the installing admin's own authorization that the
   installation is theirs, then revokes it at once; expiring tokens are a safety net.
5. **Webhook**: active, URL `https://desk.example.com/api/v1/webhooks/github`, and a random **Webhook
   secret** (`openssl rand -hex 32`), which is `github.webhook_secret`.
6. **Repository permissions**, nothing else:
   - Metadata: Read-only
   - Issues: Read and write
   - Pull requests: Read-only
7. **Subscribe to events**: Issues, Issue comment, Pull request, Repository. Installation events come
   by themselves.
8. **Where can this GitHub App be installed?**: *Any account* when teams of other GitHub accounts use
   your server; otherwise *Only on this account*.

Then, on the App's page: note the **App ID** and the **Client ID**, **Generate a new client secret**,
and **Generate a private key** (a `.pem` file).

The App can write issues and read pull requests of the repositories it is installed on. It cannot
read or write code. Keep its private key as secret as a password: whoever holds it can write issues on
every repository your teams connected.

## Settings

Save them with `collie config` (the secrets with `set-secret`, from standard input) or in the operator
console. All six are needed; a change that sets only some is refused and names what is missing.

```sh
docker compose exec server /app/collie config set github.app_id=123456 github.slug=acme-colliedesk github.client_id=Iv1.abc
docker compose exec -T server /app/collie config set-secret github.private_key < app.private-key.pem
docker compose exec -T server /app/collie config set-secret github.client_secret < client-secret.txt
docker compose exec -T server /app/collie config set-secret github.webhook_secret < webhook-secret.txt
```

| Setting | Value |
|---|---|
| `github.app_id` | The App ID. |
| `github.slug` | The App's slug, from its address `https://github.com/apps/<slug>`. |
| `github.client_id` | The Client ID. |
| `github.client_secret` | The client secret. |
| `github.private_key` | The private key: the `.pem` file as it is, or the file in base64 (`base64 < key.pem`). |
| `github.webhook_secret` | The webhook secret. |

A change is refused when `github.private_key` is not an RSA private key. The key is stored sealed with
`MAILBOX_TOKEN_KEYS`; no command, export or log shows it again.

## Behind a firewall

GitHub must reach `/api/v1/webhooks/github` for Autofix to follow fixes as they happen. A server
GitHub cannot reach still works: every ten minutes it reads, as the App, the installations, the
connected repositories and the issue or pull request of every fix in progress. Fixes then move up to
ten minutes late.

## Troubleshooting

- **Settings → Autofix says GitHub is not set up**: one of the six settings is missing; the page and
  `GET /api/v1/config` (`autofix.missing_settings`) name it.
- **Coming back from GitHub says the installation is not yours**: the person who started in Colliedesk
  must administer every repository the installation reaches on GitHub (an organization owner does; a
  member with access to some repositories does not), and finish in the same browser.
- **Connect GitHub or Continue on GitHub says the address is not secure**: the web app opens GitHub only
  over https (http only to the same machine, for a local stand-in). Nothing was sent; check that the server
  talks to `https://github.com`.
- **Fixes never move**: check the App's **Advanced** tab on GitHub for failed webhook deliveries (a
  wrong secret answers 401); fixes still move with the ten-minute check.
