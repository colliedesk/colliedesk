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
3. Open the one-time URL the command prints. It signs you in as the admin. The command also
   prints the workspace slug and the exact `pair` command for a new link; keep that line.

The link works once and expires after 15 minutes. If it expires, print a new one:

```sh
docker compose exec server /app/colliedesk pair --workspace <slug> --email admin@example.com
```

After you sign in, the empty inbox links to the next steps: connect your support email, import
from Freshdesk, and invite your team. The top bar shows the workspace and the address you are signed
in as.

## Data to back up

CollieDesk keeps data in two places. Back up both:

- PostgreSQL (the `postgres-data` volume).
- Attachment files in `ATTACHMENTS_DIR` (the `attachments-data` volume, mounted at
  `/data/attachments`). Without a volume, files are lost when the container is recreated.

To receive and answer customer email, set up the [email channel](email-channel.md).
See the [roadmap](../../ROADMAP.md).
