# Install and create a workspace

CollieDesk runs as one server plus PostgreSQL. Requirements: Docker with Compose v2.

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
   docker compose exec server /app/colliedesk create-workspace \
     --name "Example Support" --admin-email "admin@example.com"
   ```
3. Open the one-time URL the command prints. It signs you in as the admin.

The link works once and expires after 15 minutes. If it expires, print a new one:

```sh
docker compose exec server /app/colliedesk pair --workspace <slug> --email admin@example.com
```

Limits today: no email channel yet, so conversations are created through the API or UI.
See the [roadmap](../../ROADMAP.md).
