# Security policy

## Reporting a vulnerability

Do not open a public issue for a security problem.

1. Open https://github.com/colliedesk/colliedesk/security/advisories/new (the repository's
   **Security** tab, **Report a vulnerability**).
2. Describe the problem, the affected version and the steps to reproduce it.

If you cannot use GitHub, email support@colliedesk.com with "Security" in the subject.

The report stays private between you and the maintainers. We reply within 3 working days
and keep you informed until a fix is released. We credit reporters in the advisory unless
you ask us not to.

## Scope

In scope: the Colliedesk server, the web app, the container image `ghcr.io/colliedesk/colliedesk`
and the install script. Cross-workspace data access is always treated as critical.

Out of scope: denial of service by volume, findings that need a compromised host, and reports
from automated scanners without a working proof.

## Supported versions

Security fixes go to the latest release and to `edge`. Upgrade to the latest release to receive them.

## Verifying an image

Every image published from `main` and every release carries a signed build provenance
attestation and an SBOM. To check that an image was built by this repository's workflows:

```sh
gh attestation verify oci://ghcr.io/colliedesk/colliedesk:latest -R colliedesk/colliedesk
```
