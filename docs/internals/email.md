# Email channel internals

Decisions and traps only. Code: `server/internal/email` (parse, compose, SMTP, config; no database),
`server/internal/store/email-*.go`, `server/internal/emailoutbox` (worker),
`infra/mail/colliedesk-inbound-pipe.sh` (MTA wrapper, tested by `tests/mail-pipe-test.sh`).

## Decisions

- **A conversation the team starts opens a thread.** `POST /conversations` with `body` writes the
  conversation and its first reply in one transaction through the same `enqueueReply` as any reply, so
  Message-ID, reply token, trial rules and Cc rules are the same, and a refusal stores nothing. The
  outbox marks a delivery `Opening` when no earlier message of the conversation has a Message-ID and none
  is incoming; that email keeps the subject as written (no `Re: `), has no In-Reply-To or References and
  is not `Auto-Submitted`. Without an email channel the call answers 409 `email_channel_missing` instead of
  storing a reply nobody will receive.
- **Merged contacts route mail to the kept contact.** Every find-or-create of a contact by address
  (inbound mail, web form, problem reports, Freshdesk import) answers with `merged_into_id` when it is set,
  so a merge does not come back with the next email. The first-contact rule of the trial compares the
  inbound sender with the conversation contact's address, so merging a contact who wrote into one who
  never did is not a way to mail a cold address.

- **Forwarder-agnostic inbound.** One endpoint takes a raw RFC 5322 message. No vendor SDK and
  no provider-specific JSON.
- **The secret picks the workspace, never the message.** The inbound secret is
  `<key id>.<random>`. The key id finds the channel row; the SHA-256 digest of the whole secret
  is compared with `subtle.ConstantTimeCompare`. An unknown key id compares against a zero
  digest. Every later lookup (duplicates, references, reply tokens, contacts, bounces) filters
  by that workspace.
- **Accepted means 200; storage failure means 500.** Forwarders must retry 429 and 5xx. MTAs only
  retry on exit 75, so the shipped pipe wrapper maps those to `EX_TEMPFAIL`.
- **Idempotency on Message-ID per workspace.** IDs longer than 255 bytes map to
  `long-<sha256>@colliedesk.invalid` (a btree entry over ~2.7 KB fails to insert); references are
  mapped the same way so they still match. A message without Message-ID gets
  `sha256-<digest>@colliedesk.invalid` from From, To, Cc, Subject, Date, Content-Type and the body,
  not the raw bytes, so a redelivery through another hop is still a duplicate. In-Reply-To is
  capped at 10 ids and References at the last 50; all candidates resolve in one `= ANY` query.
- **Trusted authentication only.** Only the topmost `Authentication-Results` whose authserv-id
  equals `EMAIL_TRUSTED_AUTHSERV_ID` counts; without that setting every message is `none`. DMARC
  decides when present; otherwise any DKIM or SPF pass is `pass`. The verdict is shown, not enforced.
- **Bulk needs a bulk-sender signal.** `Precedence: bulk|junk` drops. `List-Id` or
  `Precedence: list` drop only with `List-Unsubscribe-Post` or `Feedback-ID`, because a Google
  Group in front of support@ stamps list headers (and `List-Unsubscribe`) on every customer mail.
- **Sending identity on a shared relay (`EMAIL_FROM_POLICY=verified`, the default).** A workspace
  sends as its support address only after a DNS TXT check: `_colliedesk.<domain>` must hold
  `colliedesk-verification=<token>`. The token is per workspace, so one workspace's record never
  verifies another's. Changing the address resets verification and the token. Unverified, the
  worker sends `"<workspace> via Colliedesk" <reply+<conversation token>@MAIL_FROM_DOMAIN>` as
  From and envelope sender, with `Reply-To: support+r-<token>@<support domain>`; replies thread
  through Reply-To and In-Reply-To. Without `MAIL_FROM_DOMAIN` an unverified workspace cannot send.
  `any` keeps the support address always (self-host, operator owns the relay).
