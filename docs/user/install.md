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

## Image channels

The installer writes `COLLIE_DESK_IMAGE` to `.env`. Pick one tag:

- `ghcr.io/colliedesk/colliedesk:edge` (current default): the latest `main` commit that passed CI
  and booted against PostgreSQL.
- `ghcr.io/colliedesk/colliedesk:X.Y.Z`, `:X.Y` or `:latest`: tagged releases.

To check which build you run:

```sh
docker compose exec server /app/collie version
```

The command was called `colliedesk` before it was renamed to `collie`. Images still ship
`/app/colliedesk` as a link to `/app/collie`, so a compose file written by an older installer
keeps working.

## Data to back up

Colliedesk keeps data in two places. Back up both:

- PostgreSQL (the `postgres-data` volume).
- Attachment files in `ATTACHMENTS_DIR` (the `attachments-data` volume, mounted at
  `/data/attachments`). Without a volume, files are lost when the container is recreated.

PostgreSQL row-level security keeps each workspace's rows apart. Because of it:

1. Run `pg_dump` as a superuser: `postgres` for installs made with the installer, `colliedesk`
   for older installs and for the repository's `compose.yaml`.
2. Before you restore into a new PostgreSQL server, create the role that Colliedesk uses for
   requests. Its name is `colliedesk_app_` followed by the database name:
   ```sql
   CREATE ROLE colliedesk_app_colliedesk NOLOGIN;
   GRANT colliedesk_app_colliedesk TO colliedesk;
   ```
   Replace the second `colliedesk` with your `DATABASE_URL` user if it is different.

## Database requirements and hardening

An external database must be PostgreSQL 16 or newer.

The installer connects Colliedesk with two users:
- `colliedesk` owns the data and runs migrations. It is not a superuser.
- `colliedesk_request` serves requests and can only read the signed-in workspace.

Installs made before this layout keep one superuser connection and upgrade without changes. To
add the request user to such an install:

1. Add one line to `.env`, with a new random password:
   ```sh
   DATABASE_REQUEST_URL=postgres://colliedesk_request:<password>@postgres:5432/colliedesk?sslmode=disable
   ```
2. Run `docker compose up -d`. The server creates the user on start.

The server opens at most 20 database connections per pool. To change that, add
`pool_max_conns=N` to `DATABASE_URL` or `DATABASE_REQUEST_URL`, for example
`...?sslmode=disable&pool_max_conns=40`. Keep it well above 2, the number of workspace exports the
server runs at once.

When you remove an install from a shared PostgreSQL server, also remove its two users. Dropping
the database does not remove them. As a superuser, after dropping the database:

```sql
DROP ROLE colliedesk_app_colliedesk;
DROP ROLE colliedesk_request;
```

Details: [workspace isolation](../internals/workspace-isolation.md).

## Usage report

The install sends a daily usage report: counts and its public host name, never names, emails or
ticket content. Add `DISABLE_TELEMETRY=true` to `.env` and run `docker compose up -d` to turn it
off. Every field is listed in [usage report](telemetry.md).

To receive and answer customer email, set up the [email channel](email-channel.md).
See the [roadmap](../../ROADMAP.md).
