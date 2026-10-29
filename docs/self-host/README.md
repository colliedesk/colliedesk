# Self-host Colliedesk

Colliedesk is open source under the AGPL-3.0 and free to run on your own server. The self-hosted
server is the same product as Colliedesk Cloud: every feature works except billing, the free
trial, the sign-up page and the ready-made `support@<workspace>.colliedesk.com` address. You run
the server, its upgrades, its backups and its email delivery yourself.

How to use Colliedesk once it runs is in the [user guides](../user/README.md); they are the same on
Cloud and on your own server.

## Quick start

Install, create a workspace and its first admin:

```sh
curl -fsSL https://raw.githubusercontent.com/colliedesk/colliedesk/main/install.sh | sh -s -- \
  --name "My Support Desk" --email "admin@example.com"
```

Upgrade, in the directory the installer wrote (back up first, see [upgrading](#upgrading)):

```sh
docker compose pull && docker compose up -d
```

## Self-host or Colliedesk Cloud?

Choose [Colliedesk Cloud](https://colliedesk.com) if you want a support desk that works today:
HC Labs runs the server and its upgrades, and every workspace gets a support address that
receives mail without DNS or relay setup.

Self-host if you want the data on your own hardware, or you already run Docker and an SMTP relay.
You need to:

- keep the server upgraded; Colliedesk is alpha and releases often;
- back up PostgreSQL and the attachment files;
- connect a mail forwarder and an SMTP relay before customers can email you;
- put the server behind your own domain and reverse proxy;
- solve problems yourself: HC Labs does not offer support for self-hosted servers. See
  [support](support.md).

## Requirements

- A Linux host with Docker and Docker Compose v2.
- A domain name and a reverse proxy that terminates TLS, for access from outside the host. The
  server listens on `127.0.0.1` only; see [running behind a proxy](running-behind-proxy.md).
- An SMTP relay and a mail forwarder for the email channel; see [configuring email](email.md).

## Set up the stack

### With the installer

The [quick start](#quick-start) command writes `compose.yaml` and `.env` to `./colliedesk` (`--dir` picks
another directory), starts PostgreSQL and the server, and creates the workspace. It prints a one-time
URL. Open it to sign in as the admin. Running the installer again keeps your `.env` and data.

### From a clone

1. Start the stack:
   ```sh
   docker compose up -d --build
   ```
2. Create a workspace and its first admin:
   ```sh
   docker compose exec server /app/collie create-workspace \
     --name "Example Support" --admin-email "admin@example.com"
   ```
3. Open the one-time URL the command prints. It signs you in as the admin. The command also
   prints the workspace slug and the exact `pair` command for a new link; keep that line.

The link works once and expires after 15 minutes. To print a new one, see
[troubleshooting](troubleshooting.md#the-sign-in-link-expired).

After you sign in, the [setup guide](../user/setup-guide.md) at the top of the inbox lists the next steps: receive
customer email, import from Freshdesk, invite your team and answer your first email.

## Upgrading

Colliedesk is alpha: releases are `0.0.x` and any release may change the database schema. Every
release is tested by upgrading the release before it. Follow
[upgrade, roll back and restore a workspace](upgrade.md):

1. Back up PostgreSQL and the attachment files, and check the backup.
2. Pull the new image.
3. Run the migrations, before the server starts.
4. Start the server and check the version you now run.

If the new release fails, the same page restores the backup and the release you had, or brings back
one workspace without touching the others.

The installer writes the image to `COLLIE_DESK_IMAGE` in `.env`. Pick one tag:

- `ghcr.io/colliedesk/colliedesk:latest` (default): the newest release.
- `ghcr.io/colliedesk/colliedesk:0.0.N`: one exact release, if you want to pin.
- `ghcr.io/colliedesk/colliedesk:edge`: the newest `main` commit that passed CI and booted against
  PostgreSQL. Newer, less tested.

Changes per release are on the [releases page](https://github.com/colliedesk/colliedesk/releases).
Security fixes go to the latest release and to `edge` only.

## Configure

- [Environment variables](environment-variables.md)
- [Running behind a proxy](running-behind-proxy.md)
- [Configuring email](email.md)
- [Autofix: the GitHub App](github-app.md)
- [Backups and database](database.md)
- [Upgrade, roll back and restore a workspace](upgrade.md)

## Troubleshooting and support

- [Troubleshooting and FAQs](troubleshooting.md)
- [Support](support.md)

## Migrate

- [Migrate to Colliedesk Cloud](migrate-to-cloud.md)

To move from another helpdesk, see [import from Freshdesk](../user/import-freshdesk.md) and
[import from Help Scout](../user/import-helpscout.md).