- **Domain verification through Resend (`RESEND_API_KEY`).** TXT proves control of a domain but not that
  Resend will send for it or that the customer's DMARC aligns, so on the hosted cloud the domain is
  verified by Resend. Both steps are needed, in this order:
  1. **Proof of control.** `POST .../domain-verification` first checks the `_colliedesk` TXT record
     (the self-host mechanism). Without it the answer is 409 `dns_record_not_found` and Resend is not
     called. Resend adds any name it is given, and its account has a fixed number of domain slots shared by
     all workspaces, so a domain nobody proved must not cost one.
  2. **Resend's records.** With the proof, the first check creates the domain
     (`email_channels.provider_domain_id`), keeps the records it lists and their status in
     `provider_domain_records`, asks Resend to re-check (a domain is `pending` while it checks) and reads it
     again. `domain_verified_at` is set only on `verified`; anything else is 409
     `dns_record_not_verified` with each record's status. Saving the address never calls Resend, and
     settings read the stored snapshot, so a person never waits on Resend.
  No database transaction stays open during a Resend call (a held connection plus a nested one deadlocks the
  pool). Two requests that both see no domain both create one; the new id is stored with a compare-and-set
  on the id read before (`AttachProviderDomain`), and the request that loses queues its domain for deletion
  and uses the stored one. A snapshot is written only for the provider domain the channel still holds and
  never for one queued for deletion (`SaveProviderDomain`), so a check that raced an address change cannot
  bring a deleted domain back. Checks that reach Resend are limited to one per workspace per 30 s
  (`domain.ProviderCheckMinInterval`; 429 with `Retry-After`, no call), because the Resend account and its
  request limit are shared by every tenant. A stored id that Resend no longer knows (404) is replaced by a
  new domain in the same request.
  **Errors.** Only Resend's 422 `validation_error` (a name it refuses) is shown to the tenant, as 409
  `email_domain_unavailable`. Anything else (bad or restricted key, suspended key, 409 `resource_locked`, 429,
  5xx, network) is the operator's: the tenant sees a generic 502 and the log has the status and Resend's
  error name, never the key.
  **Duplicates.** Resend does not refuse a name already in the account: it creates a second domain with a
  new id and DKIM key (same selector `resend._domainkey`). Only one copy can have its records in DNS, so
  **only one workspace can hold a given domain name verified**: a partial unique index on the domain of
  provider-verified rows enforces it, and the second workspace gets 409 `email_domain_unavailable`. When
  Resend reports a stored domain no longer verified, `domain_verified_at` is cleared, so the workspace sends
  from the platform address again. If rows from before the rule still give several holders, a complaint
  about that name is counted for every one of them. Two teams of one company that both use
  `support@` and `billing@` of the same domain cannot both verify it this way; the second stays
  unverified and sends from the platform address. Ownership is proved by the TXT record first and then by
  the copy's own DNS records.
  **Slots are freed in the background.** A domain a workspace stops using (support domain changed, channel
  disconnected, a stored id replaced, or the workspace deleted by retention) is written to
  `email_provider_domain_trash` in the same transaction. That table has no foreign key to `workspaces`, so
  rows survive the workspace; `workspace_id` stays as a record of who held the domain, and the table keeps
  row-level security (tenant transactions only enqueue, the sweep reads it as a reviewed privileged path).
  `internal/providerdomain.Sweeper` runs every minute beside the outbox worker and calls
  `DELETE /domains/{id}`: 404 counts as gone; a failure keeps the row with `attempts`, `last_error` and a
  backoff of 1 minute doubling to 1 hour, and logs a warning. Requests never call Resend to delete.
  **Abandoned holder.** `collie email release-domain [--by <name>] <domain>` queues the holder's provider
  domain for deletion and clears its provider domain and verification, so the real owner can verify; the
  second workspace's 409 tells the tenant to contact support. No API route does it.
  **Unverified copies expire.** The sweep also releases a provider domain that stayed unverified for 7
  days (`domain.ProviderDomainUnverifiedTTL`, counted from `provider_domain_created_at`): it queues the id
  for deletion, clears it from the channel and sets `provider_domain_expired_at`, which settings show as
  "expired, verify again". Only a domain Resend has never reported verified expires
  (`provider_domain_first_verified_at` empty): one that was verified and later reads pending keeps its
  provider domain and records, because the customer published its DKIM key; only `domain_verified_at` is
  cleared (sending falls back) and the next verify restores it. This bounds what a trial can hold: the TXT
  proof is per name, so subdomains of one domain could otherwise each keep a slot until retention deletes
  the workspace. The sweep claims rows with `FOR UPDATE SKIP LOCKED` and a 5-minute lease before it calls
  Resend, so replicas do not delete one domain twice, and it waits 250 ms between calls to spare the
  shared request limit.
  Resend's 403 "domain has been registered already" has the generic name `validation_error` (the same
  name as an unverified-domain error and an inbox limit), so it is not told apart and shows as a 502.
  Region: the client sends none, so Resend's default (us-east-1) applies and the SPF MX value follows it; an
  account whose other domains sit in another region gets different records for these. Without the key the
  TXT check above is unchanged (self-host). The client is `internal/resend`; tests run it against an
  `httptest` server shaped like the real API (duplicate names allowed, DELETE, 422/404 bodies, a different
  DKIM key per domain).
