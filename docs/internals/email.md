# Email channel internals

Decisions and traps only. Code: `server/internal/email` (parse, compose, SMTP, config; no database),
`server/internal/store/email-*.go`, `server/internal/emailoutbox` (worker),
`infra/mail/colliedesk-inbound-pipe.sh` (MTA wrapper, tested by `tests/mail-pipe-test.sh`).

## Decisions

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
  without spending the attempt. Trial workspaces also have a 200 replies per 24 h cap and may
  reply only to customers who wrote first; see "Trial outbound controls" in `billing.md`.
- **Bounces.** A delivery report (`multipart/report`, `MAILER-DAEMON`) is still dropped, but the
  Message-IDs in its returned headers mark our matching replies `failed`.
- **Inbound limits are a loop guard plus a ceiling.** 10 messages per sender address per 10
  minutes (the header From, which a sender controls) and 200 new conversations per workspace per
  10 minutes (holds when From rotates). A per-(workspace, sender) advisory lock serializes the
  count and duplicate check.
- **Third-party senders in a thread.** A known Message-ID or reply token threads regardless of
  sender (CC'd colleagues). The message shows its sender; replies go only to the conversation
  contact. With an untrusted verdict the message shows **Sender not verified**.
- **Customer email renders as plain text.** No Markdown, no remote images, links only for visible
  http(s) URLs with `rel="noopener noreferrer nofollow"`. Agent replies and notes keep Markdown.

## Files and the raw message

- **Every file is kept.** Attachments, inline images (with their Content-ID) and embedded messages
  are decoded and written to `attachments.Storage` with their SHA-256 and linked to the incoming
  message. The raw message is stored gzip-compressed as `original-message.eml.gz`;
  `email_meta.original_attachment_id` points at it.
- **Storage runs inside the ingest transaction, after the decision.** Dropped and duplicate mail
  writes nothing. A storage error rolls back the message, contact and conversation and returns
  500, so the forwarder retries.
- **Inline images become `data:` URIs** in `body_html` (5 MiB budget per message). The sandboxed
  original allows only `data:` images, so nothing is fetched from the network.
- **Limits before storage:** 25 MiB request body, 100 files; both are 413. A trial or locked workspace
  also has a 2 GiB attachment cap: mail that would pass it is 403 `storage_limit_reached` (a permanent
  failure like `mailbox_closed`; see `billing.md`, Storage cap).
- **Platform domain inbound.** `MAIL_FROM_INBOUND_SECRET` authenticates the forwarder of
  `MAIL_FROM_DOMAIN` on the same endpoint. The workspace comes from the conversation of the
  `reply+<token>@MAIL_FROM_DOMAIN` recipient (tokens are unique and 128-bit). Only that domain
  counts; a token on another domain does not route.

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
- The server design does not care which receiver feeds it. Two receivers work with the same
  endpoint: (a) up to ~28 subdomains on Cloudflare Email Routing with explicit rules to the shared
  Worker `infra/mail/cloud-inbound-worker`; (b) for unbounded workspaces, a wildcard
  `*.colliedesk.com` MX at an MTA run by us, piping each message through
  `infra/mail/colliedesk-inbound-pipe.sh` with `X-Original-To` added (Postfix
  `virtual_mailbox_domains` via `pcre` and a `pipe` transport with `flags=O`). A wildcard MX does
  not apply to names that already have records (`app`, `www`, `api`, the MTA host), which is why
  those slugs are reserved.

## Open

- **Filtered mail is not stored.** Dropped automatic mail is reported only in the 200 response.

## Traps

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
