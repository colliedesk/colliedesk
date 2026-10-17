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
  bulk actions, saved replies, attachments, Cc on replies, live updates and browser notifications.
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

- Contact history: every conversation of a customer in one place, and starting a new conversation
  from the inbox ([#92](https://github.com/colliedesk/colliedesk/issues/92)).
- Connect a Gmail / Google Workspace or Microsoft 365 mailbox with one click (OAuth); replies
  leave from your own address. Forwarding keeps working ([#95](https://github.com/colliedesk/colliedesk/issues/95)).
- An MCP server so your AI agent (Claude, Codex, Cursor) can work the queue, with read, draft-only
  and send tokens ([#112](https://github.com/colliedesk/colliedesk/issues/112)).
- An upgrade guide, and upgrades tested from the previous release on real data ([#91](https://github.com/colliedesk/colliedesk/issues/91)).
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
- Connect any agent host in one step, with setup docs and an Agent Skills pack ([#113](https://github.com/colliedesk/colliedesk/issues/113)).
- Reply drafts that a person reviews and sends, from AI agents or teammates ([#114](https://github.com/colliedesk/colliedesk/issues/114)).
- Everything you can do in the inbox, your AI agent can do too, checked in CI ([#115](https://github.com/colliedesk/colliedesk/issues/115)).

## Later

- Keep Colliedesk in sync with Freshdesk until you switch, so there is no freeze day ([#98](https://github.com/colliedesk/colliedesk/issues/98)).
- Slack notifications ([#102](https://github.com/colliedesk/colliedesk/issues/102)).
- Simple rules on arrival: assign, tag, set priority ([#103](https://github.com/colliedesk/colliedesk/issues/103)).
- AI drafts that look up customer data through your MCP servers, read-only ([#104](https://github.com/colliedesk/colliedesk/issues/104)).
- Macros: a saved reply plus actions in one step ([#117](https://github.com/colliedesk/colliedesk/issues/117)).
- SLA targets, business hours and overdue alerts ([#118](https://github.com/colliedesk/colliedesk/issues/118)).
- Log time on conversations and export it per company ([#119](https://github.com/colliedesk/colliedesk/issues/119)).
- Customer satisfaction rating after a conversation is resolved ([#120](https://github.com/colliedesk/colliedesk/issues/120)).
- Reports: volume, first response and resolution time per agent and tag ([#121](https://github.com/colliedesk/colliedesk/issues/121)).
- Importers for Zendesk ([#122](https://github.com/colliedesk/colliedesk/issues/122)) and Help Scout ([#123](https://github.com/colliedesk/colliedesk/issues/123)).
- Live chat widget into the same inbox ([#124](https://github.com/colliedesk/colliedesk/issues/124)).
- A sandbox workspace for testing an AI agent safely ([#116](https://github.com/colliedesk/colliedesk/issues/116)).
- No Colliedesk branding on anything your customers see ([#125](https://github.com/colliedesk/colliedesk/issues/125)).

How items get here: ideas start in [Discussions → Ideas](https://github.com/colliedesk/colliedesk/discussions/categories/ideas),
where upvotes show how many teams need them. An idea we agree with becomes an issue labelled
`accepted`; when we commit to it, it is labelled `planned` and listed under Next or Then. A CI check
keeps this page in step with those labels.

Have a need that is not here? Share it in [Ideas](https://github.com/colliedesk/colliedesk/discussions/categories/ideas).
