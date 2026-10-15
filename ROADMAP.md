# Roadmap

We share what we are building and in what order. Dates are targets, not promises.
Feature details live in [docs/feature-spec.md](docs/feature-spec.md).

## Now: v0.1 (pre-alpha) — shipped

- Passwordless sign-in: one-time pairing and invite links, role ceilings, revocable sessions.
- Shared inbox: replies, private notes, status, priority, assignee, tags, search, pagination.
- Workspace isolation, per-workspace timezone with per-agent override.
- OpenAPI contract, end-to-end test, CI.

## Next: v0.2 — usable by a real team

Built on `main` and in the `edge` image, in testing:

- **Email channel**: inbound and outbound, threading, spoof and loop protection. Replies are
  plain text and go to the contact only (no CC) for now.
- **Freshdesk importer**: tickets (including archived), replies, notes, attachments
  (checksummed), tags, contacts, companies and time entries, with a reconcile report.
- **One-command install** and a published container image.

Still to do before v0.2 is released:

- **CollieDesk Cloud** for the first founding customers: billing switched on (the code is merged) and outgoing email on Cloud.
- Attachments on agent replies.
- Notifications for new and assigned tickets, and an inbox that updates without a reload.
- Remove an agent from a workspace.

## Then: v0.3 — AI included

- Draft replies and triage from your own past replies and knowledge base.
- Auto-answer for repetitive tickets, with a human in the loop.
- Usage metering; bring your own AI key.
- Knowledge base and public help center.
- Cloud sign-in with Google, Microsoft and email through Clerk; your workspace session stays ours, self-hosted installs keep one-time links.

## Later

- Automations, macros (canned replies), SLA and business hours, logging new time entries (v0.4).
- CSAT and reports (v0.5).
- Importers for Zendesk and Help Scout.
- MCP server and Agent Skills so your AI agent can work the queue (read / draft only / send).
- Live chat widget.

Have a need that is not here? Open an issue.