- **Complaint pause.** After a complaint webhook is recorded (applied, copy or duplicate), the handler
  counts, for that workspace over 7 days, `email.complained` rows in `relay_webhook_events` and outbox rows
  with a `sent_at` (a reply failed by a complaint keeps `sent_at`, so it still counts as sent). 3 or more
  complaints and `complaints * 1000 > sent * 3` (strictly over 0.3%) set `workspaces.email_paused_at` and
  `email_pause_reason`. The limits are constants in `internal/domain/email-pause.go`. The pause is on the
  workspace, not the channel, so disconnecting and reconnecting the channel does not clear it. The
  `UPDATE ... WHERE email_paused_at IS NULL` decides who made the pause, so only that call mails the
  operator (`BILLING_ALERT_EMAIL`; a warn log when unset; a failed alert is logged and the pause stands).
  A duplicate webhook is evaluated too, because the first delivery may have recorded the event and then
  failed before the pause; it cannot add a complaint, since `(workspace_id, webhook_id)` is the key. The
  worker defers a paused workspace's rows 10 minutes at a time without spending the attempt, with the
  reason in `last_error`; rows can still be queued. `collie email resume <slug>` clears the pause and
  makes due only the rows the pause held (`email_outbox.held_by_pause`), not rows in error backoff, and
  starts a fresh window (`workspaces.email_pause_resumed_at`): complaints and sends before the resume stop
  counting. No API route clears it: an admin who could would defeat the control.
- **Automatic mail says so (RFC 3834).** `Compose` with `Standalone` (operator alerts, retention notices,
  reconcile reports) writes `Auto-Submitted: auto-generated`; agent replies carry no such header because
  a person wrote them. Inbound drops any `Auto-Submitted` other than `no`, so an auto-responder's answer
  to a notice never opens a conversation.
- **Replies that cannot be sent fail visibly at once.** With no SMTP relay, or an unverified domain
  and no platform domain, the outbox row is created `failed` with the reason. Retry returns 409
  until the cause is fixed, so a reply never sits in `queued` forever.
- **Outbox leasing with a claim token.** The worker claims one row at a time
  (`FOR UPDATE SKIP LOCKED`), so the 5-minute lease covers one send (at most ~75 s). The claim
  stamps a random `claim_token`; every finish requires that token and `status='queued'`, so a
  worker whose lease expired cannot overwrite the result of the worker that reclaimed the row
  (`ErrLeaseLost`). Outgoing Message-ID is fixed at enqueue, so resends carry the same ID.
- **Poison rows fail alone.** A row whose data cannot be loaded is failed in the claim; a row that
  cannot be composed (invalid contact address) fails in the worker; a row claimed after it used
  every attempt (crash loop) fails without sending. Backoff 30 s doubling to 1 h, 8 attempts,
  any 5xx fails at once.
- **Per-workspace hourly send limit (300).** Over the limit, the row is deferred 10 minutes
  without spending the attempt. Trial workspaces also have a cap of 200 recipients (To + Cc) per 24 h and may
  reply only to customers who wrote first; see "Trial outbound controls" in `billing.md`.
- **Bounces.** A delivery report (`multipart/report`, `MAILER-DAEMON`) is still dropped, but the
  Message-IDs in its returned headers mark our matching replies `failed`. When its
  `message/delivery-status` part names the failed recipients (`Final-Recipient` with
  `Action: failed`) and every one is in the reply's `cc`, the delivery keeps its status and the
  addresses are appended to `last_error`: the customer got the reply. This works only when the
  relay keeps our Message-ID and the DSN reaches `reply+...`. Resend and SES rewrite the Message-ID
  and take the envelope sender, so their DSNs never arrive here. For Resend, `POST
  /api/v1/webhooks/resend` (Svix signature, `RESEND_WEBHOOK_SECRET`) fails the reply instead. Each
  reply on the platform domain is sent from `reply+<token>-<reply id>@MAIL_FROM_DOMAIN`, where the reply
  id is the first 12 hex digits of its random Message-ID (48 bits; the local part stays under 64
  characters). The event's `from` therefore names one reply, and nothing is guessed from timing: two
  replies seconds apart cannot be confused. Inbound routing, the RCPT check and threading accept both
  this form and the older `reply+<token>@`; for the older form the webhook acts only when the relay
  kept our Message-ID. The `svix-id` is stored in `relay_webhook_events` in the same transaction, so a
  redelivery is `duplicate` even after an agent retried the reply. A reply still `queued` (the relay can
  report a suppressed address before the worker records the send) is failed too; the worker then loses
  its lease and leaves it. An event that names only the contact fails the reply and keeps any missed-copy note after
  the reason; one that names only Cc addresses of the reply leaves it sent and appends "Bounced from
  copied address" to the note, like a DSN. Resend does not document what `to` holds when a Cc copy
  bounces, so the contact next to others, or an address the reply was not sent to, changes nothing.
  `sent_at` is kept on failure: the trial recipient cap still counts the reply, the hourly send limit
  (`status='sent'`) does not. A `from` that `net/mail` rejects (unquoted "Acme, Inc.") is read from its
  last angle brackets. Ignored failures are logged at Warn with ids only. The SMTP `250`
  reply does not document the Resend email id and tags exist only on the HTTP API, so SMTP stays the
  only transport. Replies sent as a verified support address carry no token. Their events map to a workspace
  by the From domain, only when exactly one workspace holds that domain verified through the email provider
  (a TXT-only domain never matches, since support addresses are not unique across workspaces). A bounce
  there fails the reply only when the event's Message-ID is one we stored; a complaint is counted for the
  pause either way, with no message on the event row.
