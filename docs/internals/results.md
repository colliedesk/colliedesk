# Results: decisions and traps

The results page (issue #100, `docs/user/results.md`) compares a team's numbers in Colliedesk with the
numbers of the history it imported. This file records what the numbers are built from and why.

## Status history

Nothing recorded when a conversation was resolved or reopened before migration
`20261023110000-conversation-status-changes`: status was overwritten in place and only `updated_at`
moved. Resolution time and reopen rate need both, so every change now leaves a row in
`conversation_status_changes` (`from_status`, `to_status`, `changed_at`, `actor_agent_id`, `source`).

| Decision | Why |
|---|---|
| A row trigger (`AFTER UPDATE OF status`, only when the value changes) writes the row, not the store functions | Five statements change a status today (one conversation, bulk, an inbound reply that reopens, the Freshdesk and Help Scout importers), and an older image after a rollback writes too. A trigger records all of them; a writer that forgets cannot exist. Same reasoning as `conversations.last_public_*` |
| `changed_at` is `clock_timestamp()`, not `now()` | `now()` is when the transaction began. An inbound reply's transaction starts, uploads its files, and only then reopens; a person who resolved meanwhile committed first but would sort after it, and the history would end on the wrong status |
| Inserts are not recorded | Every Colliedesk writer creates a conversation `open` (the column default). An importer creates it with the source status, which is history from before the move |
| Import writes are recorded, as `source = 'import'` | An import re-run overwrites a status with the one it read from the old helpdesk. Skipping it would leave a history that disagrees with the current status, and the export would miss it. The results page reads only `colliedesk` rows |
| Who changed it comes from `app.actor_agent_id`, set with `set_config(…, true)` before the write | The bulk and inbound `UPDATE`s have no actor column to read. `setStatusActor` sets it before every status write, empty for a customer reply, so a second change in one transaction never carries the first writer's name. Importers set `app.status_source = 'import'` and an empty actor in `run.begin` (`store.MarkImportWrites`) |
| `workspaces.status_history_since` | The history is complete only from the upgrade (or the workspace's creation). A metric that needs a resolution or a reopen before that date says when it will be available instead of showing a number built on a gap |
| The history leaves with the export, inside each conversation (`status_changes`), like time entries | No lock-in: the numbers on the results page can be recomputed from the archive |
| The down migration drops the table | Conversations keep their status. The next upgrade sets `status_history_since` again, so the results page never reads the gap |

Trap: a writer that changes a status inside a transaction where another writer already set
`app.actor_agent_id` must call `setStatusActor` itself, or the change is recorded under the
previous name.

## The baseline's import (anchor)

`results_state` names the import the baseline is measured from: `baseline_source` (the ledger source,
`freshdesk:account:<id>`), `baseline_anchor_at` and the reconcile status of its latest run. The first
source whose full run finishes becomes the baseline and its finish time the anchor; a later run of that
source never moves it, only marks the baseline to compute again (`baseline_dirty`) with its new status.
The recent pass of an in-app import is not a full run. Undo of that import clears all three, and the
next full run to finish becomes the baseline. A failure to mark is logged and the next run marks again:
the import itself is already committed. Ticket stats are frozen from the anchor
(`docs/internals/freshdesk-import.md`), so the baseline cannot move with Freshdesk work done after the
move.
