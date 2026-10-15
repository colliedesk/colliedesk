# Authentication internals

## Hashed-token lookup

Pairing links and sessions use high-entropy random bearer tokens. Colliedesk displays or returns the
raw token once, then stores only its SHA-256 digest. Authentication hashes the supplied token and
uses an indexed PostgreSQL `bytea =` lookup against that digest.

This SQL comparison is acceptable for this threat model. The compared value is a fixed-length
one-way digest, not a password or another low-entropy secret. A timing difference cannot reveal a
useful token prefix: an attacker must still guess the complete random token, while rate limiting and
transport security remain separate controls. Constant-time comparison is required if a future auth
path compares raw secrets or low-entropy values in application memory.

## Optional cloud sign-in

Cloud sign-in is an OIDC-style bridge, not a second Colliedesk session system. The browser obtains
an RS256 session JWT from the configured identity provider. The server verifies it locally, maps its
verified email to an existing agent, creates an internal one-time pairing link, and redeems that link
through the normal session path. The provider token is never stored. Sign-in never creates a
workspace; the separate sign-up route below does, and only on the hosted cloud.

The bridge is provider-agnostic. Clerk is the current web widget, but the server only depends on an
issuer, a JWKS URL, authorized parties, and claims. All of these values must be present or cloud
sign-in remains disabled:

```text
CLOUD_AUTH_ISSUER=https://<frontend-api-host>
CLOUD_AUTH_JWKS_URL=https://<frontend-api-host>/.well-known/jwks.json
CLOUD_AUTH_AUTHORIZED_PARTIES=https://desk.example.com
CLOUD_AUTH_EMAIL_CLAIM=email
CLERK_PUBLISHABLE_KEY=pk_live_...
```

`CLOUD_AUTH_AUTHORIZED_PARTIES` is a comma-separated list of exact origins. The email claim defaults
to `email`. `CLERK_PUBLISHABLE_KEY` is returned only by the public config endpoint and is safe for the
browser; no Clerk Secret Key is required because Colliedesk never calls Clerk's Backend API.

### Cloud sign-up (hosted cloud only)

`POST /api/v1/auth/cloud/signup {workspace_name}` starts a 14-day trial for a Clerk-verified email
(same bearer token, same `Origin` check as the exchange). The route answers 404 unless cloud sign-in
and billing are both configured, so a self-hosted server never offers it. Code:
`httpapi/signup-handler.go`, `auth/signup.go`, `domain/trial-email.go`, `domain/brand-names.go`.

- **One trial per mailbox.** The key is the email lowercased, with the `+tag` of the local part
  dropped; for `gmail.com` and `googlemail.com` dots are removed and the host is `gmail.com`. A
  second sign-up is 409 `trial_already_used` and the message points to sign-in. The record survives
  deleting the workspace (`ON DELETE SET NULL`), so deleting does not reset the trial. Other
  providers' aliases (a catch-all domain, `ada@corp` vs `ada@alias.corp`) are not detected.
- **Three sign-ups per client address in any rolling 24 hours**, then 429 `rate_limited`. A refused
  attempt is not counted. The check, the workspace insert and the record are one privileged
  transaction under an advisory lock (the cheap refusals run once before the lock, so doomed requests do not queue on it), so a failed create burns neither the email nor an IP slot.
- **Stored data is minimal and keyed.** `cloud_signups` holds the HMAC-SHA256 of the normalized email,
  the HMAC-SHA256 of the client address, the time and the workspace id. The key is `SIGNUP_HASH_KEY`
  (at least 32 random characters, kept in Bitwarden, passed through the compose environment). Without
  the key a row cannot be tested against a list of addresses; a plain hash of an IPv4 address or of a
  breached email could. Without a key (or one shorter than 32 characters) `serve` keeps sign-up off, answers it with 503 `signup_unavailable` and logs why; every other route keeps working. The key cannot be
  rotated without losing the trial history (every stored hash would stop matching), so treat it like a
  database secret. The IP hash is set to NULL once the row is 30 days old by a sweep that `serve`
  runs at start and every hour (`Service.ClearSignupAddresses`), not by sign-ups. The email hash
  stays for good, because it is what stops a second trial; it is a pseudonymous fingerprint kept
  under legitimate interest (fraud prevention). The privacy policy must say: we keep a one-way keyed
  fingerprint of your email to prevent repeated free trials. The client address is not logged at
  INFO; the log line carries the new workspace slug only.