- **Inbound limits are a loop guard plus a ceiling.** 10 messages per sender address per 10
  minutes (the header From, which a sender controls) and 200 new conversations per workspace per
  10 minutes (holds when From rotates). A per-(workspace, sender) advisory lock serializes the
  count and duplicate check.
- **Third-party senders in a thread.** A known Message-ID or reply token threads regardless of
  sender (CC'd colleagues). The message shows its sender; replies go only to the conversation
  contact. With an untrusted verdict the message shows **Sender not verified**.
- **Customer email renders as HTML when it has one**, in `web/src/features/inbox/email-frame.tsx`: a
  `srcdoc` iframe with `sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"`, never
  `allow-scripts`, `allow-forms` or any top navigation. `allow-same-origin` exists only so the parent can
  measure the content (`fit-frame.ts`, ResizeObserver; the frame is as tall as the mail and scales wide
  fixed layouts down). It is safe because nothing in the frame can run: the sandbox has no scripts and the
  document CSP says `script-src 'none'`. A ResizeObserver in the parent is the only moving part; do not add
  `allow-scripts` for sizing. `email-html.ts` strips `meta`, `base`, `link`, `script`, forms, frames,
  `on*` and every URL attribute except `<a href>` (http, https, mailto, tel; opened with
  `target=_blank rel="noopener noreferrer"`) and `<img src>` (`data:image/` or the proxy path).
  CSP: `default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data: <app origin>`.
  Remote `url(...)` in CSS goes through the same proxy: a style attribute or `<style>` block is walked with a small
  CSS tokenizer (`mailimage/css.go`; comments, strings, escapes, `url()` and `image-set()` strings), every remote
  address becomes a signed proxy path and counts toward the 50 per message, and every `@import` is dropped (replaced by a space). Function and at-rule names are read with CSS escapes decoded (`\75 rl(`). The same address written many times is parsed and signed once. A mail
  brand logo is often a CSS background, and the browser must not even attempt a request to the sender.
  The mail's own `<body>` keeps `style`, `bgcolor`, `text`, `class`, `id` and `dir` (a white logo on a dark body is blank without them), and our base rules paint no body colour so they cannot cover the mail's. Text with newlines and no markup for them (a chat note in a `<div>`) gets `white-space: pre-wrap` only when the mail has no `br`, `p`, table, list or other structure and no indentation between tags. `height`/`min-height` in viewport units become `auto`/`0`, and `html,body{height:auto!important}` comes last. An image that fails to load and was declared at most 2 px square (or 0) is hidden, so a blocked tracking pixel shows no broken-image icon.
  Quoted history (`blockquote`, `.gmail_quote`, `.yahoo_quoted`) under a reply folds into
  `<details class="quoted-history">`. Plain text stays the fallback and a toggle: no Markdown, links only
  for visible http(s) URLs; `tidyPlainText` collapses whitespace-only line runs.
  Agent replies and notes keep Markdown.
- **Remote images go through a signed proxy** (`GET /api/v1/mail-image?u=<url>&s=<hmac>`,
  `server/internal/httpapi/mail-image-handler.go`, `server/internal/mailimage`). `GET .../messages`
  rewrites every remote `<img src>` in `body_html` for a person's session (read with the HTML tokenizer,
  not a regex; `srcset` dropped; at most 50 distinct images per message, the rest get an empty `src`). An
  agent token gets the HTML as stored and the route is PersonOnly for tokens.
  - **Signature:** HMAC-SHA256 over workspace id and URL. The key is 32 random bytes in `server_secrets`
    (created once with `INSERT ... ON CONFLICT DO NOTHING`, read by every process), so replicas, a deploy
    overlap and a restart all agree. It is not `SIGNUP_HASH_KEY`: that one has another purpose and another
    rotation.
  - **Fetch** (`mailimage.Fetcher`): the dialer's Control runs on every connection, so DNS rebinding and
    each of at most 3 redirects are covered: loopback, private, link-local incl. 169.254.169.254, CGNAT,
    multicast, unspecified, `fd00:ec2::254`, NAT64/6to4/Teredo, `::/96`, `fec0::/10`, `100::/64`; only ports
    80 and 443. `Proxy` is nil, so `HTTP_PROXY` cannot route around the guard (a test pins it). No cookies,
    credentials or Referer, neutral User-Agent, PNG/JPEG/GIF/WebP by sniffing, or an SVG when the whole body parses as XML with an `svg` root in the SVG namespace, is at most 1 MiB, nests at most 64 deep, has at most 10000 elements and at most 256 attributes on any element (checked before the XML decoder runs); 5 MiB for rasters, 10 s. An SVG is served with `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`, `nosniff` and `Content-Disposition: inline; filename=image.svg`, so opening the proxy address directly runs nothing, and it is only ever shown through `<img>` (never `object`, `embed` or `iframe`). The Freshdesk importer still stores raster images only.
  - **Admission:** at most 8 fetches at once and 4 per workspace. A request over the limit waits up to 10 s
    for a slot (a newsletter sends all its images together and an `<img>` never retries), then gets 503 with
    `Retry-After`. A workspace queues alone: it takes its workspace slot before a global one.
  - **Cache:** the image is written to `attachments.Storage` and indexed per workspace in
    `mail_image_cache`. It is a derived cache, not data: it is not counted against the plan storage cap, not
    exported, and evicted under pressure. A workspace may cache at most `MAIL_IMAGE_CACHE_BYTES` (default
    1 GiB; past it an image is served without being kept). `serve` sweeps hourly: rows fetched more than 30
    days ago go (a source that withdrew an image stops being served), then, while the whole cache is over the
    cap, the least recently served. `last_served_at` is written at most once a day per image. A stale copy
    is served only while the source is down and the row is inside its 30 days. Deleting a message does not
    delete its cached images; the 30-day limit bounds how long they stay.
  - The timeline never waits for an image: the text renders first and each image loads by itself.
  - The dial guard (`internal/netguard`) is shared with the Freshdesk importer.
