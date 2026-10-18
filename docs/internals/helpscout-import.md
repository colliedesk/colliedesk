# Help Scout import: decisions and traps

Code: `server/internal/helpscout`. CLI: `server/cmd/collie/import_helpscout.go`; undo is the shared
`collie import undo` (`freshdesk.Undo`). Written from the public Help Scout Inbox API 2.0
documentation (developer.helpscout.com/mailbox-api) only. Not yet run against a live account.

## Same contract as the Freshdesk importer

- One ledger table, `import_items`, keyed by source `helpscout:mailbox:<inbox id>`. Inbox ids are
  never reused, so renaming an inbox never imports it twice.
- Ledger entities `ticket` (a Help Scout conversation), `contact`, `tag`, `attachment` and
  `inline_image` mean what they mean for Freshdesk. A thread that became a message is `thread`.
  `freshdesk.Undo` knows `thread` and accepts `helpscout:` sources, so one undo serves both.
- Statuses `OK`, `FAILED`, `UNVERIFIED`, `DRY_RUN`; a partial import is `FAILED`.
- Shared through `server/internal/importkit`: the dial guard (public addresses only), the
  workspace import lock (one import or undo per workspace, across importers), the trial extension,
  the storage cap error, and the `<img>` rewriting and image type checks.

## Traps in the Help Scout API

| Trap | What we do |
|---|---|
| OAuth2 client credentials; the token lives 2 days and expiry shows only as HTTP 401 | Request a token on the first call. On 401, request a new one once and repeat the call; a second 401 is an error. Counted in `rate_limit.token_refreshes` |
| Rate limit per account, by plan; 429 with `X-RateLimit-Retry-After` (seconds), and `X-RateLimit-Remaining-Minute` | 429 waits `X-RateLimit-Retry-After` (then `Retry-After`, then 60 s). Remaining `0` pauses the next request for `X-RateLimit-Retry-After`. 5xx and network errors retry four times with 2–16 s backoff |
| HAL pages: items in `_embedded.<name>`, `page.number` from 1, `page.totalPages`; conversations 25 per page, others 50 | `listAll` asks `page=1..totalPages` and stops early on an empty page |
| List Conversations returns only active ones by default | Always `status=all`, `sortField=createdAt`, `sortOrder=asc` (new conversations land on the last page, earlier pages do not shift). Spam is listed separately with `status=spam` and reported, not imported |
| Conversation `state` can be `deleted` or `draft` | Not imported; listed by id |
| Thread `type` `lineitem` is a status or assignee change with no body | Not a message; counted in `messages.line_items_not_imported` |
| Thread `state` `draft` was never sent | Not imported; listed by id |
| Attachment bytes come as base64 JSON from `/v2/conversations/{id}/attachments/{id}/data`, from the API host | Decoded and compared with `size` (when present) before storing. A mismatch fails the attachment |
| Attachment `state` `virus` | Not downloaded; listed by id |
| `primaryCustomer.email` can be empty (phone, chat) | Read `/v2/customers/{id}` and use the first `_embedded.emails[].value`; otherwise a `.invalid` address with a warning |
| `assignee.type` can be `team` | Imported unassigned with warning `assigned_to_team` |
| `userUpdatedAt` may not move on a customer reply | A finished conversation is read again when `userUpdatedAt`/`closedAt` moves **or** its `threads` count changes |

## Mapping

- Status: `active` → open, `pending` → pending, `closed` → closed; anything else pending with a warning.
  Priority: always normal (Help Scout has none).
- Threads: `customer` → incoming; `message` → outgoing; `note` → note (`private_note`);
  `forwardparent`/`forwardchild` → note (`forward`); `phone` → note (`other`); `chat`/`beaconchat` →
  incoming when `createdBy.type` is `customer`, else outgoing. Only an agent reply is ever outgoing.
- `origin.external_id` of a conversation is its `number` (what agents see); the ledger uses `id`.
- The body is the thread HTML reduced to text (`email.HTMLToText`); the HTML is kept in `body_html`.

## Completeness

The Help Scout API has no total independent of the list endpoint we read (folder `totalCount`
semantics are undocumented, and folders overlap). Two checks instead:

1. The number of conversations read equals `page.totalElements` of the last page.
2. For each conversation, the threads endpoint returned at least `threads` (the count on the
   conversation). Fewer makes the run `UNVERIFIED` and names the conversation. Stored in the
   ledger meta (`threads`, `threads_read`), so a rerun that skips the conversation still reports it.

Whether `threads` counts line items and drafts is not documented; the fixtures assume it counts
every thread. Check this on the first live import.

## Images inside threads

`<img src>` on `helpscout.net` or a subdomain is fetched without the token through the guarded
client (redirect hops must stay on those hosts), type-checked (PNG, JPEG, GIF, WebP), stored as an
attachment row and embedded as a `data:` URI, 5 MiB per image and per message, as for Freshdesk.
404/410 is `skipped` (gone), anything else `failed`. Unverified against a live account: which host
Help Scout uses for pasted images and whether it needs a session. If it does, every such image fails
and the report says `FAILED`, which is the safe direction.
