# Freshdesk import: decisions and traps

Code: `server/internal/freshdesk`. CLI: `server/cmd/collie/import.go` and `undo.go`. Written
from the public Freshdesk API v2 documentation (developers.freshdesk.com/api) only.

## Traps in the Freshdesk API

| Trap | What we do |
|---|---|
| List All Tickets returns only tickets created in the last 30 days unless `updated_since` is set | Always send `updated_since` (default 1970-01-01T00:00:00Z) with `order_by=updated_at&order_type=asc` |
| The list stops after 300 pages (30,000 tickets) | When the cap is hit, start a new window at the last `updated_at` seen and skip ids already seen. If a whole window shares one `updated_at`, the run stops with an error instead of skipping tickets |
| Archived tickets (closed and inactive for 120 days) are not in the list, have no list endpoint, and can have ids above the newest listed ticket | See "Completeness" below |
| Trash and spam are excluded from the list | Not imported. Reported by id |
| Filter (search) endpoints exclude archived tickets and deleted contacts, and return `total` | Used as an independent cross-check, not as a source of items |
| `include=conversations` returns at most 10 conversations | Use `GET /tickets/{id}/conversations` with paging |
| `attachment_url` is a short-lived pre-signed link | Download as soon as the parent is read. A 400/403/410 from the file host counts as expired: re-read the ticket once for a fresh URL and retry |
| Attachment size is `size` in most examples, `file_size` in one | Read both |
| Rate limit: `X-Ratelimit-Remaining`, 429 with `Retry-After` | Remaining `0` pauses the next request one minute. 429 waits `Retry-After` (60 s if absent). 5xx and network errors retry four times with 2–16 s backoff |
| Conversation `source`: 0 reply, 2 note, 8 forwarded email | A forward (`source` 8, not incoming) went to a third party and a public note (`source` 2, not private) was not an email to the customer: both become Colliedesk notes. `origin.kind` keeps the Freshdesk meaning |
| Time entries of archived tickets: no documented endpoint | Warning per ticket; listed under "not imported" |
| View Account (`/api/v2/account`) gives `account_id` | Required. The ledger is keyed by it |

## Completeness (never OK from our own listing)

The report may say OK only when the expected totals come from a source other than the list
endpoints the importer reads:

1. **Tickets: per-id walk.** Ticket ids are sequential. Every id from 1 to the highest known id
   (listed, in the ledger, or `--last-ticket-id`) that the list did not return is looked up:
   `/tickets/archived/{id}`, then `/tickets/{id}`. Each id becomes listed, archived, trashed, spam,
   absent, or failed. Probing continues above that bound until 200 ids in a row are absent. Absent
   ids below the highest existing id are stored as `skipped/absent` with `archive_checked`; trailing
   absent ids are not, because Freshdesk may give them to new tickets. While the archive endpoint is
   refused, no absent verdict is stored (the id may be archived); an absent verdict without
   `archive_checked` is probed again. Four lookups run at a time.
2. **Cross-check:** `search/tickets?query="created_at:>'2000-01-01'"` must not report more
   non-archived tickets than were found (listed + unchanged + trashed + spam); contacts
   `active:true` + `active:false` must not exceed the contact list.
3. A refused archive endpoint (HTTP 403) or an unreadable total makes the report **UNVERIFIED**
   (non-zero exit). Any other probe error fails only that id. The report states the method.
4. While the archive endpoint is refused, ids below the highest existing id that answer 404 are
   `Tickets.UnresolvedIDs` (JSON `unresolved_ids`), not `AbsentIDs`. The CLI prints them after the
   report paths. They stay unresolved on purpose: weakening this would hide a possibly archived ticket.

## Operator feedback

`Options.Progress` (stderr in the CLI) gets `progress: ...` lines: phase starts, counters at most
every `ProgressInterval` (30 s), and each rate-limit wait. Stdout keeps its fixed summary.
`Report.RateLimit` counts `X-Ratelimit-Remaining: 0` pauses, HTTP 429 responses and the seconds
waited (requested durations, not measured time). 5xx backoff is not counted.

## Decisions

- **The ledger is the source of idempotency.** `import_items` maps `(workspace, source, entity,
  Freshdesk id)` to a Colliedesk id; `source` is `freshdesk:account:<account_id>`, so renaming the
  Freshdesk subdomain does not import again. Each row is written in the same transaction as the
  row it describes. A ticket's row is `failed` ("incomplete") from the moment its conversation is
  created until every child succeeded, so an interruption at any point resumes correctly.
- **One run at a time per workspace.** `pg_try_advisory_lock` on the workspace, held for the whole
  import or undo on a dedicated connection. A second import (any source) or an undo fails fast with
  `ErrImportRunning`. Per-workspace, not per-source: imports share content-addressed files, so an
  undo deleting unreferenced files must not overlap another import.
