# CI and releases

## Pull requests and `main`

`.github/workflows/ci.yml` runs `go`, `web`, `e2e`, `onboarding`, `image` and `roadmap` (which also runs the compose smoke test) in parallel. The `image`
job builds the server image once, then:

1. `tests/image-boot-test.sh`: checks the stamped version, then boots the image against Postgres
   (`migrate`, `serve` with the row-level security login) until `healthcheck` passes.
2. `tests/install-smoke.sh`: runs `install.sh` against that image and signs in with the printed link.
3. On `main` only: pushes the image as `sha-<commit>` with its SBOM and provenance. The two checks
   above run in the background meanwhile, and the job fails when either fails; so `sha-<commit>`
   can exist for a commit that failed them, and only `edge` (moved after `ci-ok`) is deployed.

Before the build, the job starts `tests/upgrade-test.sh` in the background with
`UPGRADE_TEST_RESTORE=before` ([upgrade testing](upgrades.md)): it fills an install of the previous
release with the upgrade fixture and tests both restores while the image builds, then upgrades it
to the built image and fails the job on any lost row, file or cross-workspace link.

`ci-ok` collects every job result. It is the one check to watch, so adding or splitting jobs never
renames it. `tests/ci-ok-needs-test.sh` fails the run when a job is missing from `ci-ok`'s `needs`.

On `main`, `.github/workflows/publish-edge.yml` runs when CI completes and retags that run's `sha-<commit>`
digest as `edge` only if CI passed on a push to this repository's `main`. It skips the retag when `main`
has already moved on, so a late run never moves `edge` backwards. It is the CD phase, with its own
60-second clock from the CI run's completion (`ci-budget.py --phase cd`). The image reports
`edge-<12-char sha>` from `collie version`.

A newer push to a PR cancels its older run. On `main` a running run is never cancelled, but GitHub
keeps only one pending run per group: during a burst of merges the middle commits get no CI run.
To release one of those commits, re-run CI for it first or tag a later commit.

## Cutting a release

1. Run Release (`gh workflow run release.yml -R colliedesk/colliedesk -f part=minor`). It takes the
   commit Colliedesk Cloud runs and the next version for `part` (`minor` by default, `patch` for fixes
   only, `major` for a breaking change; `.github/scripts/release-next.sh`). The first release after
   the `v0.0.N` ones is `v1.0.0`. Or tag a `main` commit whose CI run passed and push the tag:
   ```sh
   git tag v1.2.0 <commit> && git push origin v1.2.0
   ```
2. `.github/workflows/release.yml` then:
   - refuses a tag `.github/scripts/release-tag-check.sh` does not accept: `vX.Y.Z` from `v1.0.0`,
     or one of the earlier `v0.0.N`; no other `0.x` and no pre-release suffix;
   - requires a successful CI push run on `main` for that commit;
   - rebuilds the image stamped with the tag and boots it;
   - requires `restore-workspace.sh` in the release commit (the upgrade guide links it at the tag);
   - upgrades an install of the previous release to it (`tests/upgrade-test.sh`, the test and
     fixture from `main`) and tests the rollback and the one-workspace restore; a failure stops the
     release before anything is published. On the tag-push path the tag already exists, so a
     failure there leaves a tag without an image or a GitHub release; delete the tag or fix and run
     Release again;
   - publishes `X.Y.Z` and `X.Y`, plus `latest` only when the tag is the highest stable version;
   - builds the client files (below) before publishing anything, and attests them;
   - creates a GitHub release with generated notes and attaches the client files.

Pre-release tags (`v1.3.0-rc.1`) are refused by the tag check; `release.yml` still marks a release
with a hyphen as a prerelease if that check ever allows one. Local and `go run` builds report `dev`.

`install.sh` still defaults to `edge`. Switch its default to `latest` after the first stable release
exists.

## Client files

Besides the image, a release carries what a person installs on their own computer to connect an
AI host (`docs/user/connect-*.md`). `.github/scripts/client-assets.txt` names them, and both
`.github/scripts/client-release.sh` (which builds them) and the tests read it:

- `collie_<system>_<machine>.tar.gz` (`.zip` on Windows): `cmd/collie-client`, the about 6 MB
  `collie` that only has `mcp`, `version` and `help`. Names follow `uname -s`/`uname -m` (and
  PowerShell's `PROCESSOR_ARCHITECTURE`), so a guide's install command picks its file:
  `Darwin_arm64`, `Darwin_x86_64`, `Linux_x86_64`, `Linux_aarch64`, `Windows_AMD64`, `Windows_ARM64`.
- `colliedesk.mcpb`: the Claude Desktop extension, a ZIP of `cmd/collie-client/mcpb-manifest.json`
  (its version set to the tag) and the binaries. The format picks a command per platform, not per
  CPU, so macOS gets one universal binary, joined on Linux by `internal/release/universal` (lipo
  is macOS only); Windows gets the x64 binary, which Windows on Arm also runs. The token is passed
  only in `env` (`TestTheExtensionPassesTheTokenOnlyThroughTheEnvironment`).
- `SHA256SUMS` of the files above. `actions/attest-build-provenance` attests every file, so
  `gh attestation verify <file> -R colliedesk/colliedesk` proves it was built here.

The guides link `releases/latest/download/<name>`, so a renamed file breaks them at the next
release: change the name in `client-assets.txt` and the guides together. Try a build on a Mac with
`sh .github/scripts/client-release.sh v0.0.0 <dir>` (needs Go, `jq` and `zip`). The binaries are not
signed with an Apple or Windows certificate.
