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

1. Pick a `main` commit whose CI run passed (normally the head of `main`).
2. Tag and push it:
   ```sh
   git tag v0.1.0 <commit> && git push origin v0.1.0
   ```
3. `.github/workflows/release.yml` then:
   - rejects a tag that is not `vX.Y.Z` or `vX.Y.Z-prerelease`;
   - requires a successful CI push run on `main` for that commit;
   - rebuilds the image stamped with the tag and boots it;
   - upgrades an install of the previous release to it (`tests/upgrade-test.sh`, the test and
     fixture from `main`) and tests the rollback and the one-workspace restore; a failure stops the
     release before anything is published. On the tag-push path the tag already exists, so a
     failure there leaves a tag without an image or a GitHub release; delete the tag or fix and run
     Release again;
   - publishes `X.Y.Z` and `X.Y`, plus `latest` only when the tag is the highest stable version;
   - creates a GitHub release with generated notes.

A tag with a hyphen (`v0.2.0-rc.1`) is a prerelease: it gets `X.Y.Z` only and the GitHub release is
marked prerelease. Local and `go run` builds report `dev`.

`install.sh` still defaults to `edge`. Switch its default to `latest` after the first stable release
exists.
