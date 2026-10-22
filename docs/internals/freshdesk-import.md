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
- **Merged contacts survive undo.** A contact the import created is deleted by undo only while it has
  no conversations and is not merged into another contact. A merged one has no conversations of its own,
  but deleting it would drop the merge record and let mail from its address recreate the duplicate.
  The import maps a Freshdesk contact whose address is merged to the kept contact.
- **Re-run rule.** A ticket is skipped when its ledger row is `done` and the listed `updated_at` is
  not newer. A newer ticket is processed again: new children are added, existing ones are skipped
  by id, and subject, status, priority, assignee, `origin` and `updated_at` are overwritten. Tags
  are only added. `ticketMeta` keeps source counts so a skipped ticket still counts in the report.
- **A partial import is FAILED.** Any failed ledger row, any count where Colliedesk holds fewer
  items than Freshdesk, a byte total that differs, or a stored file (attachment or inline image) whose SHA-256 no longer matches, or an inline image that could not be brought over.
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
- **Inline images** (ledger entity `inline_image`). An `<img src>` in a message HTML that Freshdesk
  hosts is downloaded, stored through `attachments.Storage` (SHA-256, content-addressed) as an
  `attachments` row on the message, and the stored `body_html` gets a `data:` URI in its place.
  Why `data:`: the locked frame is `sandbox=""` with `img-src data:` and no network, so a URL to our
  API would not load there (no origin, no session cookie), and relaxing the CSP would let every
  stored original load remote content. It is the email channel's representation (`email.inlineImages`,
  `email.md`). The attachment row is the stored copy: it gives the file a SHA-256, re-verification,
  the storage cap and undo for free, and shows in the message's file list. It is not counted in
  `Attachments`, which compare with Freshdesk's own attachment totals.
  - *Hosts.* Exact patterns only (`inline-image-hosts.go`): the importing account's host
    (case-insensitive), the attachment gateways `attachment.freshdesk.com`, `euc-attachment.freshdesk.com`
    and `indattachment.freshdesk.com`, and Freshdesk's public buckets `cdn.freshdesk.com` and
    `assets.freshdesk.com` (path style `s3[.-]<region>.amazonaws.com/<bucket>/...` or virtual host
    `<bucket>.s3[.-]<region>.amazonaws.com`). Seen on hclabs.freshdesk.com (18/10): the gateway
    answers 302 to `<tenant>.freshdesk.com`, which answers 302 to `<tenant>.attachments<N>.freshdesk.com`
    (image/png or image/jpeg); old content sits in `s3.amazonaws.com/assets.freshdesk.com/...`. Those
    redirect hops are allowed by a second predicate (`allowedRedirectURL`: any tenant or tenant CDN host)
    that applies to hops only, never to the address written in a message. A tenant portal of another Freshdesk account, a
    bucket anyone can name, and a custom support domain are not fetched: a URL that names Freshdesk
    but is not fetched raises `inline_images_not_rehosted` for its ticket (no silent skip). `http://`
    links of an allowed host are upgraded to https. The same predicate runs on every redirect hop of
    the download (a context-carried guard on the files client), so a redirect off the list fails with
    "redirect refused". The dialer guard of "Egress" still applies; the API key goes only to the
    account host. Unverified against a live account: whether the account host's
    `/helpdesk/attachments/<id>` accepts the API key and which hosts it redirects to.
  - *Type.* The stored media type never comes from the response header. The bytes are sniffed
    (`http.DetectContentType`) and must be PNG, JPEG, GIF or WebP; the declared type must agree or be
    `application/octet-stream`. SVG and anything else fails. The `data:` URI is built from that fixed
    type and HTML-escaped; header text echoed in an error is reduced to a safe character set.
  - *Id.* `<entity>:<message source id>:<hash of host + path + query without signing parameters>`.
    Pre-signed URLs change on every read, so the signature, expiry and `X-Amz-*` parameters are not
    part of the hash. The same image twice in one message is one item.
  - *Atomic.* All images of one message are stored first, then their attachment rows, ledger rows
    and one `body_html` rewrite commit in one transaction, so a resumed run never double-stores and
    never leaves HTML and ledger disagreeing. Only `<img src>` values (not `data-src`) equal to an
    image are rewritten; text that happens to hold the URL is not.
  - *Limits.* 5 MiB per image, and 5 MiB of embedded `data:` images per message, the email channel's
    budget (`email/files.go`); the bytes already stored for the message count, so a resumed run keeps
    the total. An image over the message budget is still stored and `done` (the bytes are kept) but
    its link stays in the HTML, with warning `inline_image_not_embedded` for the ticket. A too large
    image, a non-image, 404, expired link, refused redirect or 5xx makes that image `failed`.
    `body_html` is returned with every timeline message, so the budget also bounds that payload.
  - *A failed image makes the report FAILED.* The image is lost the day the account closes; the
    report must not say OK. The ticket stays `failed`, the next run reads it again and retries the
    image. The HTML keeps the original link. This covers every transient or fixable cause: 5xx,
    timeout, an expired link (400/403), a refused redirect, a wrong or hostile type, an image over 5 MiB.
  - *An image Freshdesk no longer has is not a failure (owner decision 18/10).* HTTP 404, or 410
    (`errGone`, which also matches `errURLExpired` so attachments still refresh), from an allowed host
    cannot be fixed by a rerun, and failing forever would stop the customer from ever seeing OK. The
    ledger row becomes `skipped` with the redacted error (no target). Reconcile lists it under
    `inline_images.gone_images` and in the Markdown section "Images already gone in Freshdesk (not
    recoverable)" (ticket + message), subtracts it from the expected count (`Recoverable()`), and
    leaves the status alone. The link stays in the HTML. The ticket finishes `done`, so a rerun skips
    it and does not ask again; if the ticket's `updated_at` moves, the ticket is read again and the
    skipped image is retried once (it is not `done`), which recovers a file that came back. Undo drops
    the row with the rest.
  - *Counts.* `ticketMeta.inline_images` is the number of distinct hosted images per ticket
    (`Report.InlineImages.Source`). Freshdesk reports no size, so there is no source byte total;
    `ImportedBytes` and the SHA-256 re-verification (`verifyAttachments`) cover the stored files.
  - *Storage cap.* The sizing pass counts each image not yet `done` by its announced Content-Length
    (it reads the body only when none is announced), so bytes count before any write and images are
    not downloaded twice. An image the sizing cannot read counts 0 and fails later in the real run.
    The cap and the workspace counter measure stored objects. The base64 copy in `body_html` (about
    4/3 of the embedded bytes, at most 5 MiB of images per message) lives in the database like message
    text and is not counted; counting it in the sizing only would refuse imports the counter would
    accept.
  - *Undo.* The attachment rows go with their messages; the files go with the orphan sweep.
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
  are not imported (saved replies carry none) and are listed. Personal folders are listed by response id and skipped unless `Options.IncludePersonalCannedResponses` (CLI `--include-personal-canned-responses`). HTML fallback keeps `<a href>` as `text (url)` and warns when an `<img>` is dropped. A folder list that fails for a reason other than 403/404 is a `saved_reply_folder` failure with id `all`; tickets still import. Verification compares the sum of folder
  `responses_count` with the responses listed; a refused or missing endpoint (403 or 404: a plan without
  canned responses) is only a note (`not_available`) and never makes the run `UNVERIFIED`; an
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

