# Operator console internals

The server's configuration lives in the database (`internal/serverconfig`), so an operator changes it
without a deploy. The environment keeps only the bootstrap: what opens the database, the origin, where
the server listens and stores files, and `MAILBOX_TOKEN_KEYS`, which seals the secrets stored here and
cannot be sealed by itself. `DISABLE_TELEMETRY` and `DO_NOT_TRACK` stay readable, only to turn the
usage report off.

## Decisions

- **Versions are snapshots.** Each save writes every key set (about 50 at most) as a new
  `server_config_versions` row; restoring copies an old snapshot, secrets still sealed, into a new
  version. No diff chain to replay, nothing lost by a restore.
- **Old variable names stay the parsers' interface.** `Snapshot.Getenv` answers with the names the
  parsers always read (`polar.ConfigFromEnv`, `email.ConfigFromEnv`, …), so no parser changed and their
  tests did not move. `cmd/collie/config_build.go` `buildConfig` is the one place that turns a snapshot
  into what serve runs; the CLI checks every change with it before saving.
- **Secrets are sealed with their key name as context** (`server-config:<key>`), so a sealed value moved
  to another key does not open. `redactSecrets` replaces a validation error that repeats a secret's
  value with one that names the key: several parsers quote what they reject.
- **The first import is by group, all or nothing,** under one advisory lock
  (`colliedesk-server-config`), so overlapping deploys import once. A group the database already
  holds is skipped (the database wins); a group holding only the old compose file's defaults
  (`SMTP_PORT=587`…) imports nothing. A secret the database lacks is imported as soon as a key ring
  exists, even into a held group: before the ring it could not be stored (D8), and skipping it then
  would lose it when the operator removes the environment.
- **The sign-up hash key is imported before it is ever generated,** in the same transaction: a fresh key
  would forget who had a trial. A key shorter than 32 characters is left out (sign-up stays off) rather
  than replaced.
- **Polar products are add-only** (`polar_products`, trigger): `PlanOf` falls back to `Products.Retired`,
  every product off sale, so an old subscription keeps its plan.
- **`trusted_proxies` unset trusts no proxy,** as the unset variable did. The installer saves the
  value its compose file used to pass (loopback and the Docker bridge, `InstallerTrustedProxies`) when
  `.env` does not set the variable, through the same `collie config import`; an explicitly empty
  `TRUSTED_PROXIES=` imports nothing and keeps trusting none. `TRUSTED_PROXIES` is therefore not a
  "compose default" to ignore on import.
- **`collie config import FILE|--stdin` never generates the sign-up hash key.** Only the server's start
  (and `--from-env`) reads the real environment, where the key may still be.
- **The installer probes the image (`collie help` lists `config`) before replacing `compose.yaml`:** an
  older image would run without the settings the new compose file no longer passes.
- **Secret variables stay in the shipped compose files** (`${X:-}`): without a key ring they are still
  read from the environment (D8); once imported and removed from `.env` they pass empty.
- **The server never refuses to start over its configuration.** A version that does not build is marked
  `rejected` (once, with an audit line) and the last good one runs, else every default.

## Re-serve

`collie serve` is a process (`cmd/collie/generation.go` `process`: pools, attachment store, the in-app
import keys, one Cursor client, `httpapi.Carry`) running one generation at a time (`generation`: the API
and every worker built from one version). The `http.Server` and its listener never change; its handler is
a `swapHandler` (atomic pointer). `reloader` reads `server_config_state.desired_version` every 5 seconds
and on `LISTEN colliedesk_config`:

1. Same version and same products fingerprint: heartbeat only. A product change makes no version, so the
   poll compares `Fingerprint(products)` too.
2. The new version does not build: `rejected` (once), `last_error` on the process, the generation keeps
   running.
3. Only `Hot` keys changed: `httpapi.Hot.Store` and done (the API reads them through `API.hot()`).
4. The generation runs an in-app import (`importjobs.Worker.Running`): only the hot settings apply,
   `waiting_for` says why, and the next poll tries again (a red log after 6 hours). Stopping the
   generation would cancel the import.
5. Otherwise: build the new generation (starts nothing), stop the old one's workers (30 seconds at most),
   start the new ones, swap the handler, `config.apply` in the audit log.

`httpapi.Carry` keeps what a rebuilt API must not reset: every rate limiter, the export limiter and links,
the alert dedupe, the video upload gate, the Founding state, and the WaitGroup of GitHub sends (shutdown
waits for every generation's). Config-independent loops (daily sweep, AI retrieval, the image cache sweep,
the reloader) run at process level (`process.Go`); `TestServeStartsWorkersOnlyThroughAGenerationOrTheProcess`
fails on a bare `go` statement elsewhere in serve. The environment is imported only at process start.

AI prices and the monthly allowance, retention and telemetry apply by re-serve, not at once: the AI
service and the retention sweeper take them at construction. A few seconds later than "hot", but correct.

## Traps

- A moved variable left in the environment is never read. It is logged at every start (error when it
  differs from the database, warning when equal) and `collie config doctor` exits 1; that is the only
  signal, by design (D9).
- `serverconfig` must not import `httpapi`: the operator routes in `httpapi` use the store. Validation
  that needs `httpapi` parsers lives in `cmd/collie` and reaches the API as a function.
- Tests that set `POLAR_*` and friends with `t.Setenv` test nothing now: write settings with
  `configure` (`cmd/collie/config_helpers_test.go`), which runs `collie config import --stdin --replace`.
- The end-to-end servers each get their own database and their settings through `collie config import`
  (`web/e2e/support/cli.ts`, `settingsFile`), the path an operator uses.
