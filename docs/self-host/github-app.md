# Autofix: the GitHub App

Autofix sends a conversation to a coding agent your team already uses (Claude, Copilot or Cursor) on a
private GitHub repository, and follows it to a pull request and its merge. It works through a GitHub
App. Colliedesk Cloud has its own; a self-hosted server uses one you create, and Autofix stays hidden
until the server has its six settings (`GET /api/v1/config` names the missing ones).

How people use Autofix is in the [user guide](../user/autofix.md).

## Create the App

On GitHub, open **Settings → Developer settings → GitHub Apps → New GitHub App** (for an organization:
the organization's settings). Use `APP_ORIGIN` for `https://desk.example.com` below.

1. **GitHub App name**: any name; its slug (the name in the App's address) is `GITHUB_APP_SLUG`.
2. **Homepage URL**: `https://desk.example.com`.
3. **Callback URL**: `https://desk.example.com/api/v1/autofix/github/callback`.
4. Tick **Expire user authorization tokens** and **Request user authorization (OAuth) during
   installation**. Colliedesk checks with the installing admin's own authorization that the
   installation is theirs, then revokes it at once; expiring tokens are a safety net.
5. **Webhook**: active, URL `https://desk.example.com/api/v1/webhooks/github`, and a random **Webhook
   secret** (`openssl rand -hex 32`), which is `GITHUB_APP_WEBHOOK_SECRET`.
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

| Variable | Value |
|---|---|
| `GITHUB_APP_ID` | The App ID. |
| `GITHUB_APP_SLUG` | The App's slug, from its address `https://github.com/apps/<slug>`. |
| `GITHUB_APP_CLIENT_ID` | The Client ID. |
| `GITHUB_APP_CLIENT_SECRET` | The client secret. |
| `GITHUB_APP_PRIVATE_KEY` | The private key: the `.pem` file's text, the same on one line with its line breaks written `\n`, or the file in base64 (`base64 < key.pem`). |
| `GITHUB_APP_WEBHOOK_SECRET` | The webhook secret. |

The server refuses to start when `GITHUB_APP_PRIVATE_KEY` is set and is not an RSA private key. The key
stays in the environment; nothing of it goes into the database.

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
- **Fixes never move**: check the App's **Advanced** tab on GitHub for failed webhook deliveries (a
  wrong secret answers 401); fixes still move with the ten-minute check.
