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
- Import from Freshdesk inside the app, recent tickets first, with the reconcile report on screen
  ([#97](https://github.com/colliedesk/colliedesk/issues/97)).
- See who is viewing or replying to a conversation ([#101](https://github.com/colliedesk/colliedesk/issues/101)).
- One end-to-end test of the first-time path (sign up, import, first reply) on every change
  ([#105](https://github.com/colliedesk/colliedesk/issues/105)).
- A first team that moved from Freshdesk and works in Colliedesk every day.

## Then: AI included

- Draft replies and triage from your own past replies and knowledge base, with their sources shown
  ([#89](https://github.com/colliedesk/colliedesk/issues/89)).
- A backtest before you pay: how drafts would have answered your own past tickets ([#99](https://github.com/colliedesk/colliedesk/issues/99)).
- Results page and weekly report: before and after numbers measured against your imported history
  ([#100](https://github.com/colliedesk/colliedesk/issues/100)).
- Auto-answer only for topics where drafts proved accurate, with a human in the loop.
- Usage metering; bring your own AI key.
- Knowledge base and public help center, including your Freshdesk articles ([#90](https://github.com/colliedesk/colliedesk/issues/90)).
- MCP server and Agent Skills so your AI agent can work the queue (read / draft only / send).

## Later

- Keep Colliedesk in sync with Freshdesk until you switch, so there is no freeze day ([#98](https://github.com/colliedesk/colliedesk/issues/98)).
- Slack notifications ([#102](https://github.com/colliedesk/colliedesk/issues/102)).
- Simple rules on arrival: assign, tag, set priority ([#103](https://github.com/colliedesk/colliedesk/issues/103)).
- AI drafts that look up customer data through your MCP servers, read-only ([#104](https://github.com/colliedesk/colliedesk/issues/104)).
- Automations, macros, SLA and business hours, logging new time entries.
- CSAT and reports.
- Importers for Zendesk and Help Scout.
- Live chat widget.

Have a need that is not here? Open an issue.
