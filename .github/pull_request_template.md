## What and why

<!-- One or two sentences. Write "Closes #N" for the issue this ships. State the review result in one line ("Review: no open findings."), not its rounds or finding counts; CI checks. -->

<!-- If the issue has a comment starting "Design spec", add this section (CI checks it):
## Design spec

- done: <mechanism>
- changed: <mechanism>, because <reason>
- not in this PR: <mechanism> (#issue)
-->

## Checklist

- [ ] Tests written first and passing (`make test`; `make e2e` for user-visible flows)
- [ ] API changes start in `contracts/openapi.yaml`
- [ ] New queries filter by `workspace_id`, with a cross-workspace test
- [ ] Every new action has a way back and a way to see its state
- [ ] Easy for both users: person steps and AI agent MCP calls, before and after, are in "What and why" (item 7 in AGENTS.md)
- [ ] New or changed UI actions are available to agents: route classified in `token-scope.go`, MCP tool added or gap tracked (item 6 in AGENTS.md)
- [ ] User-visible change updates its page in `docs/user/` (server operation: `docs/self-host/`)
- [ ] Ships a `planned` issue: its line is removed from `ROADMAP.md` in this PR (CI checks)

External contributions need the HC Labs Contributor License Agreement before they can be merged.
See `CONTRIBUTING.md`.
