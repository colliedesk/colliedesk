# What general availability guarantees

Colliedesk is generally available from release 1.0.0. This page is for a support team deciding
whether it can rely on Colliedesk Cloud, or on its own Colliedesk server. Every statement here is
true today. A change that makes one untrue changes this page in the same pull request.

## Versions

Releases follow [Semantic Versioning](https://semver.org). A breaking change to the API, the CLI or
the export format comes only in a new major version. New features raise the minor version, fixes the
patch version. Each release lists its changes on the
[releases page](https://github.com/colliedesk/colliedesk/releases).

## Upgrades keep your data

- Every schema change ships as a forward migration. `collie migrate` applies it to the database in
  place, before the new release starts. Colliedesk Cloud runs it on every deploy;
  [the upgrade guide](self-host/upgrade.md) has a self-hosted server run it the same way.
- Every pull request, and every release before it is published, runs an upgrade test in CI. The test
  installs the previous release and fills it with a test data set: three workspaces, a value in every
  column, and stored files with their checksums. It then upgrades that install to the new build. The
  test fails if a row, a file or a checksum changed or is missing, or if one workspace can read
  another workspace's rows. The same test restores a backup and restores one workspace on its own.
- A migration that changes existing data on purpose fails that test until it is declared, with the
  reason, in the repository.
- The test data set is written for the test. It holds no customer data.

How the test works: [upgrade testing](internals/upgrades.md).

## Export at any time

A workspace admin can export the whole workspace at any time, as one ZIP file in an open,
documented format: conversations with their messages and attachments, contacts, the knowledge
base, settings and their history. The export also works when a Cloud trial or subscription has ended and the workspace
is locked, and on self-hosted servers. See [export all your data](user/export.md).

## Colliedesk Cloud backups

- **Continuous.** Every change to the database is archived within a minute or two, encrypted, to
  storage off the Cloud server in the EU (Germany). The recovery point is 5 minutes: if the server is lost, at
  most the last 5 minutes of changes are lost.
- **Nightly, a second copy.** An encrypted database dump and an encrypted archive of the attachments,
  also stored off the Cloud server in the EU.
- **Restore drills.** We restore from these backups at least every 30 days and check the result.
  Last drill: 2026-11-05.

## Uptime

Every paid Colliedesk Cloud plan has an uptime commitment of 99.9% in each calendar month. Uptime is
measured by the checks on [status.colliedesk.com](https://status.colliedesk.com), which run from
several regions outside the Cloud server. Service credits and support response targets are in the
[Terms](https://colliedesk.com/terms).

## Self-hosted servers

A self-hosted server runs the same releases and the same migrations as Colliedesk Cloud, and every
release passes the same upgrade test. The Cloud backups and the uptime commitment do not cover it:
backups and uptime are the operator's job. See [backups and database](self-host/database.md),
[upgrade a self-hosted install](self-host/upgrade.md) and [support](self-host/support.md).