- **Client address behind the proxy.** `TRUSTED_PROXIES` is a comma-separated list of CIDRs (or single
  IPs) of the reverse proxies whose `X-Forwarded-For` is believed; the default is empty, meaning the
  peer address is the client. When the peer is trusted, the header is read from the right and the
  first entry that is not a trusted hop is the client (entries to its left were written by the
  client). A malformed list stops `serve` at start. An empty list while sign-up would be enabled (cloud
  sign-in and billing both on) turns sign-up off the same way as a missing key: without it every sign-up shares the proxy's address and the
  fourth of the day is refused for everyone. IPv6 clients are bucketed by /64. On collie-01 Caddy
  runs on the host and reaches the container through Docker, so the peer is the gateway of the
  compose network that publishes `127.0.0.1:8080`. Trust exactly that address as a `/32` (read it
  with `docker network inspect`, for example `172.18.0.1/32`), not the whole network: then no other
  container on the network can forge `X-Forwarded-For`.
- **Catch-all domains and disposable mail defeat the one-per-email rule.** The key collapses `+tag`
  and Gmail dots only, so a custom domain with a catch-all address, or a disposable-mail provider,
  gives unlimited verified emails; the per-IP limit and the caps on mail and storage bound the damage.
  This is closed in Clerk, not in code: before go-live turn on Clerk's block of disposable email
  addresses and its sign-up bot protection, and keep them on. The go-live checklist in the trial plan
  lists both.
- **Workspace name.** 2 to 60 characters after trimming. `domain.DeniedBrand` first folds the name (lowercase, compatibility
  forms such as fullwidth letters, accents dropped, common Cyrillic and Greek look-alikes mapped to
  Latin), then splits it into
  words at every character that is not a letter or digit and refuses it (422 `name_not_allowed`) when
  consecutive words joined together equal a listed brand, so `PayPal Support`, `pay pal` and
  `G-O-O-G-L-E` are refused, while `Pineapple`, `Applebee` and `Googleplex` are not (a brand must be
  a whole word, never a substring). A real business named like a brand (`Apple Orchard`) is
  refused too: it contacts us and an operator creates the workspace with the CLI, which has no
  such check. Digit-for-letter spellings (`paypa1`) are not caught. The same check runs on
  `PATCH /workspace/slug` when billing is on.
- **Slug.** Derived from the name (runs of hyphens collapse, so it can never start with the reserved
  `xn--`); every candidate is checked as a hosted label; if it is taken, reserved for mail (`support`, `help`) or empty it
  becomes `<base>-<4 hex>`, tried up to twice more, then 409 `slug_unavailable`.

### Verification traps

- Customize the Clerk session token itself with `email` and boolean `email_verified` claims. The
  default token does not contain enough email information. Use the verified primary email only.
- JWT time checks allow at most 60 seconds of clock skew around `exp` and `nbf`. Keep host clocks
  synchronized; do not widen this window to hide clock problems.
- Public keys are cached by `kid`. An unknown `kid` triggers one JWKS refresh so normal key rotation
  does not interrupt sign-in. An unresolved key or refresh failure starts a 30-second cooldown to
  prevent repeated network fetches. A bad signature with a known `kid` does not trigger a refresh.
- `iss` and `azp` use exact string comparison. Keep the provider Frontend API URL separate from the
  Colliedesk origin, and list every real Colliedesk origin explicitly.
- The exchange writes an HttpOnly Colliedesk cookie. It therefore requires the same exact `Origin`
  check as other cookie-writing requests.
