# CollieDesk

CollieDesk is an open-source helpdesk for small support teams leaving Freshdesk. It keeps customer conversations portable, uses flat workspace pricing for the hosted product, and is designed to include useful AI assistance without a per-agent add-on.

**Status:** pre-alpha. The v0.1 interface and database schema may change before the first stable release.

## Quickstart

Requirements: Docker with Compose.

```sh
docker compose up -d --build
docker compose exec server /app/colliedesk create-workspace \
  --name "Example Support" \
  --admin-email "admin@example.com"
```

Open the one-time URL printed by the command. CollieDesk does not use passwords.

For local development, run `make dev`. Run `make test` with PostgreSQL available at the default test URL or set `TEST_DATABASE_URL`.

## License

Copyright (C) 2026 HC Labs. CollieDesk is licensed under the GNU Affero General Public License v3.0 only (`AGPL-3.0-only`). See `LICENSE`.
