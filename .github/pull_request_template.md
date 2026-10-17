## What and why

<!-- One or two sentences. Write "Closes #N" for the issue this ships. -->

## Checklist

- [ ] Tests written first and passing (`make test`; `make e2e` for user-visible flows)
- [ ] API changes start in `contracts/openapi.yaml`
- [ ] New queries filter by `workspace_id`, with a cross-workspace test
- [ ] Every new action has a way back and a way to see its state
- [ ] New or changed UI actions are available to agents: route classified in `token-scope.go`, MCP tool added or gap tracked (item 6 in AGENTS.md)
- [ ] User-visible change updates its page in `docs/user/`
- [ ] Ships a `planned` issue: its line is removed from `ROADMAP.md` in this PR (CI checks)

External contributions need the HC Labs Contributor License Agreement before they can be merged.
See `CONTRIBUTING.md`.