- **Re-run rule.** A ticket is skipped when its ledger row is `done` and the listed `updated_at` is
  not newer. A newer ticket is processed again: new children are added, existing ones are skipped
  by id, and subject, status, priority, assignee, `origin` and `updated_at` are overwritten. Tags
  are only added. `ticketMeta` keeps source counts so a skipped ticket still counts in the report.
- **A partial import is FAILED.** Any failed ledger row, any count where Colliedesk holds fewer
  items than Freshdesk, a byte total that differs, or a stored file whose SHA-256 no longer matches.
- **Stored files are re-verified every run.** A missing or changed file turns its attachment and
  ticket back to `failed`; the next run downloads it into the same attachment row.
- **Downloads are bounded.** `Put` takes the expected size and rejects any other length before the
  rename; a `Content-Length` that disagrees fails before reading. The directory is fsynced after
  the rename. Stale temp files (older than 24 h) are removed at startup. The run stops when less
  than 256 MiB would stay free.
- **Egress.** The API key goes only over HTTPS to the account host; next-page links and API
  redirects off that host are refused. Downloads follow at most five HTTPS redirects. The dialer
  refuses loopback, private, link-local, CGNAT and unspecified addresses, so a URL or redirect
  cannot reach the host's network or a metadata service. Error messages strip query strings.
- **Timeouts.** API calls: 60 s per request. Downloads: 60 s to the response header, then an idle
  deadline of 2 minutes without bytes; large files are not limited by total time.
- **Timestamps.** Freshdesk returns UTC; conversations, messages, contacts and time entries keep
  the Freshdesk times. Message inserts never move the conversation's `updated_at`.
- **Bodies.** `body_text` becomes the Markdown body. The original HTML is stored in
  `messages.body_html` and returned by the API; the UI shows it only in an `iframe sandbox=""` whose
  CSP is `default-src 'none'` (no scripts, no network, so no tracking pixels either).
- **People.** Agents match Colliedesk agents by email in the same workspace; `author_name` is
  always stored. Contacts reuse a contact with the same email in the same workspace. A contact
  without email gets `freshdesk-contact-<id>@import.invalid` (RFC 2606). Companies become
  `contacts.organization`; companies without contacts are reported.
- **Undo** (`collie import undo`) deletes the conversations an import created and the contacts
  and tags it created (`meta.created`) that nothing else uses, then stored files no longer
  referenced. Conversations with a message not written by the import are kept with their ledger
  rows, so a later import does not duplicate them.
- **Canned responses** become `saved_replies` (ledger entity `saved_reply`, source id = canned response
  id). Title is `<folder> / <title>` (the folder name keeps the origin visible; there is no folder
  column). Text is Freshdesk's plain `content`, else `content_html` through `email.HTMLToText`.
  `{{ticket.requester.name}}`, `{{ticket.from_name}}` and `{{ticket.agent.name}}` map to the two
  Colliedesk variables; other placeholders stay and are reported by name. A ledger-`done` reply is never
  rewritten or recreated, so an agent's edit or deletion survives a rerun. Files on a canned response
  are not imported (saved replies carry none) and are listed. Verification compares the sum of folder
  `responses_count` with the responses listed; a refused endpoint is a warning plus `UNVERIFIED`; an
  unreadable folder is a `saved_reply_folder` failure that the next successful read clears. Undo
  deletes imported replies whose `updated_at` equals `created_at` and keeps (with their ledger rows)
  the ones an agent edited.
- **Trial extension.** On Colliedesk Cloud, a real (not dry) run that leaves at least one ticket `done`
  in the ledger moves `workspaces.trial_ends_at` forward by 16 days (14 + 16 = 30 days from the trial
  start) and sets `trial_extended_at`, once. The update is one statement guarded by
  `trial_extended_at IS NULL AND trial_ends_at > now`, so a repeat or resumed run, or a run after the
  trial ended, changes nothing and an import never unlocks a workspace. Undo does not revoke it. The
  report prints `trial: extended to <UTC>` on Cloud only (`Options.Cloud`, set when billing is configured);
  self-hosted installs write the column and say nothing. If the extension fails, the report of the
  finished import is kept, says `trial: extension failed`, and the same import run again retries it.
- **Downloads are never rendered inline.** `GET /api/v1/attachments/{id}` answers
  `application/octet-stream`, `Content-Disposition: attachment` (RFC 5987 `filename*`) and
  `nosniff`, and refuses a row whose storage key belongs to another workspace.

## Not verified against a live account

The test suite uses a fake server built from the documentation. Before the first customer
migration, run a dry run and a full import against a real Freshdesk trial account and check:
that ticket ids are sequential from 1, the archive endpoint's status on plans without archive,
the expiry status of attachment URLs, `size` versus downloaded bytes, the filter query syntax and
totals (including whether spam is counted), rate-limit headers on the plan, conversation `source`
values for forwards and public notes, and the agent-as-requester case.