- **An attachment is `inline` only when its bytes are embedded in `body_html`**, set by the writer that
  embedded it (the email channel for a `cid:` image that fit the 5 MiB budget, the Freshdesk importer for a
  rehosted image that fit), never from a file name. The formatted view hides only those rows; an
  over-budget image or an attachment a sender named `inline-image-...` stays listed. Rows written before
  the column existed are `false` and stay listed.

## Files and the raw message

- **Every file is kept.** Attachments, inline images (with their Content-ID) and embedded messages
  are decoded and written to `attachments.Storage` with their SHA-256 and linked to the incoming
  message. The raw message is stored gzip-compressed as `original-message.eml.gz`;
  `email_meta.original_attachment_id` points at it.
- **Storage runs inside the ingest transaction, after the decision.** Dropped and duplicate mail
  writes nothing. A storage error rolls back the message, contact and conversation and returns
  500, so the forwarder retries.
- **Inline images become `data:` URIs** in `body_html` (5 MiB budget per message). The sandboxed
  original allows only `data:` images, so nothing is fetched from the network. The Freshdesk importer
  uses the same representation for images Freshdesk hosts (`freshdesk-import.md`, Inline images).
- **Limits before storage:** 25 MiB request body, 100 files; both are 413. A trial or locked workspace
  also has a 2 GiB attachment cap: mail that would pass it is 403 `storage_limit_reached` (a permanent
  failure like `mailbox_closed`; see `billing.md`, Storage cap).
- **Platform domain inbound.** `MAIL_FROM_INBOUND_SECRET` authenticates the forwarder of
  `MAIL_FROM_DOMAIN` on the same endpoint. The workspace comes from the conversation of the
  `reply+<token>@MAIL_FROM_DOMAIN` recipient (tokens are unique and 128-bit). Only that domain
  counts; a token on another domain does not route. An envelope recipient at `MAIL_FROM_DOMAIN` is
  checked first, before the hosted slug lookup, in both inbound routing and the RCPT check, because
  `UnderDomain` is true for every name below `HOSTED_MAIL_DOMAIN`. `ConfigFromEnv` refuses a
  `MAIL_FROM_DOMAIN` that is one valid, non-reserved label below `HOSTED_MAIL_DOMAIN`, so no workspace
  can own it as a slug (tests: `TestReplyTokenBelowTheHostedDomainThreadsIntoItsConversation`,
  `TestConfigFromEnvKeepsTheReplyDomainOutOfTheWorkspaceSlugs`).

## Hosted cloud addresses (`HOSTED_MAIL_DOMAIN`)

`support@<slug>.<HOSTED_MAIL_DOMAIN>` is an inbound transport only. Workspace and domain logic are
unchanged; the workspace's Support address stays the identity customers see.

- **One endpoint, one secret.** The shared forwarder posts to the normal inbound endpoint with
  `MAIL_FROM_INBOUND_SECRET`. No new route, no per-workspace secret. The per-workspace URL and
  secret keep working for self-host and ignore hosted routing: a workspace secret always files
  into its own workspace.
- **Only the envelope recipient routes.** The forwarder writes the envelope RCPT TO as the topmost
  `X-Original-To`; `email.Inbound.EnvelopeTo` reads only that line. To, Cc and lower `X-Original-To`
  lines are sender-controlled and never route (test:
  `TestHostedAddressMailReachesOnlyItsOwnWorkspace`). The shipped Worker and the Postfix `pipe`
  `flags=O` both put it first.
- **Refuse at RCPT on our own MTA.** The hosted cloud runs Postfix for `*.HOSTED_MAIL_DOMAIN` (owner
  decision 2026-10-15). Before it accepts `RCPT TO` it asks `POST /api/v1/channels/email/inbound/recipient`
  with the server-wide secret: 204 accept, 404 refuse with 550. The answer comes from the same lookup as
  inbound routing, so the two cannot disagree. Refusing in the session avoids backscatter: a bounce sent
  after `250` goes to the envelope sender, which spam forges. The pipe maps a late 404 to `EX_NOUSER` (67).
