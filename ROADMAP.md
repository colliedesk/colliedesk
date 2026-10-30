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
- **New conversation from the inbox**: email a contact first, and see a contact's history with
  merged duplicates.
- **Email channel**: inbound and outbound, threading, spoof and loop protection.
- **Freshdesk importer**: tickets (including archived), replies, notes, attachments
  (checksummed), tags, contacts, companies, time entries and canned responses, with a reconcile
  report. In the app or from the command line; in the app, recent tickets come first and the
  Freshdesk agents can be invited and linked.
- **Help Scout importer**: conversations of one inbox, replies, notes, attachments (checksummed),
  inline images, tags and customers, with a reconcile report and undo.
- **Knowledge base and public help center**: Markdown articles in collections with history and a
  trash, a media library with images and videos, a public help center without scripts,
  your Freshdesk solution articles imported, and article links in replies.
- Team settings: invite, deactivate and reactivate agents.
- **MCP server** so your AI agent (Claude, Codex, Cursor) can work the queue: search, read and
  triage conversations and add private notes, with read, draft-only, send and admin agent tokens; an
  admin token runs workspace settings, the team and channels, and risky actions wait for a person.
- Workspace isolation, per-workspace timezone with per-agent override, full workspace export.
- **One-command install** and a signed container image.
- **Tested upgrades**: every release is upgraded from the previous one on real-shaped data before it
  ships, with an upgrade guide, rollback from a backup and a one-workspace restore.
- **Colliedesk Cloud**: 14-day free trial, then Team or Founding; a workspace whose trial ended is
  locked and can still export everything.
- **AI included**: reply drafts and triage suggestions from your own knowledge base and solved
  tickets, with their sources shown; your own provider key or the server's; optionally a draft for
  every customer message and suggestions that apply themselves (with Undo). AI never sends a reply
  in a conversation.
- **Help chat**: an AI on your help center (its chat page, every article, and your own site) that
  answers customers only from your published articles, shows its sources, and hands the chat to
  your team as a conversation; with its own monthly limit and every transcript kept for the team.
- **Results**: your numbers in Colliedesk next to the history you imported from Freshdesk (first
  response, time to resolve, reopens, volume, AI drafts), each opening the conversations it counts,
  with a share link and a weekly email to admins.

## Next: beta (0.1.0)

Beta starts when HC Labs announces it. Before that:

- Connect a Gmail / Google Workspace or Microsoft 365 mailbox with one click (OAuth); replies
  leave from your own address. Forwarding keeps working ([#95](https://github.com/colliedesk/colliedesk/issues/95)).
- A first team that moved from Freshdesk and works in Colliedesk every day.

## Then: AI included

- A backtest before you pay: how drafts would have answered your own past tickets ([#99](https://github.com/colliedesk/colliedesk/issues/99)).
- Auto-answer only for topics where drafts proved accurate, with a human in the loop.
- Usage metering; bring your own AI key.

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
- Importer for Zendesk ([#122](https://github.com/colliedesk/colliedesk/issues/122)).
- Live chat widget into the same inbox ([#124](https://github.com/colliedesk/colliedesk/issues/124)).
- A sandbox workspace for testing an AI agent safely ([#116](https://github.com/colliedesk/colliedesk/issues/116)).
- No Colliedesk branding on anything your customers see ([#125](https://github.com/colliedesk/colliedesk/issues/125)).
- Help center articles end with a helpful vote, a still-need-help step and related articles ([#338](https://github.com/colliedesk/colliedesk/issues/338)).
- A request form and "My requests" on the help center, like a Freshdesk portal ([#290](https://github.com/colliedesk/colliedesk/issues/290)).
- Articles for agents only or for signed-in customers ([#343](https://github.com/colliedesk/colliedesk/issues/343)).
- Suggested articles while a customer writes a request, with the requests avoided counted ([#344](https://github.com/colliedesk/colliedesk/issues/344)).
- A help center search report: top searches and searches with no results ([#345](https://github.com/colliedesk/colliedesk/issues/345)).
- Knowledge gaps: what customers could not find, and a draft article from your solved conversations ([#346](https://github.com/colliedesk/colliedesk/issues/346)).
- Help center numbers in Results: self-service rate, article views and the articles that lead to requests ([#347](https://github.com/colliedesk/colliedesk/issues/347)).

How items get here: ideas start in [Discussions → Ideas](https://github.com/colliedesk/colliedesk/discussions/categories/ideas),
where upvotes show how many teams need them. An idea we agree with becomes an issue labelled
`accepted`; when we commit to it, it is labelled `planned` and listed under Next or Then. A CI check
keeps this page in step with those labels.

Have a need that is not here? Share it in [Ideas](https://github.com/colliedesk/colliedesk/discussions/categories/ideas).
