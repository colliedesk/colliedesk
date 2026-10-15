# Roadmap

We share what we are building and in what order. Dates are targets, not promises.
Feature details live in [docs/feature-spec.md](docs/feature-spec.md).

## Now: v0.1 (pre-alpha) — shipped

- Passwordless sign-in: one-time pairing and invite links, role ceilings, revocable sessions.
- Shared inbox: replies, private notes, status, priority, assignee, tags, search, pagination.
- Workspace isolation, per-workspace timezone with per-agent override.
- OpenAPI contract, end-to-end test, CI.

## Next: v0.2 — usable by a real team

- **Email channel**: inbound and outbound, threading, spoof and loop protection.
- **Freshdesk importer**: tickets, replies, notes, attachments (checksummed), tags, contacts,
  with a reconcile report.
- **One-command install** and a published container image.
- **Colliedesk Cloud** for the first founding customers.

## Then: v0.3 — AI included

- Draft replies and triage from your own past replies and knowledge base.
- Auto-answer for repetitive tickets, with a human in the loop.
- Usage metering; bring your own AI key.
- Knowledge base and public help center.
- Cloud sign-in with Google, Microsoft and email through Clerk; your workspace session stays ours, self-hosted installs keep one-time links.

## Later

- Automations, macros, SLA and business hours, time tracking (v0.4).
- CSAT and reports (v0.5).
- Importers for Zendesk and Help Scout.
- MCP server and Agent Skills so your AI agent can work the queue (read / draft only / send).
- Live chat widget.

Have a need that is not here? Open an issue.