- **Reject, not drop.** A recipient under the hosted domain with no matching workspace, or whose
  workspace has no connected email channel, is `404 unknown_recipient`, so the forwarder bounces it.
  Everything else on the platform secret keeps the old behaviour (`200 dropped`).
- **Slug as subdomain.** The workspace slug is the subdomain. `email.ValidateHostedSlug` allows one DNS
  label (1-63 of `a-z0-9-`, no edge hyphen, no `xn--`) and blocks a reserved list. A workspace whose
  slug fails has no hosted address and no route until an admin picks another. Renaming is a one-way
  door for the old address; the UI warns. A workspace cannot set its Support address to another
  workspace's hosted address.
- **Forwarded mail trust model.** A forward keeps the original `From`, so the conversation and
  contact belong to the real customer. Authentication comes from `Authentication-Results` of the
  trusted server only (`EMAIL_TRUSTED_AUTHSERV_ID`, set to the Cloudflare name on the hosted cloud).
  After a Gmail forward SPF refers to Gmail's IP, not the original domain, so a good verdict
  comes from DMARC (DKIM survives forwarding); a missing or failing verdict is shown as
  **Sender not verified** or **Failed sender verification**, never used to reject. ARC headers are
  ignored: they are written by the forwarder, not by a server we trust. Self-host behaviour and
  anti-spoofing are not changed. Gmail's forwarding confirmation (from `forwarding-noreply@google.com`)
  is ordinary mail and becomes a conversation (test: `TestGmailForwardingConfirmationBecomesAVisibleConversation`).

### Receiving mail for `*.HOSTED_MAIL_DOMAIN`: checked against Cloudflare docs, 2026-10-15

- Email Routing is enabled per domain. A subdomain is added from the apex domain's Settings
  (Subdomains), and Cloudflare then writes its DNS records. API: `POST /zones/{zone_id}/email/routing/dns`
  with `name`, and `GET` with `?subdomain=`. The docs do not describe a wildcard MX for Email Routing.
