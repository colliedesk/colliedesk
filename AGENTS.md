# CollieDesk

CollieDesk is an open-source helpdesk for small support teams (1–10 agents) leaving Freshdesk.
A Go server (PostgreSQL) serves a JSON API under `/api/v1` and an embedded React + TypeScript
web app. HC Labs sells the hosted cloud and migrations; self-hosting is free.

## What we never compromise on

1. **No data loss.** A migration or an edit never drops a ticket, reply, attachment, tag or
   timestamp silently. Every import ends with a reconcile report. A partial success is a failure.
2. **Workspace isolation.** No query, filter, search, id or file can cross from one workspace to
   another. Every new endpoint ships with a cross-workspace test.
3. **Fast.** A person waits at most 200 ms from action to usable screen. Budgets and gates are in
   `docs/internals/performance.md`. Never loosen a budget to pass CI.
4. **No lock-in.** The data model and the export format stay open and documented. Never add a
   feature that only works on the hosted cloud when it could work self-hosted.
5. **Open at the core.** Roadmap, decisions and code are public. Write as if a stranger will read
   it tomorrow.

## Glossary

- **workspace**: one customer team; every row belongs to exactly one.
- **agent**: a person on the support team who signs in. Roles: `admin`, `agent`.
- **contact**: an end customer who writes in.
- **conversation**: a ticket; has status, priority, assignee, tags.
- **message**: an item on a conversation timeline: `outgoing` reply or private `note`
  (`incoming` arrives with the email channel).
- **pairing link / invite link**: a one-time sign-in URL; stored only as a hash.
- **AI agent**: an external model client (Claude, Codex) working the queue through MCP. Not the
  same as a support agent.

## Three ways to hurt yourself

1. Writing a query without `workspace_id` in the filter. The tests will catch most of these;
   do not rely on that.
2. Changing the API without changing `contracts/openapi.yaml` first. The contract is the single
   source; web types are generated from it and CI fails when they drift.
3. Adding a one-way door: an action with no reverse (close without reopen, add without remove,
   invite without revoke) or no way to see its state.

## Hit every surface

- **Entry points:** a feature reachable in the UI is also reachable through the API, and later
  through MCP with the right permission tier (read / draft only / send).
- **Reverse states:** every way in has a way out and a way to see it.
- **Contracts:** anything crossing the wire is typed in `contracts/openapi.yaml`.
- **Docs:** a user-visible change updates its page in `docs/user/`. Internal docs record decisions
  and traps only (`docs/internals/`).

## Verifying

- Tests first (red → green), at the public seams: HTTP API, CLI, web components.
- Go tests use a real PostgreSQL (`TEST_DATABASE_URL`); never mock the database.
- `make test` before pushing; `make e2e` for user-visible flows.
- Market is international: timestamps are stored and returned in UTC; the UI formats them in the
  agent's IANA timezone. No country-specific defaults.

## Clean room

Do not open, copy, translate or paraphrase the source code of other helpdesks (in particular
Libredesk, AGPL). Product ideas come from public descriptions only. Code copied from
MIT-licensed projects needs its notice in `THIRD-PARTY-NOTICES.md`.
