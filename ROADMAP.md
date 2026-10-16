# Roadmap

We share what we are building and in what order. Dates are targets, not promises.
Feature details live in [docs/feature-spec.md](docs/feature-spec.md). Releases are listed in the
[changelog](CHANGELOG.md).

## Now: alpha (0.0.x)

Released as `0.0.x` and running on Colliedesk Cloud. Any release may change the interface or the
database schema.

- Passwordless sign-in: one-time pairing and invite links, role ceilings, revocable sessions;
  cloud sign-in through Clerk.
- Shared inbox: replies, private notes, status, priority, assignee, tags, search, pagination,
  bulk actions, saved replies, attachments, live updates and browser notifications.
- **Email channel**: inbound and outbound, threading, spoof and loop protection.
- **Freshdesk importer**: tickets (including archived), replies, notes, attachments
  (checksummed), tags, contacts, companies, time entries and canned responses, with a reconcile
  report.
- Team settings: invite, deactivate and reactivate agents.
- Workspace isolation, per-workspace timezone with per-agent override, full workspace export.
- **One-command install** and a signed container image.
- **Colliedesk Cloud**: 14-day free trial, then Team or Founding; a workspace whose trial ended is
  locked and can still export everything.

## Next: beta (0.1.0)

Beta starts when HC Labs announces it. Before that:

- Copy other people on a reply (Cc) and start a new conversation from the inbox.
- Contact history: every conversation of a customer in one place.
- An upgrade guide, and upgrades tested from the previous release on real data.
- A first team that moved from Freshdesk and works in Colliedesk every day.

## Then: AI included

- Draft replies and triage from your own past replies and knowledge base.
- Auto-answer for repetitive tickets, with a human in the loop.
- Usage metering; bring your own AI key.
- Knowledge base and public help center.
- MCP server and Agent Skills so your AI agent can work the queue (read / draft only / send).

## Later

- Automations, macros, SLA and business hours, logging new time entries.
- CSAT and reports.
- Importers for Zendesk and Help Scout.
- Live chat widget.

Have a need that is not here? Open an issue.
