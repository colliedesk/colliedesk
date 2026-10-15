# CI and releases

## Pull requests and `main`

`.github/workflows/ci.yml` runs `go`, `web`, `e2e`, `onboarding`, `compose-smoke` and `image` in parallel. The `image`
job builds the server image once, then:

1. `tests/image-boot-test.sh`: checks the stamped version, then boots the image against Postgres
   (`migrate`, `serve` with the row-level security login) until `healthcheck` passes.
2. `tests/install-smoke.sh`: runs `install.sh` against that image and signs in with the printed link.
3. On `main` only: pushes the tested image as `sha-<commit>`.

`ci-ok` collects every job result. It is the one check to watch, so adding or splitting jobs never
renames it. `tests/ci-ok-needs-test.sh` fails the run when a job is missing from `ci-ok`'s `needs`.

On `main`, `publish-edge` runs after `ci-ok` passes and retags the tested `sha-<commit>` digest as
`edge`. It skips the retag when `main` has already moved on, so re-running an old run never moves
`edge` backwards. The image reports `edge-<12-char sha>` from `collie version`.

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
   - publishes `X.Y.Z` and `X.Y`, plus `latest` only when the tag is the highest stable version;
   - creates a GitHub release with generated notes.

A tag with a hyphen (`v0.2.0-rc.1`) is a prerelease: it gets `X.Y.Z` only and the GitHub release is
marked prerelease. Local and `go run` builds report `dev`.

`install.sh` still defaults to `edge`. Switch its default to `latest` after the first stable release
exists.
