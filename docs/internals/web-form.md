# Web form channel internals

Decisions and traps only. Code: `server/internal/store/web-form.go`,
`server/internal/httpapi/web-form-*.go`, migration `web-form-channel`.

## Decisions

- **One public route, no secret.** `POST /api/v1/public/forms/{workspace_slug}` is unauthenticated.
  The slug is the only selector; the workspace is resolved through the existing slug lookup
  (`PrivilegeWorkspaceBySlug`), and everything after runs in that workspace's scope with RLS.
- **Unknown, off and locked are one 404.** Same status, same body. The locked case follows the email channel (a locked workspace refuses new
  inbound), but answers 404 instead of `mailbox_closed` for that reason.
- **Honeypot after the lookup, before validation.** A bot gets the success answer and nothing is
  stored. An unknown slug still gets 404.
- **JSON only.** `text/plain` is a CORS "simple" content type that a foreign page can send with no
  preflight. Requiring `application/json` forces the preflight, which only a listed origin passes.
- **CORS is exact-match.** The `Origin` header is compared with the stored list; the answer echoes
  it and adds `Vary: Origin`. No wildcard. A preflight that is not allowed is the same 404 as a
  missing form. CORS is not authentication: a script outside a browser sets any `Origin`.
- **Two limiters, in this order.** First 30 requests per minute per client address across all
  slugs (junk slugs and preflights included), before any database lookup. Then the form is
  resolved, and only an open form is charged to the 5 per minute (address, slug) bucket, so junk
  slugs cannot fill that map. Both are sliding windows in memory, 20000 keys each; a full map
  evicts the key used longest ago instead of refusing new ones.
- **Own ceiling, 50 per 10 minutes per workspace,** counted over incoming messages marked
  `via=web_form`. Email keeps its own 200; form spam cannot starve mail.
- **Provenance lives on the message.** `email_meta` is `{from, authentication: none, via: web_form}`.
  The tag can be removed by an agent, the mark cannot. `customerWroteFirst` accepts only `via=platform`
  (or imports), so trial replies to form conversations stay refused; a test pins that.
- **Slug secrecy is not claimed.** Unknown, off and locked answer one 404, but the paths do
  different work, so timing can still tell them apart, and hosted mail addresses already contain
  the slug. The per-address ceiling makes such probing slow, not impossible.
- **Client address.** The repo had no proxy handling. `TRUSTED_PROXIES` lists proxies; only a
  peer inside it may supply `X-Forwarded-For`, and the rightmost entry that is not a trusted proxy
  counts, so entries a client wrote on the left are ignored. Entries may carry a port. IPv6 counts
  by /64. The variable is `TRUSTED_PROXIES`, shared with sign-up and parsed by
  `httpapi.ParseTrustedProxies`. Unset and empty both trust none (fail closed) and log a WARN at
  startup; there is no default in server code, because `requireSignupConfig` must keep refusing the
  hosted cloud when the operator forgot it. Only the self-host compose files default it, to loopback
  plus `172.16.0.0/12` with `${VAR-default}` (no colon, so only an unset variable takes the default):
  Docker delivers host connections from the bridge gateway, not from loopback. A test checks those
  defaults trust the gateway and no public address.
- **Form posts do not count toward the per-sender email limit.** That limit counts messages by
  `email_meta.from`, and anyone can post a customer's address to the form. The count skips
  `via=web_form` (test: ten posts as a customer, then that customer's real mail is accepted).
- **The address-ceiling 429 has no CORS headers.** It runs before the origin list is known, and the
  server never echoes an unchecked `Origin`.
- **Validation errors carry `error.fields`.** The shared `Error` schema gained an optional
  `fields` map; existing responses are unchanged.
- **Settings table is separate.** `web_form_channels` (one row per workspace, RLS like the other
  tables). No row means off. Turning off keeps the origins.
- **Locked workspaces cannot change the settings.** The settings routes are not on the locked
  allowlist, so they answer 402 like other authenticated routes; the public form is closed anyway.

## Traps

- Submissions are not verified email: anyone can submit anyone's address.
- The limiter is per server process. Several processes each count on their own.
- Message text is stored as plain text and the UI renders customer text as plain text.
