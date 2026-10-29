# Changelog

Colliedesk is alpha: releases are `0.0.x`, and any release may change the interface or the
database schema. Every release ships the migrations to upgrade from the previous one. Back up
before upgrading ([data to back up](docs/self-host/database.md#data-to-back-up)).

From 0.0.2, the notes of each release are on the
[releases page](https://github.com/colliedesk/colliedesk/releases), generated from the merged pull
requests. This file keeps the first release.

## Unreleased

**Operators: the reverse proxy must send the visitor's `Host` header.** The server now answers the app,
the API, MCP and attachments only when the `Host` of the request is `APP_ORIGIN`'s host, `localhost` or
an IP address (health checks, the proxy's certificate question). Every other host name gets the help
center, or the help center's 404. Caddy keeps the `Host` header by default. With nginx, set
`proxy_set_header Host $host;`; a proxy that rewrites it to an internal name such as `colliedesk:8080`
now gets 404 for everything. See [running behind a proxy](docs/self-host/running-behind-proxy.md#the-host-header).

**The workspace app can no longer be framed by another site.** Its pages send
`Content-Security-Policy: frame-ancestors 'self'`, `X-Frame-Options: SAMEORIGIN`, `X-Content-Type-Options: nosniff`
and `Referrer-Policy: strict-origin-when-cross-origin`, so no other origin can load a signed-in agent's inbox in a
hidden frame. Help centers and the chat widget keep their own framing rules.

API changes in the email view (the full notes will be on the releases page):

- `GET /contacts/{id}` no longer embeds `conversations` (an unpaged list). It returns
  `conversation_count`, `merged_into` and `merges`; read the history with `GET /conversations?contact={id}`.
  `Contact` has a new field, `merged_into_id`. Contact searches leave merged contacts out.
- `POST /conversations` takes an optional `body` (and `cc`, or `multipart/form-data` with files) to start
  the conversation by emailing the contact.

- `MessageOrigin.kind` in message responses has a new value, `automated_response`: an automatic reply that
  Freshdesk flagged. A client that rejects unknown enum values must accept it. Tickets imported before this
  change keep `incoming`; import them again to get the new value.
- `Attachment` has a new required boolean, `inline`: true when the file's bytes are embedded in the
  message's `body_html`. Existing attachments are `false`.
- For a signed-in person, `body_html` in `GET /conversations/{id}/messages` has its remote images rewritten
  to the new `GET /api/v1/mail-image` route; an agent token still reads the HTML as stored.
- `EmailChannel` has a new required field, `sender_name`: the name customers see in the From and
  Reply-To lines of every reply. `PUT /channels/email` takes it as an optional field (1 to 100
  characters, no control characters). The upgrade sets each existing channel to its workspace name, so
  mail does not change until an admin edits it; replies no longer use the workspace name.
- New route `PATCH /workspace/name` (admin) renames the workspace: 1 to 100 characters, no control
  characters. It does not change any email channel's `sender_name`.
- New operator command `collie email sender-name [--by <name>] <workspace slug> <name>` sets a sender
  name that the brand check of Colliedesk Cloud refuses through the API, for the brand's owner. It
  checks the name as the API does and skips only the brand check.
- New route `PUT /kb/order` (`send` tier; MCP `colliedesk_kb_order_set`) sets the order of the collections
  and articles directly under one parent; a list that is not exactly the parent's live children is 409
  `order_stale` with the current lists. Article answers have a new required field, `position`.
  `GET /kb/articles` takes `sort` (`recent` or `manual`); with `collection_id` and no `q` it now lists the
  collection's articles in their order (by title until ordered by hand) instead of most recently updated first.
- The help center can serve on a custom domain: `GET`, `PUT`, `DELETE /help-center/domain` and `POST
  /help-center/domain/check` (MCP `colliedesk_help_center_domain_get`, `_set`, `_check`, `_remove`).
  While it is active, `HelpCenter.url` and every article `url` name it, the slug addresses answer 301 to it,
  and `tls-ask` answers for it. The export's `help-center.ndjson` has `custom_domain`, `custom_domain_status`
  and `custom_domain_verified_at`. The `CNAME` target is the claim's own `hc-<claim>.<HELP_CENTER_DOMAIN>`;
  slugs of exactly that shape are reserved. An active domain that no check proves for 7 days goes back to
  `pending`, and another workspace that proves the host name while its records do not takes it over
  (`HelpCenterDomain.failing_since`, `pending_reason`). Checks are limited to 10 an hour per workspace.
- The help center theme takes `custom_css` (`PUT /help-center/theme`, `/check`, `/preview`; MCP
  `colliedesk_help_center_theme_update` and `_check`), served after the brand on every public page;
  CSS that could load anything is 422 `custom_css_refused` with its line and column. `HelpCenterTheme`
  answers have a new required field, `custom_css`, and the export's `help-center.ndjson` carries it.
  `PUT /help-center/theme` requires `custom_css` (null or `""` clears it): a save without it is 422
  `validation_failed` and an agent request without it is not filed, so an older client cannot clear it.
  The brand sheet's name is now `brand-<32 hex digits>.css`.
  Under a parent ordered by hand, `POST /kb/collections` without `position` or with 0 puts the collection
  last (an explicit 0 used to be kept); a created, moved, imported or restored article or collection goes last too.

## 0.0.1

First tagged release. Everything built so far:

- Passwordless sign-in with one-time pairing and invite links, revocable sessions, and cloud
  sign-in through Clerk on Colliedesk Cloud.
- Shared inbox: replies, private notes, status, priority, assignee, tags, search, bulk actions,
  saved replies, attachments (10 MiB per file, 10 files per message), live updates and browser
  notifications.
- Email channel: inbound and outbound mail, threading, spoof and loop protection.
- Freshdesk importer: tickets including archived ones, replies, notes, checksummed attachments,
  tags, contacts, companies, time entries and canned responses, with a reconcile report and undo.
- Team settings: invite, deactivate and reactivate agents.
- Workspace isolation, per-workspace timezone with per-agent override, full workspace export.
- One-command install with a signed container image. The installer now defaults to `latest`
  (the newest release) instead of `edge`.
- Colliedesk Cloud: 14-day free trial, Team and Founding plans; a workspace whose trial ended is
  locked and can still export everything.
