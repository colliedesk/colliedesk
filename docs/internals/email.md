# Email channel internals

Decisions and traps only. Code: `server/internal/email` (parse, compose, SMTP; no database),
`server/internal/store/email-*.go`, `server/internal/emailoutbox` (worker).

## Decisions

- **Forwarder-agnostic inbound.** One endpoint takes a raw RFC 5322 message. No vendor SDK and
  no provider-specific JSON. Any forwarder that can POST bytes works.
- **The secret picks the workspace, never the message.** The inbound secret is
  `<key id>.<random>`. The key id finds the channel row; the SHA-256 digest of the whole secret
  is compared with `subtle.ConstantTimeCompare`. An unknown key id compares against a zero
  digest, so both failures take the same path. Every lookup after that (duplicates, threading
  references, reply tokens, contacts) filters by that workspace. A message that carries another
  workspace's reply token or Message-ID starts a new conversation in the posting workspace.
- **Accepted means 200.** Created, threaded, duplicate and dropped all return 200 with a
  `status`, so forwarders do not retry them. Storage failures return 500 so they do retry:
  nothing is lost silently.
- **Idempotency on Message-ID per workspace** (`messages_workspace_email_message_id_idx`).
  A message without Message-ID gets `sha256-<digest>@colliedesk.invalid` from its raw bytes. A
  concurrent duplicate that hits the unique index is re-read and reported as `duplicate`.
- **Outgoing Message-ID is fixed at enqueue time**, on the support address domain. Retries and
  lease-expiry resends carry the same ID, so receivers de-duplicate. Delivery is at least once.
- **Outbox leasing.** `ClaimDueDeliveries` uses `FOR UPDATE SKIP LOCKED`, sets `locked_until`
  and counts the attempt in one statement. A worker that dies mid-send leaves the row due again
  after the lease (5 minutes). Backoff: 30 s doubling to 1 h; 8 attempts; any 5xx reply fails
  at once. `failed` is final until an agent retries (reverse state: failed → queued).
- **Notes never reach the outbox.** Only `CreateMessage` with kind `outgoing` enqueues, and only
  when the workspace has a channel. Replies written before the channel existed are not sent
  retroactively, and the migration queues nothing.
- **Authentication-Results: topmost header only.** It is the one added by the receiving server
  nearest to us. DMARC decides when present; otherwise any DKIM or SPF pass is `pass`. The
  verdict is shown, not enforced: the forwarder owns rejection policy.
- **Rate limit per sender address**, 10 messages per 10 minutes, counted from stored incoming
  messages. Exceeding it returns 429, never a silent drop.
- **Attachments are listed, not stored**, in `messages.email_meta`. The attachment model is a
  separate change.

## Traps

- **Reply tokens are lowercase hex.** Some relays lowercase the local part of an address; a
  case-sensitive token would stop matching.
- **Plus addressing must reach us.** Cloudflare Email Routing needs subaddressing or a
  catch-all for `support+r-…@`. Threading by In-Reply-To works without it.
- **A forwarder can forge Authentication-Results.** Anyone with the secret can. Treat the
  verdict as the forwarder's claim. Forwarders should drop upstream copies of the header.
- **`multipart.Reader.NextPart` decodes quoted-printable and deletes the header**; the parser
  uses `NextRawPart` and decodes once, by the part's own `Content-Transfer-Encoding`.
- **PostgreSQL text rejects NUL bytes.** The parser strips them from bodies.
- **`net/smtp` refuses PLAIN auth without TLS** except on localhost. Leave `SMTP_STARTTLS=true`
  for real relays.
