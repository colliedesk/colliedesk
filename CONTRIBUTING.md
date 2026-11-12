# Contributing to Colliedesk

Thank you for helping improve Colliedesk.

Before a contribution can be accepted, every contributor must agree to the [Colliedesk Contributor License Agreement](CLA.md). You keep the copyright in your work; the agreement lets HC Labs use and license it. To agree, read CLA.md and post this comment on your pull request, exactly:

    I have read the Colliedesk CLA and I agree to it.

CI fails until the opener of the pull request and the GitHub account of every commit author have posted it (`.github/scripts/cla-check.py`). Then push a commit, or ask a maintainer to re-run the failed job.

Please keep changes focused, add tests for observable behavior, run `make test`, and do not include secrets or customer data.

## Ideas, bugs and the roadmap

- **Ideas** go to [Discussions → Ideas](https://github.com/colliedesk/colliedesk/discussions/categories/ideas). Upvote an existing idea rather than posting a duplicate.
- **Bugs** go to Issues with the bug template; they start as `needs-triage`.
- A maintainer labels an issue `accepted` when it belongs in Colliedesk, and `planned` when it is scheduled and listed on [ROADMAP.md](ROADMAP.md). CI fails when ROADMAP.md links an issue that is closed or not labelled for its section (`.github/scripts/roadmap-check.py`).
