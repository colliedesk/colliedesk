# Install and create a workspace

Colliedesk runs as one server plus PostgreSQL. Requirements: Docker with Compose v2.

## One command

```sh
curl -fsSL https://raw.githubusercontent.com/colliedesk/colliedesk/main/install.sh | sh -s -- \
  --name "My Support Desk" --email "admin@example.com"
```

Re-running the installer keeps your existing `.env`. The steps below do the same by hand from a
clone of the repository.

## From a clone

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

The link works once and expires after 15 minutes. If it expires, print a new one:

```sh
docker compose exec server /app/collie pair --workspace <slug> --email admin@example.com
```

After you sign in, the empty inbox links to the next steps: connect your support email, import
from Freshdesk, and invite your team. A bar at the top of every page shows the workspace and the address you are signed
in as.

## Image channels and upgrades

The installer writes `COLLIE_DESK_IMAGE` to `.env`. Pick one tag:

- `ghcr.io/colliedesk/colliedesk:latest` (default): the newest release.
- `ghcr.io/colliedesk/colliedesk:0.0.N`: one exact release, if you want to pin.
- `ghcr.io/colliedesk/colliedesk:edge`: the newest `main` commit that passed CI and booted against
  PostgreSQL. Newer, less tested.

Colliedesk is alpha: releases are `0.0.x` and any release may change the database schema. Back up
before you upgrade (see [back up and database](database.md)). Changes per release are in the
[releases page](https://github.com/colliedesk/colliedesk/releases).

To check which build you run:

```sh
docker compose exec server /app/collie version
```

## Next steps

- Receive and answer customer email: [email on your server](email.md).
- Put the server behind your domain and proxy: [configuration](configuration.md#reverse-proxy).
- The install sends a daily usage report with counts only; [turn it off](configuration.md#usage-report)
  if you prefer.
- Then use Colliedesk as described in the [user guides](../user/README.md).