## In-app import (#97): importer hooks

The in-app import runs this importer in a background job of `collie serve`. The importer gained
options for it; with their zero values the CLI behaves as before.

- **`Mode: ModeRecent`** imports the tickets the list returns for `Since`, newest updated first, and
  nothing account-wide: no knowledge base, no contact or saved-reply list, no id probe, no totals.
  Its report is `INTERIM` (`interim: true`), never OK, or FAILED when an item failed. The job then runs `ModeFull`, the same run as
  the CLI, which skips the recent tickets by the re-run rule and alone can report OK. The probe is
  left out on purpose: it imports every ticket the list does not return (`ticket_not_listed`), so a
  first `Run` with `Since` imports the whole account and the recent tickets come last. The list is
  still read oldest first (the 300-page window needs it) and the slice is sorted in memory.
  Contacts and saved replies are left out because they are written before the slice is sized.
- **Capped plans in `ModeRecent`** size only the slice (`sizeRefs`) before its first write. The full
  run sizes everything left with `guardStorage` before its own first write. When that does not fit,
  the slice stays imported, the job ends FAILED with `storage_cap` and offers undo: a deliberate
  exception to "nothing is imported" that the owner accepted, so recent tickets do not wait for hours
  of sizing. The CLI never runs `ModeRecent`.
