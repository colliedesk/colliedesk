# CollieDesk

**The open-source alternative to Freshdesk, Zendesk and Help Scout** for small support teams.
Leave Freshdesk with all your data. AI that actually answers tickets is included, not an add-on.
Flat price, no per-agent fees.

Think of CollieDesk as the sheepdog for your support queue: the AI herds every ticket home,
your team stays in charge.

**Status:** pre-alpha (v0.1). The interface and database schema may change before the first
stable release. See the [roadmap](ROADMAP.md).

## "Wait, what are you selling me?"

Self-hosting CollieDesk is free, forever, with every feature. We sell two things:

1. **CollieDesk Cloud**: we run it for you. One flat price per workspace, not per agent.
2. **Migration**: we move your Freshdesk history (tickets, replies, attachments, tags) and give
   you a reconcile report that proves nothing was lost.

AI features work with your own model key on self-hosted installs (**bring your own AI key**).
On the cloud you can bring your key or use ours at cost plus a small margin.

## If you don't like something, fork it

You were held hostage at renewal once. That should not happen again. CollieDesk is AGPL-3.0:
the code, your data and the export format are yours. If we ever go in the wrong direction,
you have everything you need to run it yourself or fork it.

## Install

Requirements: Docker with Compose v2.

```sh
docker compose up -d --build
docker compose exec server /app/colliedesk create-workspace \
  --name "Example Support" \
  --admin-email "admin@example.com"
```

Open the one-time URL printed by the command. CollieDesk does not use passwords. A one-command
installer is in progress; see the [roadmap](ROADMAP.md).

## What works today

- Passwordless sign-in with one-time pairing and invite links, revocable sessions.
- Shared inbox: reply, private notes, status, assignee, tags, search, pagination.
- Workspaces fully isolated from each other.
- Per-workspace timezone with per-agent override.

User guides: [docs/user](docs/user/README.md).

## Development

`make dev` runs the server and web app. `make test` needs PostgreSQL at the default test URL or
`TEST_DATABASE_URL`. Read [AGENTS.md](AGENTS.md) before changing code, whether you are a person
or a coding agent.

## License

Copyright (C) 2026 HC Labs. CollieDesk is licensed under the GNU Affero General Public License
v3.0 only (`AGPL-3.0-only`). See `LICENSE`. Contributions require the HC Labs CLA
(see `CONTRIBUTING.md`).
