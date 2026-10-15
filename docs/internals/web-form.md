# Web form channel internals

Decisions and traps only. Code: `server/internal/store/web-form.go`,
`server/internal/httpapi/web-form-*.go`, migration `web-form-channel`.

## Decisions

- **One public route, no secret.** `POST /api/v1/public/forms/{workspace_slug}` is unauthenticated.
  The slug is the only selector; the workspace is resolved through the existing slug lookup
  (`PrivilegeWorkspaceBySlug`), and everything after runs in that workspace's scope with RLS.
- **Unknown, off and locked are one 404.** Same status, same body, so the endpoint does not tell
  which slugs exist. The locked case follows the email channel (a locked workspace refuses new
  inbound), but answers 404 instead of `mailbox_closed` for that reason.
- **Honeypot after the lookup, before validation.** A bot gets the success answer and nothing is
  stored. An unknown slug still gets 404.
- **JSON only.** `text/plain` is a CORS "simple" content type that a foreign page can send with no
  preflight. Requiring `application/json` forces the preflight, which only a listed origin passes.
- **CORS is exact-match.** The `Origin` header is compared with the stored list; the answer echoes
  it and adds `Vary: Origin`. No wildcard. A preflight that is not allowed is the same 404 as a
  missing form. CORS is not authentication: a script outside a browser sets any `Origin`.
- **Rate limit is attempts, not successes.** Sliding minute window per (client address, slug), in
  memory, 20000 keys at most (full means refuse). The store adds the email channel's ceiling of 200
  new conversations per workspace per 10 minutes, which holds when addresses rotate.
- **Client address.** The repo had no proxy handling. `TRUSTED_PROXY_CIDRS` lists proxies; only a
  peer inside it may supply `X-Forwarded-For`, and the rightmost entry that is not a trusted proxy
  counts, so entries a client wrote on the left are ignored. IPv6 counts by /64.
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