- **`ReserveRateLimit`** makes the run pause once `X-Ratelimit-Remaining` is at most
  max(10, 20 % of `X-Ratelimit-Total`), capped at half the limit, instead of at 0. The limit is per
  Freshdesk account and shared with the customer's own apps and webhooks; the in-app job runs for
  hours while the team still works in Freshdesk. Without a `X-Ratelimit-Total` header the reserve is 10.
- **`OnProgress`** receives every phase, counter and rate-limit wait with the API call count; the
  progress lines of the CLI are unchanged. The sizing run of a capped plan reports to the callback
  but writes no lines.
- **`Preview`** reads `/account`, the agents, the companies, the contact and ticket search totals,
  the newest listed ticket (with `updated_since=1970-01-01`, or the list answers only the last 30
  days), one archive lookup and the first page of canned responses and solution categories. It
  writes nothing. The ticket numbers are "at least": search leaves out archived tickets and archived
  tickets can have ids above the newest listed one. A 401 is `ErrKeyRejected`, a 403 on `/account` or
  `/agents` `ErrNeedsAdminKey`.
- **A refused key or a plan without API calls ends the run.** A 401 (`ErrKeyRejected`) or any
  answer with `X-Ratelimit-Total: 0` (`ErrPlanNoAPI`) stops the run at once instead of failing every
  item or waiting for a limit that never returns; what was imported stays (undo removes it). The
  Freshdesk Free plan has 0 API calls a minute (support article 225439); a Free account can start a
  14-day trial of a higher plan (support article 50000010099) and import during it, and when that
  trial ends mid-import the key stops working this way. Before this, the CLI recorded each ticket of
  a revoked key as a failure and went on.
- **Author ids.** Messages keep the Freshdesk agent who wrote them (`origin.author_external_id`)
  and conversations the agent they were assigned to (`origin.responder_external_id`), so a Freshdesk
  agent can be linked to a Colliedesk agent after the import. Messages imported before this have no
  id; a re-run updates the conversation origin but not existing messages.

## In-app import (#97): the job

Code: `server/internal/importjobs`. A row of `import_jobs` is the durable state of one in-app import
(or undo); `collie serve` runs the worker. No HTTP handler talks to Freshdesk.

### States

| State | Meaning | Next |
|---|---|---|
| `previewing` | Waiting for a worker to read the account (`freshdesk.Preview`) | `previewed`, `failed` |
| `previewed` | Counts shown, nothing written; the key is kept one hour | `queued` (start), `discarded` (cancel, a new preview, or one hour) |
| `queued` | Started, waiting for a worker | `running`, `cancelled` |
| `running` | A worker holds the lease; `phase` is `recent`, `full` or `undo` | `finished`, `failed`, `cancelled`, `retry_wait`, `waiting_key` |
| `retry_wait` | Freshdesk unavailable, another import holds the lock, or failed items are retried; runs again at `next_run_at` | `running`, `failed`, `cancelled` |
| `waiting_key` | The key cannot be opened (server restarted without `MAILBOX_TOKEN_KEYS`, or its key left the ring) | `queued` (new key), `cancelled` |
| `finished` | The full pass returned a report (`status` OK, UNVERIFIED or FAILED), or an undo finished | |
| `failed` | The run stopped; `error_code` says why. Imported data stays | |
| `cancelled`, `discarded` | Stopped by a person; imported data stays | |

