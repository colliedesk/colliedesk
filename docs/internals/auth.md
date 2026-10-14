# Authentication internals

## Hashed-token lookup

Pairing links and sessions use high-entropy random bearer tokens. CollieDesk displays or returns the
raw token once, then stores only its SHA-256 digest. Authentication hashes the supplied token and
uses an indexed PostgreSQL `bytea =` lookup against that digest.

This SQL comparison is acceptable for this threat model. The compared value is a fixed-length
one-way digest, not a password or another low-entropy secret. A timing difference cannot reveal a
useful token prefix: an attacker must still guess the complete random token, while rate limiting and
transport security remain separate controls. Constant-time comparison is required if a future auth
path compares raw secrets or low-entropy values in application memory.
