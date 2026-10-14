# Freshdesk import: decisions and traps

Code: `server/internal/freshdesk`. CLI: `server/cmd/colliedesk/import.go`. Written from the public
Freshdesk API v2 documentation (developers.freshdesk.com/api) only.

## Traps in the Freshdesk API

| Trap | What we do |
|---|---|
| List All Tickets returns only tickets created in the last 30 days unless `updated_since` is set | Always send `updated_since` (default 1970-01-01T00:00:00Z) with `order_by=updated_at&order_type=asc` |
| The list stops after 300 pages (30,000 tickets) | When the cap is hit, start a new window at the last `updated_at` seen and skip ids already seen. If a whole window shares one `updated_at`, the run stops with an error instead of skipping tickets |
| Archived tickets (closed and inactive for 120 days) are not in the list and cannot be enumerated | Ticket ids are sequential, so every id between 1 and the highest listed id that the list did not return is probed: `GET /tickets/archived/{id}`, then `GET /tickets/{id}`. Found tickets are imported; trashed, spam and absent ids are recorded. Cost: one or two requests per gap. Skipped when `--since` is used |
| Trash and spam are excluded from the list | Not imported. Reported by id |
| `include=conversations` returns at most 10 conversations | Use `GET /tickets/{id}/conversations` with paging |
| `attachment_url` is a short-lived pre-signed link | Download as soon as the parent is read. A 400/403/410 from the file host counts as expired: re-read the ticket once for a fresh URL and retry |
| Attachment size is `size` in most examples, `file_size` in one | Read both |
| Rate limit: `X-Ratelimit-Remaining`, 429 with `Retry-After` | Remaining `0` pauses the next request one minute. 429 waits `Retry-After` (60 s if absent). 5xx and network errors retry four times with 2–16 s backoff |
| Time entries of archived tickets: no documented endpoint | Warning per ticket; listed under "not imported" |

## Decisions

- **The ledger is the source of idempotency.** `import_items` maps `(workspace, source, entity,
  Freshdesk id)` to a CollieDesk id. `source` is `freshdesk:<host>`, so two accounts never collide.
  Each row is written in the same transaction as the row it describes. A ticket's ledger row is
  written as `failed` ("incomplete") when its conversation is created and set to `done` only after
  every child item succeeded, so an interruption at any point resumes correctly.
- **Re-run rule.** A ticket is skipped when its ledger row is `done` and the listed `updated_at`
  is not newer. A newer ticket is processed again: new children are added, existing ones are
  skipped by id, and subject, status, priority, assignee and `updated_at` are overwritten. Tags are
  only added. `ticketMeta` keeps the source counts of a ticket so a skipped ticket still counts in
  the report.
- **A partial import is FAILED.** Any failed ledger row, any count where CollieDesk holds fewer
  items than Freshdesk, a byte total that differs, or a stored file whose SHA-256 no longer matches
  makes the run FAILED. Per-item errors never stop the run; only cancellation, a rejected key, an
  unreachable account or a database error do.
- **Size check is strict.** A downloaded file whose size differs from Freshdesk's reported size is
  rejected and retried on the next run. If a real account shows systematic differences, change the
  rule with evidence; do not loosen it blindly.
- **The API key never leaves the account host.** Credentials go only to the `--domain` host. Go's
  HTTP client drops `Authorization` on a redirect to another host. Error messages strip query
  strings, which hold pre-signed signatures.
- **Timestamps.** Freshdesk returns UTC. Conversations, messages, contacts and time entries keep
  the Freshdesk times. The importer writes messages with its own SQL so a message insert never
  moves the conversation's `updated_at`.
- **Bodies.** `body_text` becomes the Markdown body (plain text renders as text). The original
  HTML is kept in `messages.body_html` (not in the API yet) for a later export or image rehosting.
  An empty text body gets a visible placeholder because a message body cannot be empty.
- **People.** Agents match CollieDesk agents by email; every imported message also stores
  `author_name`. Contacts reuse an existing contact with the same email. A contact without email
  gets `freshdesk-contact-<id>@import.invalid` (`.invalid` is reserved by RFC 2606). A requester
  missing from the contact list is fetched by id; if Freshdesk no longer has it, a contact is made
  from the agent record or the id. A company becomes `contacts.organization`; companies without
  contacts are reported.
- **Ticket source 10 (Outbound Email)**: the description is an outgoing message by the responder.
- **Attachment storage** is content-addressed (`<workspace>/<sha[0:2]>/<sha>`), so a retried
  download rewrites the same file instead of leaving an orphan. The bytes are written to a temp
  file, synced and renamed; a failed stream leaves nothing.
- **Downloads are never rendered inline.** `GET /api/v1/attachments/{id}` always answers
  `application/octet-stream` with `Content-Disposition: attachment` and `nosniff`, because files
  from customers must not run as HTML or SVG on the app origin.

## Not verified against a live account

The test suite uses a fake server built from the documentation. Before selling a migration, run a
dry run and a full import against a real Freshdesk trial account and check: archived ticket probing
(ids sequential from 1), the expiry status code of attachment URLs, `size` versus downloaded bytes,
rate-limit headers on your plan, and the agent-as-requester case.