One job per workspace is `queued`, `running`, `retry_wait` or `waiting_key` (partial unique index):
a second start answers "an import is running". The worker also takes the importer's advisory lock,
so the CLI and a job never run together; a job that finds it taken waits in `retry_wait`.

The key is erased (`sealed_key` and the in-memory copy) in the same update that moves a job to
`finished`, `failed`, `cancelled`, `discarded` or `waiting_key`; a check constraint refuses a key on
any of them. An undo job holds no key.

### Error codes

| Code | Cause | State |
|---|---|---|
| `freshdesk_key_rejected` | Freshdesk answered 401 (`ErrKeyRejected`) | `failed` |
| `freshdesk_needs_admin_key` | 403 on the account or the agents (`ErrNeedsAdminKey`) | `failed` |
| `freshdesk_plan_no_api` | `X-Ratelimit-Total: 0` (`ErrPlanNoAPI`): the Free plan, or a trial of a higher plan that ended | `failed`, never retried |
| `freshdesk_unavailable` | 5xx, 429 or network after the client's retries (`ErrUnavailable`) | `retry_wait`, backoff 1, 2, 5, 10, then 30 minutes; `failed` after 24 hours without progress |
| `import_running_elsewhere` | Another import or undo (CLI) holds the lock | `retry_wait` 1 minute, same 24 hours |
| `retrying_failed_items` | The full pass reported FAILED; it runs again (twice at most, while failures go down) | `retry_wait` 1 minute |
| `storage_cap` | The full pass does not fit the plan's storage; the recent tickets stay | `failed` |
| `knowledge_base_limit` | The knowledge base passes a limit (`KnowledgeBaseLimitError`) | `failed` |
| `key_unavailable` | The key cannot be opened | `waiting_key` |
| `no_progress` | Found running after a crash five times without progress in between | `failed` |
| `import_error` | Anything else: never retried, the message is kept | `failed` |

### Lease

The claim crosses workspaces (`PrivilegeImportJobClaim`) and returns only ids; everything else runs
in the job's workspace. A claim sets `claim_token` and a two-minute lease; the worker renews it every
30 seconds and reads `cancel_requested_at` then. Every write of a run presents the claim token; a
write that matches no row means another worker took the job, and the run stops. After a restart the
lease expires and the job runs its saved phase again; the ledger skips what is done. The recent
window ends at `started_at` minus `recent_days` (default 90; 0 skips the recent pass), so a rerun
reads the same window. The phase moves to `full`, with the interim report saved, before the full
pass starts.

Without `MAILBOX_TOKEN_KEYS` the key stays in the memory of the process that received it and the
job is pinned to that process (`owner_instance`). Another process, or the same one after a restart,
moves the job to `waiting_key`. Cloud sets the ring; it is shared with Gmail grants, so rotate it by
putting a new key in front (`k2:new,k1:old`), never by replacing it.

## Knowledge base (solutions)

Code: `solutions*.go`; the writes are `store/kb-import.go`, which take the same lock, limits,
numbering and versions as the API (`docs/internals/knowledge-base.md`).

- **Listed in full before any write**, on every run: categories, the folders of each category, the
  subfolders of every folder, the articles of every folder, `/settings/helpdesk` and, for every
  supported language other than the primary one, the translated articles of every folder; then
  `/products`. `--since` applies to tickets only. Settings and products are read only when there is at
  least one category, so an account without a knowledge base costs one call.
