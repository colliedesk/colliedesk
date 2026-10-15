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
through the normal session path. The provider token is never stored. There is no automatic sign-up.

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
