# Roadmap

Colliedesk is an open-source help desk for teams moving off Freshdesk. We are a small team, we
merge into `main` nearly every day, and we build in the open: every item below is either live
today or backed by an open issue you can follow.

`shipped` is live on Colliedesk Cloud. `planned` is committed and next in line. `later` is
accepted and waiting for its turn.

We are in alpha (`0.0.x`): any release may change the interface or the database schema. Beta
(`0.1.0`) starts when a first team that moved from Freshdesk works in Colliedesk every day.
Releases are listed in the [changelog](CHANGELOG.md); feature details live in
[docs/feature-spec.md](docs/feature-spec.md).

## Leave Freshdesk without losing anything

- **Freshdesk importer**: tickets (archived ones too), replies, notes, checksummed attachments, contacts, companies, time entries and canned responses, with a reconcile report; recent tickets come first and your Freshdesk agents can be invited. `shipped`
- **Help Scout importer**: one inbox's conversations, replies, notes, inline images and customers, with a reconcile report and undo. `shipped`
- **Your numbers next to your Freshdesk history**: first response, time to resolve, reopens and volume side by side, each opening the conversations it counts, with a share link and a weekly email to admins. `shipped`
- **Connect Gmail or Microsoft 365 in one click**: replies leave from your own address, and forwarding keeps working ([#95](https://github.com/colliedesk/colliedesk/issues/95)). `planned`
- **Stay in sync with Freshdesk until you switch**: no freeze day, your team moves when it is ready ([#98](https://github.com/colliedesk/colliedesk/issues/98)). `later`
- **Zendesk importer**: the same move, from Zendesk ([#122](https://github.com/colliedesk/colliedesk/issues/122)). `later`

## AI that drafts, and a person who sends

- **Reply drafts from your own answers**: drafts and triage suggestions from your knowledge base and solved tickets, with their sources shown; optionally a draft for every customer message. AI never sends a reply in a conversation. `shipped`
- **Help chat on your help center**: answers customers only from your published articles, shows its sources, and hands the chat to your team as a conversation, with every transcript kept. `shipped`
- **Bring your own AI key**: use your provider's key, or the AI included with Cloud. `shipped`
- **A backtest before you pay**: see how drafts would have answered your own past tickets ([#99](https://github.com/colliedesk/colliedesk/issues/99)). `planned`
- **Auto-answer where drafts proved right**: only for topics where drafts were accurate, with a person in the loop ([#281](https://github.com/colliedesk/colliedesk/issues/281)). `later`
- **AI usage you can see on Cloud**: drafts and triage within a monthly allowance, auto-answer priced per resolution ([#262](https://github.com/colliedesk/colliedesk/issues/262)). `later`
- **AI drafts that look up customer data**: read-only, through your own MCP servers ([#104](https://github.com/colliedesk/colliedesk/issues/104)). `later`

## Your AI agent works the queue

- **MCP server**: Claude, Codex or Cursor search, read and triage conversations and add private notes, with read, draft-only, send and admin tokens; risky actions wait for a person. `shipped`
- **A sandbox workspace**: try an AI agent on real-shaped data before it touches your queue ([#116](https://github.com/colliedesk/colliedesk/issues/116)). `later`

## Everything a support team expects

- **Shared inbox**: replies, private notes, status, priority, assignee, tags, search, bulk actions, saved replies, attachments, Cc, live updates and browser notifications. `shipped`
- **Email channel**: inbound and outbound, threading, spoof and loop protection; start a conversation with a contact and see their history. `shipped`
- **Knowledge base and public help center**: Markdown articles in collections with history, a media library, a help center without scripts, and your Freshdesk articles imported. `shipped`
- **Your team, signed in without passwords**: one-time invite links, role ceilings, revocable sessions, per-agent timezone. `shipped`
- **Simple rules on arrival**: assign, tag and set priority as conversations come in ([#103](https://github.com/colliedesk/colliedesk/issues/103)). `later`
- **Macros**: a saved reply plus its actions in one step ([#117](https://github.com/colliedesk/colliedesk/issues/117)). `later`
- **Slack notifications** ([#102](https://github.com/colliedesk/colliedesk/issues/102)). `later`
- **Reports**: volume, first response and resolution time per agent and tag ([#121](https://github.com/colliedesk/colliedesk/issues/121)). `later`
- **Customer satisfaction rating** after a conversation is resolved ([#120](https://github.com/colliedesk/colliedesk/issues/120)). `later`
- **Time logged on conversations**, exported per company ([#119](https://github.com/colliedesk/colliedesk/issues/119)). `later`
- **Live chat widget** into the same inbox ([#124](https://github.com/colliedesk/colliedesk/issues/124)). `later`
- **Help center articles that help further**: a helpful vote, a still-need-help step and related articles ([#338](https://github.com/colliedesk/colliedesk/issues/338)). `later`
- **No Colliedesk branding** on anything your customers see ([#125](https://github.com/colliedesk/colliedesk/issues/125)). `later`

## Yours to keep

- **Open source, one-command install**: run it on your own server from a signed container image. `shipped`
- **Tested upgrades**: every release is upgraded from the previous one on real-shaped data before it ships, with an upgrade guide, rollback from a backup and a one-workspace restore. `shipped`
- **Your data leaves with you**: full workspace export, also after a Cloud trial ends. `shipped`
- **Workspaces kept apart**: every query is scoped to its workspace. `shipped`

## Tell us what you need

Ideas start in [Discussions → Ideas](https://github.com/colliedesk/colliedesk/discussions/categories/ideas),
where upvotes show how many teams need them. An idea we agree with becomes an issue labelled
`accepted` and shows here as `later`; when we commit to it, it is labelled `planned`. A CI check
keeps every status on this page in step with those labels.