- **Catch-all rules exist only for the apex domain** ("Catch-all rules are only available for the apex
  domain", email-routing/setup/subdomains), and **a zone has at most 30 domains** for Email Routing
  and Email Sending together, apex included.
- So per-workspace Cloudflare subdomains stop at about 28 workspaces, and a catch-all cannot serve
  `*.colliedesk.com`. A "mail domain provisioner" calling the API would hit the 30 limit, so the
  server has none.
- The server design does not care which receiver feeds it. **The hosted cloud uses an MTA run by us**
  (owner decision 2026-10-15, live since 09:05 UTC for every workspace): `*.colliedesk.com MX 10
  mx.colliedesk.com` -> Postfix + rspamd on collie-01 (infra repo `hosts/collie-01/mail`). Per-workspace
  Cloudflare subdomains are retired; the Worker in `infra/mail/cloud-inbound-worker` stays as an option for
  self-hosters.
  - Postfix: `relay_domains` = a pcre map of one-label subdomains (queried with the **domain**);
    `transport_maps` = a second pcre map anchored on `@` (a regexp transport map is queried with the **full
    address**; matching the domain there silently falls back to relay and bounces); transport `colliedesk`
    is `pipe flags=O` through `infra/mail/colliedesk-inbound-pipe.sh`, one recipient per call.
  - RCPT check: `infra/mail/colliedesk-rcpt-policy.py` (policy service) asks
    `POST /api/v1/channels/email/inbound/recipient`; unknown -> 550, server trouble -> 451.
  - rspamd milter writes the only `Authentication-Results` (authserv-id `mx.colliedesk.com`, the Postfix
    `myhostname`) after removing every sender-written one. `milter_headers` skips local and authenticated
    clients by default, which lets a forged header through: set `skip_local = false` and
    `skip_authenticated = false`. `milter_default_action = tempfail`, so a stopped rspamd defers mail
    instead of accepting it unwashed.
  - A wildcard MX does not apply to names that already have records (`app`, `www`, `send`, `rsend`, `mx`),
    which is why those slugs are reserved.

## Cc on replies

- `messages.cc text[]` (NULL = none, 1 to 10 entries) holds the list of an outgoing reply; the outbox
  reads it from the message, so a retry or a re-sent lease goes to the same people. Envelope
  recipients are the contact plus `cc`; the header is `Cc:`. There is no Bcc anywhere.
- Validation is in `httpapi/message-cc.go`: plain addr-spec, at most 10, lowercase, de-duplicated,
  refuses the workspace support address and its plus forms (`support+r-…@`, `email.SameMailbox`)
  and anything under `HOSTED_MAIL_DOMAIN` or `MAIL_FROM_DOMAIN`
  (a Cc to those would loop the reply back in). The store drops the conversation contact.
  `email.Compose` re-checks every address, so a stored value can never write a header line.
- A Cc address never fails the reply to the contact. The worker leaves out a stored address that is
  not a plain addr-spec, and `SMTPSender.SendCopying` skips a copy refused at `RCPT TO` (temporary
  refusals too: a retry would send the customer a second copy). The reply is `sent` and
  `last_error` names the missed copies; the web timeline shows that line under a sent reply.
  A refused contact still fails the delivery.
- Inbound Cc is stored in `email_meta.cc` (sender and own addresses removed, at most 50) and feeds the
  composer default only when that message's `authentication` is `pass` and, on the hosted cloud
  (`billing.enabled`), its `via` is `platform` (`suggestedCc` in the web app), so it never prefills an
  address the trial rule would refuse. It is a suggestion; the server never trusts it on send.
- A copied person's reply-all threads by `In-Reply-To`/`References` like any reply. It never changes
  the conversation contact, which is set when the conversation is created.

## Sending through Gmail (issue #95, phase A)

Code: `internal/google` (OAuth with PKCE, Gmail `messages.send`, no SDK), `internal/google/googletest` (the
stand-in tests and `cmd/google-stand-in` use), `internal/tokenseal`, `store/mailbox-connections.go`,
`httpapi/gmail-handlers.go`, `emailoutbox/gmail.go`.

- **Send only.** Scopes are `gmail.send` (sensitive, no CASA) plus `openid email`: `gmail.send` cannot read
  the address it sends from (`users.getProfile` needs a read scope), and the ID token names it. Inbound
  stays forwarding. Mailbox sync (`gmail.modify`, restricted, CASA) is phase B.
- **The transport is chosen when the reply is queued** (`email_outbox.transport`, also on retry), not when it
  is sent. A reply accepted for Gmail never leaves through SMTP: no connection (disconnected meanwhile) fails
  it with `MailboxDisconnectedNote`; a failed connection fails it with the reason. A retry chooses again.
  Rows queued for SMTP before Gmail was connected still go through SMTP.
- **A broken grant is a visible state, not a fallback.** `mailbox_connections.status='failed'` with
  `error_code`: `revoked` (token endpoint `invalid_grant`) or `permission` (a 403 that is not a rate limit).
  Only Google's answers mark it. While failed, new replies are recorded `failed` at once and retry is 409,
  exactly like a server without SMTP. Settings show `sending_from: null` and the alert.
- **This server's keys are not the grant's fault.** A sealed grant that does not open with
  `MAILBOX_TOKEN_KEYS` (an unknown key id during a rolling key change, a typo in the env) only makes the reply
  retry with `MailboxKeyNote`, and fail after the attempts; the connection stays `connected`, so fixing the
  keys sends the next attempt without a reconnect (review 2026-10-19: an earlier `key_missing` state was
  stored and outlived the fix). A server with no Google settings at all records Gmail replies `failed` at
  once (`MailboxNotConfiguredNote`) and shows `sending_from: null`.
- **Not every Gmail 403 is a broken connection.** 403 with `rateLimitExceeded`, `userRateLimitExceeded`,
  `dailyLimitExceeded`, `quotaExceeded` and every 429/5xx retry with the normal backoff; other 4xx fail the
  reply only. A 401 refreshes once and tries again. Source: Gmail API "Resolve errors"
  (developers.google.com/workspace/gmail/api/guides/handle-errors, read 2026-10-19): the 403 rate reasons
  above, 429 "User-rate limit exceeded (Mail sending)" for the daily sending limit (it can last hours, so it
  may use all 8 attempts and fail), and 403 `domainPolicy` (the Workspace admin disabled Gmail apps), which
  counts as a broken connection. Marking failed is conditional on the sealed grant the
  worker used, so a reconnect that raced it is not marked.
- **Threading does not depend on Gmail keeping our Message-ID.** `gmail.send` cannot read back what was
  delivered. Reply-To stays `support+r-<token>@`, so the customer's answer threads by the token (test with a
  stand-in that rewrites the Message-ID). Whether Gmail keeps a client-set Message-ID was not verified;
  nothing relies on it.
- **"Sent" means Gmail accepted it.** Bounces go to the connected mailbox, not to `MAIL_FROM_DOMAIN` or the
  Resend webhook; they reach Colliedesk only if that mailbox forwards them, and then the DSN path applies.
  Per-recipient Cc refusals are not known at send time.
- **Identity.** From is the connected address with the workspace name; it may differ from the support
  address (owner decision 2026-10-19: allowed, Settings say where answers go). The From is always the
  account address (send-as aliases would need `gmail.settings.basic` to list). The
  connected address is refused in Cc like the support address. Domain verification and
  `EMAIL_FROM_POLICY` do not apply to Gmail replies; trial rules, Cc rules, the hourly limit and the
  complaint pause do, in the same order as SMTP.