- **Folders are kept once by id and placed by `parent_folder_id`.** With Flexible Hierarchy the
  category's folder list already returns nested folders (the docs' example does) and `/subfolders`
  returns them again. Category → collection level 1, folder → 2, subfolder → 3. Deeper folders become
  level-3 collections named after the folders from level 3 down (`Advanced / Deep`) next to the level-3
  one, and warn `kb_folder_flattened`. A slug taken by a sibling gets `-<Freshdesk id>`.
- **HTML to Markdown** with html-to-markdown v2 (MIT; base, commonmark, table, strikethrough). Before
  converting, an iframe of a YouTube or Vimeo player is replaced by a marker word (a random nonce, so
  no article text can match it) and put back after conversion as the HTML the editor's Insert video
  writes (`kbhtml.PlayerSnippet`: rebuilt from the video id, `youtube.com` becomes
  `youtube-nocookie.com`), which the help center shows; it is not a warning. Other iframes, video,
  audio, embeds, objects, forms, scripts, styles, SVG and MathML are removed and named in
  `kb_article_markup_dropped`, as are a table cell with `colspan`/`rowspan` (its
  content is repeated in the cells it spans), a table cell holding a line break or a block (a Markdown
  cell is one line, so the converter writes that table as plain text: the words stay, the grid goes),
  subscript and superscript, a link whose scheme is not http, https or mailto (its text stays), and
  any tag the converter left in the Markdown (only players' raw HTML is shown). A rerun takes an
  article again only when Freshdesk changed it, and undo removes it whole, so neither behaves
  differently. Files on an article are not in the
  documented attributes; if the API sends them they are listed (`kb_article_attachments_not_imported`). `source_html` keeps the original; `source_meta` keeps `tags`, `seo_data`, `hits`,
  `thumbs_*`, `type`, `status`, `hierarchy`, `agent_id` and the draft reason. Timestamps are
  Freshdesk's; `published_at` is `updated_at` (Freshdesk has no publish time). The author is the
  Colliedesk agent with the Freshdesk agent's email.
- **Published only under the strictest reading.** `status` 2, every folder on the path
  `visibility` 1 (All Users), and the category in the help center's portal. The docs say
  `visible_in_portals` is "allowed only if the account is configured with multiple portals"
  (category attributes), so: no category with the field → one portal, every category in it; the
  field present → it must hold the portal (the only one the categories name, or
  `--help-center-portal`; another id is refused before writing); an empty array → draft; a category
  without the field while others have it → draft (portal unknown). `/products` only adds a note when
  the account has products and no category names a portal. Everything else is a draft, counted by
  reason in `knowledge_base.drafts`.
- **Images** go through the same host allowlist, type sniffing, 5 MiB limit and SHA-256 as message
  images, into `kb_media` with `counted` false (an imported file, like imported attachments), and
  the `src` becomes `/api/v1/kb/images/<id>` before converting. 404/410 is gone (ledger `skipped`,
  link kept, listed); any other failure fails the image and marks the article `failed`, which keeps the
  link; the next run tries again and writes the article again. Ledger id `<article>:<key>`
  (`<article>:<language>:<key>` for a translation); no `parent_source_id`, so the ticket reconcile
  queries, which group by parent, never see knowledge base rows. Translation images are linked to
  their article in `kb_article_media`, or the purge would delete them.
- **Translations** go to `kb_article_translations` (kept and exported, not rendered); `source_meta`
  says whether the language is in `portal_languages`. A 404 on a folder's translated list means
  nothing there is translated.
- **Limits are checked before the first write** (`kbLimitProblems`): articles and collections of the
  workspace (trash included) plus the new ones against `domain.MaxKBArticles` and
  `MaxKBCollections`, and every title, name, description, locale and converted body (images counted at
  the length of their Colliedesk address) against its column. A real run that would pass one stops
  with `KnowledgeBaseLimitError` and writes nothing; a dry run lists them in `knowledge_base.refused`.
  The write-time checks under `lockKB` stay authoritative (a race with the web app fails that item).
  Image bytes join the storage-cap sizing and the dry run's `storage_cap`.
- **Reconcile.** A folder's `articles_count` and `sub_folders_count` are compared with what was
  listed in it; categories and folders directly in a category are listed only (Freshdesk reports no
  count for them), which `Verification.Method` says. A mismatch, unreadable settings or an unreadable
  translated list is `UNVERIFIED`. A failing list is a failure (`kb_category`/`kb_folder`) and the rest
  imports. The category list: 404 is a note (`not_available`), 403 is `UNVERIFIED` (the key may only
  lack the right; the knowledge base is what customers fear losing), unlike canned responses.
- **Rerun by version.** An article is written again when Freshdesk's `updated_at` is newer than the
  ledger's or its last run failed, and only while its newest version is of kind `import`; the row is
  locked (`FOR UPDATE`) before that check. Otherwise Colliedesk's version stays and
  `kb_article_changed_in_both` lists it. An article deleted for good is not brought back. Collections
  are never renamed by a rerun. A folder's visibility or a category's portal can change without
  touching the article's `updated_at`, so every run judges publication again: an untouched published
  article Freshdesk no longer shows to everyone becomes a draft (a new import version,
  `kb_article_unpublished`); one changed in Colliedesk stays published with
  `kb_article_published_but_not_public`. A folder or portal change the other way never publishes an unchanged article.
- **Undo** removes imported articles whose newest version is the import (`updated_at > created_at`
  cannot tell: the article carries Freshdesk's timestamps), then imported collections left with no
  article and no collection (deepest first), then images the import added (`meta.created`) that no
  article links, with their files. `targetExists` covers every `kb_*` entity, so a kept article keeps
  its ledger row and a later import does not add it twice.

### Not verified against a live account

1. Whether the article list returns draft articles (and whether `articles_count` counts them; a
   difference makes the run `UNVERIFIED`, never `OK`).
2. Which version the API returns for a published article with an unpublished draft.
3. On which hosts article images live (the message-image allowlist is used).
4. Which rights the API key needs for solutions, `/settings/helpdesk` and `/products`.
5. Whether a single-portal account really omits `visible_in_portals`, and what the field holds on an
   account with several products.
6. Whether an article can carry attached files in the API (the docs list no `attachments` attribute).

Stored knowledge base images are not re-hashed by the reconcile, unlike attachments and message images.

## Not verified against a live account

The test suite uses a fake server built from the documentation. Before the first customer
migration, run a dry run and a full import against a real Freshdesk trial account and check:
that ticket ids are sequential from 1, the archive endpoint's status on plans without archive,
the expiry status of attachment URLs, `size` versus downloaded bytes, the filter query syntax and
totals (including whether spam is counted), rate-limit headers on the plan, conversation `source`
values for forwards and public notes, and the agent-as-requester case.

For the in-app import, also check (each is handled the safe way until then):

1. What Freshdesk answers on the Free plan, which has no API calls (support article 225439), and
   after a trial of a higher plan ends. The importer assumes an `X-Ratelimit-Total` of 0 (reported
   as `ErrPlanNoAPI`), a 401 (key refused) or a 403 on `/account` (needs an admin key, or a plan
   with the API); a 403 on every ticket is reported as item failures, so the report is FAILED.
2. Whether `X-Ratelimit-Remaining` reflects the per-endpoint limits (Tickets List 40 a minute on
   Growth) or only the account total, and whether every plan sends `X-Ratelimit-Total`; without it
   the reserve falls back to 10 calls.
3. Whether the rate limit of the plans whose table says "/hr" is per hour or per minute.
4. Whether `/account` and the archive endpoints need an admin key (a 403 on `/account` is reported
   as "needs an admin key"; a 403 on the archive as the archive refused, so the report is UNVERIFIED).
5. Whether the archive lookup of a ticket that is not archived answers 404 (the preview counts a 404
   as "archive readable").
6. How long an `attachment_url` stays valid (an expired one is read again, as for the CLI).
