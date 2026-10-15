# Colliedesk feature spec

Status: v0.1 scope defined 2026-10-14. Owner: HC Labs.

## Product

Open-source helpdesk (AGPL-3.0) for small support teams (1–10 agents: MSPs, IT teams, customer
support) leaving Freshdesk. Hosted cloud is the paid product; self-hosting is free.

Positioning: "Leave Freshdesk with all your data. AI that actually answers tickets is included,
not an add-on. Flat price, no per-agent fees."

## Market

International from day one. Target customers are in the US, EU, UK and Australia, not Vietnam.
- English is the default and only UI language in v0.1, but every UI string goes through one
  messages module so more locales can be added without touching components.
- Each workspace has an IANA timezone (default `UTC`) and each agent may override it. The server
  stores and returns UTC; the UI formats dates with `Intl` in the agent's effective timezone.
- No country-specific defaults (currency, phone format, timezone, holidays) in code.
- Data residency: hosted regions will be US and EU; nothing in the design may assume one region.

## Clean-room rule

Feature ideas may come from public product descriptions of other helpdesks (Freshdesk, Zendesk,
Help Scout). Code must not. Nobody working on this repository may open, copy, translate or
paraphrase the source code of another helpdesk, especially a copyleft-licensed one. Everything here is written from this spec.

## Authentication (modelled on T3 Code's environment auth)

Reference: T3 Code (MIT, github.com/pingdotgg/t3code), `docs/internals/environment-auth.md` and
`docs/user/remote-access.md`. T3 Code is MIT-licensed, so studying it is fine; any code copied from
it must keep its MIT notice in `THIRD-PARTY-NOTICES.md`.

1. **No passwords.** Nobody sets or types a password in Colliedesk.
2. **Bootstrap like `t3 pair`.** `colliedesk create-workspace --name <n> --admin-email <e>` prints a
   one-time pairing URL. `colliedesk pair --workspace <slug> --email <e>` prints a fresh one for an
   existing agent (new device or lost session). Links are single use, expire after 15 minutes and
   are stored only as a SHA-256 hash; the raw link is shown once, at creation.
3. **Invites carry a role ceiling.** An admin creates an invite link for `agent` or `admin` in the UI.
   Redeeming a link can narrow but never widen what it grants. Creating a link requires the
   `access:write` permission and every permission being delegated. The invite list shows metadata
   (who, role, expiry, used or not), never the secret.
4. **Sessions.** Redeeming a link creates a session: an HttpOnly, Secure, SameSite=Lax cookie for
   browsers, or a bearer token for API and CLI clients. Sessions are stored hashed with a label
   (browser/OS), created and last-seen times, 30-day sliding expiry, and can be revoked. A
   **Sessions** settings page lists the agent's own sessions (admins: the whole workspace) with
   Revoke. A rejected credential never falls back to another credential. An **agent token** is a
   session for an AI agent with a scope (`read`, `draft` or `send`): bearer only (`cdk_agent_` prefix),
   agent role, tier enforced on every route, 30-day sliding expiry and a one-year limit from creation
   (`docs/internals/auth.md`).
5. **Every route declares its permission.** Permissions: `conversations:read`, `conversations:write`,
   `contacts:read`, `contacts:write`, `access:read`, `access:write`, `workspace:export` (admins only: the bulk export of the whole workspace); `channels:inbound` marks channel routes authenticated by a channel secret instead of a session. Roles map to permission sets. A test
   enumerates every registered API route and fails if one has no declared permission.
6. **Later.** Email magic link (v0.2, with the email channel). **Cloud sign-in through Clerk** (v0.3):
   Clerk authenticates the account (Google, Microsoft, email, MFA; SAML when an enterprise needs it),
   then the Colliedesk server mints a one-time link that is redeemed for an ordinary Colliedesk session.
   Clerk never holds a Colliedesk session; self-hosted installs do not use Clerk at all; MCP clients sign in through OAuth authorization code + PKCE and receive either a
   read-only grant or an operate grant (v0.4+).

## Engineering principles (learned from T3 Code)

- **One contract.** Everything that crosses the wire is defined once in `contracts/openapi.yaml`.
  Server tests validate responses against it; web types are generated from it
  (`openapi-typescript`). A CI step fails when generated types are stale.
- **Reverse states.** Every way in has a way out and a way to see it: close/reopen, assign/unassign,
  add/remove tag, invite/revoke, sign in/revoke session.
- **Docs record decisions and traps**, not what the code already says (`docs/internals/`).
- **UI:** dense, keyboard-first, desktop-first. Layout: conversation list on the left, timeline in the
  middle, composer at the bottom, details on the right; visible focus; every action reachable by
  keyboard.

## Capability map (target product)

| Area | Capability | Release |
|---|---|---|
| Workspaces | Multi-tenant; every row scoped by `workspace_id`; no cross-workspace reads; admin sets workspace IANA timezone, each agent may set an override (validated IANA names) | v0.1 |
| Agents | Passwordless sign-in by one-time pairing/invite links (see Authentication), roles `admin` / `agent`, session list with revoke | v0.1 |
| Contacts | Customer records (name, email), conversation history per contact | v0.1 |
| Conversations | Subject, status (open, pending, resolved, closed), priority, assignee, tags (agents create, rename and delete tags) | v0.1 |
| Messages | Outgoing reply and private note; Markdown body; ordered timeline. Incoming messages arrive with the email channel | v0.1 (incoming: v0.2) |
| Inbox views | Filter by status, assignee (me / unassigned / anyone), tag; text search on subject and body | v0.1 |
| Email channel | Inbound via provider webhook, outbound SMTP/API, Message-ID threading, spoof and loop protection | v0.2 |
| Freshdesk import | Tickets, replies, notes, attachments (checksummed), tags, contacts, reconcile report | v0.2 |
| AI included | Draft reply and triage from past replies and knowledge base; auto-answer repetitive tickets; usage metering | v0.3 |
| Knowledge base | Articles, collections, public help center | v0.3 |
| Automation | Rules on conversation events (assign, tag, set status), macros (saved replies + actions) | v0.4 |
| SLA | First-response and resolution targets, business hours, breach alerts | v0.4 |
| Time tracking | Time entries per conversation, CSV export (MSP need) | v0.4 |
| CSAT and reports | Rating after resolve; response/resolution times, volume per agent | v0.5 |
| Live chat widget | Embeddable website chat into the same inbox | later |
| MCP + Agent Skills | Agents (Claude, Codex) work the queue through MCP: `/mcp` serves read and triage tools today (`docs/user/mcp.md`); reply drafts and skills come later | in progress |
| Billing | 14-day free trial, then a Polar subscription; an expired workspace is locked (sign in, pay, export only) | cloud only |

## v0.1 acceptance

1. An admin creates a workspace and its first admin agent from the CLI.
2. An agent logs in, sees the inbox, filters it, opens a conversation, reads the timeline,
   replies, adds a private note, changes status, assignee and tags.
3. An agent of workspace A can never read or change data of workspace B (tested at the API).
4. `make test` runs Go tests against a real PostgreSQL and web unit tests; `make build`
   produces the server binary and the web bundle.