- **Sealed grants.** AES-256-GCM, random nonce, additional data `colliedesk/mailbox/google/<workspace id>`
  (`google.GrantContext`), key ring `MAILBOX_TOKEN_KEYS` = `id:base64[,id:base64]` (first seals, all open).
  A grant copied into another workspace's row does not open there and never sends; test
  `TestAGrantSealedForOneWorkspaceNeverSendsForAnother`. The key is not in the database on purpose.
  **Key change:** put the new key first; a grant opened with an older key is sealed again with the first key
  by the worker on its next send (`resealGrant`, conditional on the stored seal, and the delivery then
  holds the new seal). Remove the old key only after every connected workspace sent a reply since, or
  reconnected; no command re-seals idle workspaces yet.
- **Consent binding.** `POST .../gmail` stores the SHA-256 of a random state with the agent and the PKCE
  verifier (10 minutes, `mailbox_oauth_states`). The callback is a public route (Google's redirect) that
  authenticates the session cookie itself, refuses bearer tokens, requires admin and an unlocked workspace,
  looks the state up inside the session's workspace (so another workspace's state is simply not found) and
  requires the same agent. Without this, a consent link sent to a victim would attach the victim's Gmail
  to the sender's workspace. Every outcome is a 303 to `/settings?gmail=connected|gmail_error=<code>`.
- **Revoking.** Disconnect, removing the channel, and replacing the account revoke the dropped grant at
  Google (best effort, after the row is gone, never inside a transaction). Google's revoke "removes all
  OAuth 2.0 scopes previously granted to a project, invalidating any issued access or refresh tokens for
  all clients registered under that project" (developers.google.com/identity/protocols/oauth2/web-server,
  read 2026-10-19), so a reconnect with the same account does not revoke the old token, and a failed save
  after a successful exchange does not revoke the new one. **Shared account:** before revoking, the server
  asks (`PrivilegeMailboxGrantShared`, a yes or no) whether another workspace holds a connection for the
  address, connected or failed; if one does, nothing is revoked and the dropping workspace's token stays
  valid at Google until the last holder drops the address. The order is fixed: remove our own row, commit,
  ask, revoke. Asking before removing would let two workspaces dropping together both skip the revoke
  (`TestTwoWorkspacesDisconnectingTogetherStillRevoke`).
- **Access tokens are not cached**: one refresh per send. At the 300/hour limit that is well inside Google's
  token endpoint quota; cache only if a profile says so.
- **Agents.** Connect and disconnect are approval routes; the approver receives the consent URL as `url`
  (withheld from the token, like an invite link). The callback is NotForPeople: no token can drive a
  browser through Google.

## Open

- **Filtered mail is not stored.** Dropped automatic mail is reported only in the 200 response.
- **Retention does not revoke Gmail.** Deleting a workspace removes its sealed grant with it, but the grant
  stays valid at Google until it expires or the account owner removes it.

## Traps

- **Google returns a refresh token only with `access_type=offline` and `prompt=consent`.** Without the
  prompt, a second consent of the same account answers without one; the callback refuses that
  (`no_refresh_token`) and leaves the existing connection alone.
- **A person can untick "Send email on your behalf"** on Google's screen; the token answer's `scope` then
  lacks `gmail.send` and the callback refuses (`send_not_granted`).
- **One Google account in two workspaces shares one grant.** Revocation is per account and project. Colliedesk
  does not revoke while another workspace holds the address, but the account owner removing the app in their
  Google account still stops both (`revoked` in each).
- **Testing-mode Google apps expire refresh tokens after 7 days** (External, unverified). That shows as
  `revoked`; use an Internal app to dogfood.
- **Reply tokens are lowercase hex.** Some relays lowercase the local part of an address.
- **Plus addressing must reach us.** Cloudflare Email Routing needs subaddressing or a
  catch-all for `support+r-…@`. Threading by In-Reply-To works without it.
- **Trusting the wrong authserv-id trusts the sender.** If the receiving server does not stamp
  its own header, a sender's header with the trusted name is the topmost match.
- **`multipart.Reader.NextPart` decodes quoted-printable and deletes the header**; the parser
  uses `NextRawPart` and decodes once, by the part's own `Content-Transfer-Encoding`.
- **PostgreSQL text rejects NUL bytes.** The parser strips them from bodies.
- **Header lines must stay under 998 bytes.** Subjects are cut to 250 characters and encoded-words
  are folded onto continuation lines.
- **`net/smtp` refuses PLAIN auth without TLS** except on localhost, and has no implicit TLS;
  port 465 is rejected at startup.
- **Never delete stored objects after a failed request.** Objects are content-addressed per
  workspace (same bytes, same key), so a logo in two concurrent mails is one object. A failed
  request leaves its objects; the retry writes the same keys and references them.
- **The `messages (workspace_id, id)` key** belongs to the attachment and import migration, which
  sorts before this one. This migration adds it only when missing and its down migration leaves
  it. A database that applied an earlier draft of this migration before that one merged would hit
  a duplicate constraint; no shared database ever did.
