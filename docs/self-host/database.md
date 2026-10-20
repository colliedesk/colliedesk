# Backups and database

Colliedesk keeps its data in PostgreSQL and in a directory of attachment files. This page covers
backups, database requirements and the database users.

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

A workspace admin can also download everything in one workspace from the web app; see
[export all your data](../user/export.md).

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
