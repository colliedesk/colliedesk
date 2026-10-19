# Colliedesk

**The open-source alternative to Freshdesk, Zendesk and Help Scout** for small support teams.
Leave Freshdesk with all your data. AI that actually answers tickets is included, not an add-on.
Flat price, no per-agent fees.

Think of Colliedesk as the sheepdog for your support queue: the AI herds every ticket home,
your team stays in charge.

**Status:** alpha (0.0.x). The interface and database schema may change in any release; every
release ships the migrations to upgrade, and export always works. See the [roadmap](ROADMAP.md)
and [versioning](#versioning).

## "Wait, what are you selling me?"

Self-hosting Colliedesk is free, forever, with every feature. We sell two things:

1. **Colliedesk Cloud**: we run it for you. One flat price per workspace, not per agent. Every new
   workspace starts with a 14-day free trial, no card needed; there is no permanent free plan. When
   the trial ends the workspace is locked, never deleted, and you can always export all your data.
2. **Migration**: we move your Freshdesk history (tickets, replies, attachments, tags) and give
   you a reconcile report that proves nothing was lost.

AI features work with your own model key on self-hosted installs (**bring your own AI key**).
On the cloud you can bring your key or use ours at cost plus a small margin.

## If you don't like something, fork it

You were held hostage at renewal once. That should not happen again. Colliedesk is AGPL-3.0:
the code, your data and the export format are yours. If we ever go in the wrong direction,
you have everything you need to run it yourself or fork it.

## Install

Requirements: Docker with Compose v2.

```sh
curl -fsSL https://raw.githubusercontent.com/colliedesk/colliedesk/main/install.sh | sh -s -- \
  --name "My Support Desk" --email "admin@example.com"
```

The installer starts Colliedesk and PostgreSQL, keeps your secrets in a local `.env`, and prints a
one-time sign-in URL. Colliedesk does not use passwords. Details: [docs/self-host](docs/self-host/README.md).

## What works today

- Passwordless sign-in with one-time pairing and invite links, optional cloud sign-in, and revocable sessions.
- Shared inbox: reply, private notes, status, assignee, tags, search, pagination.
- Workspaces fully isolated from each other.
- Per-workspace timezone with per-agent override.

User guides: [docs/user](docs/user/README.md). Running your own server: [docs/self-host](docs/self-host/README.md). Both are also published at [colliedesk.com/docs](https://colliedesk.com/docs) by [colliedesk/colliedesk.com](https://github.com/colliedesk/colliedesk.com).

## Development

`make dev` runs the server and web app. `make test` needs PostgreSQL at the default test URL or
`TEST_DATABASE_URL`. Read [AGENTS.md](AGENTS.md) before changing code, whether you are a person
or a coding agent.

## Versioning

Colliedesk uses [Semantic Versioning](https://semver.org). While it is alpha, releases are
`0.0.x`: any release may change the interface or the database schema. Beta starts at `0.1.0`,
when HC Labs announces it. Merging to `main` does not make a release: HC Labs cuts one by hand,
from the build that already runs on Colliedesk Cloud. Changes per release are on the
[releases page](https://github.com/colliedesk/colliedesk/releases).

Container image tags:

- `latest`: the newest release. The installer uses it by default.
- `0.0.x`: one exact release, if you want to pin.
- `edge`: the newest `main` commit that passed CI. Newer, less tested.

## License

Copyright (C) 2026 HC Labs. Colliedesk is licensed under the GNU Affero General Public License
v3.0 only (`AGPL-3.0-only`). See `LICENSE`. Contributions require the HC Labs CLA
(see `CONTRIBUTING.md`).
