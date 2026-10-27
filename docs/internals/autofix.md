# Autofix (bridge version)

A person sends a conversation to a coding agent the workspace already uses, on a private GitHub
repository connected through the Colliedesk GitHub App, and follows it to a pull request, its merge and
the release. Colliedesk runs no agent and keeps no model key; the only vendor key it keeps is Cursor's,
sealed. This page records decisions and traps; what people see is in `docs/user/`.

## One fix attempt, one set of rules

- States, connectors and outcome reasons live in `server/internal/autofix/states.go`. The migration's
  CHECKs and the contract's `FixState`, `FixConnector` and `FixOutcomeReason` hold the same lists;
  `TestTheDatabaseAndTheContractNameTheSameStatesAndReasons` fails when one drifts. A branch that adds a
  reason edits all three.
- Every signal (GitHub webhook, Cursor poll, the reconcile pass, the worker's clock) goes through
  `store.ApplyFixObservation`, which locks the row, asks `autofix.Decide` and records the event. A
  signal never fails: it moves the fix, is only noted on the timeline, or is ignored. Signals move
  forward only; an old one arriving late is ignored.
- A person's change (cancel, link or unlink a pull request, mark or unmark the release) goes through
  `store.ActOnFix` and `autofix.Act`, which refuse a move the state does not allow (409). The vendor
  side of a cancel (closing the issue, cancelling the Cursor run) runs after the transaction commits.
- One fix in progress per conversation (`fix_attempts_one_in_progress`). A reopened issue or pull
  request brings an ended fix back only while the conversation has no newer one in progress;
  otherwise it is only noted.
- `authorization_abandoned`, `authorization_denied` and `connector_disabled` (an admin turned the agent
  off, withdrawing the consent) end only a fix still `sending`, whose content has not left Colliedesk;
  once sent they are only noted, because the vendor already has it.
- A person's cancel is never undone by a signal; an issue closed while a pull request is open is not a
  cancellation (the pull request decides); an issue closed by a merged pull request whose own webhook
  was missed is a merge.
- `stale` (sent, no sign of the agent for 30 minutes) and the Fixes screen's `group` are computed in
  SQL on read; nothing stores them.

## Data

- `fix_attempt_events` is append-only: a trigger refuses UPDATE and the app role holds no UPDATE or
  DELETE. Events go only by cascade with their fix (cascades run as the owner).
- Conversations have no trash: they are deleted only by a Freshdesk import undo, and their fixes go
  with them by cascade, `sent_body` included. `conversation_deleted` stays a reason for a later trash.
- Disconnecting a repository keeps its fixes: `repo_id` becomes null and `repo_full_name` keeps the
  name it was sent to. Removing the installation keeps its repositories, paused.
- `autofix_webhook_deliveries` is platform level (no workspace, no grant to the app role); only a
  privileged transaction reads it.
- The export carries installations, repositories, connectors (only whether Cursor's key is set) and
  every fix with exactly what it sent and its timeline.

## How the work was split

The first branch shipped the schema, the rules, the store, the read routes and their MCP tools, the
export and the whole contract of the first release. Each later branch registers its own routes with
their `tokenTiers` rows and MCP tools, and adds its own token permissions with them
(`autofix:update`, `autofix:send`, `autofix_settings:*`): `TestEveryRoutePermissionIsHeldByTheTierTheRouteHad`
refuses a permission that opens no route, and that gate is not loosened. The privileged lookups
(installation → workspace, the worker's claim) come with the code that calls them, with their row in
the table of `workspace-isolation.md`.
