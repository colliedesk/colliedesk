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

## Counting

The rules are in `docs/user/results.md`; `internal/results/metrics.go` applies them, and its tests
work every number out by hand. Decisions behind them (agreed in the #100 design review):

| Decision | Why |
|---|---|
| One rule for both sides: customer-started conversations (first public message incoming) created in a window, measured at a moment M; only events at or before M count | Different cohorts or measuring moments make Colliedesk look faster by construction: a 7-day-old conversation cannot take 20 days to resolve |
| Resolution uses conversations created in the window ending at M − 7 days | A resolution counts only once it stayed for 7 days; younger ones are "still settling", never counted and never subtracted later |
| At most one resolution per conversation: the last one that stuck | Freshdesk's `resolved_at` is its last resolution; the same rule on our side keeps the two comparable |
| Live leaves out conversations an import created (`channel = 'imported'`) and problem reports | An imported ticket's `created_at` is from before the move; counting it would put Freshdesk's work on Colliedesk's side |
| The baseline's windows end at the newest ticket created by the anchor; it reads only the baseline source's tickets (through `import_items`) | An old history (or the e2e fixture from 2024) ends long before the import; a second account's tickets are not that team's history |
| No baseline reopen rate | Freshdesk documents no reopen history (owner agreed, scope note on #100) |
| A median or p90 needs 5 values; p90 is nearest-rank | A small team's 7 days often hold fewer; the page says "Too few (n)" instead of a number that swings |
| Resolution and reopen windows that need status history from before `status_history_since` say when they will be ready | A number built on a gap is wrong, not small |

## Snapshots and the worker

The page never computes: `results.Worker` (started by `serve`, every 15 seconds, so a finished import's
baseline shows within seconds; a look that finds nothing due is one small query) leases due
workspaces through `PrivilegeResultsClaim` (`results_state.claimed_until`, so two servers never compute
one at once) and computes each in its own workspace transaction with a 30-second statement timeout.
Due means: the baseline is dirty, never computed, or an hour old; an hour-old workspace whose inputs
(newest `conversations.updated_at`, status change, AI draft decision, and the UTC day, since
resolutions settle with time alone) did not change is only checked again in an hour. A failure is
kept in `results_state.error` for admins and retried after the lease.

`results_snapshots` holds the numbers (`data`, returned by the API as stored) and `results_members`
the conversations each number counts (`<window>.<metric>`), so every number opens its list. A current
snapshot stays 24 hours after it is replaced, so a number clicked on a page loaded before a refresh
still opens. Members have no foreign key to conversations: a baseline writes tens of thousands of
rows at once and the key tripled that write; the list joins conversations, so a deleted one never
shows.

A workspace that imported before results existed gets, on its first compute only, the import whose
first run finished first as its baseline (by its in-app job, or when its CLI ledger last changed),
with an unknown reconcile status. Later an undone baseline does not come back.

## Reading and opening a number

`GET /api/v1/results` (every role; token tier `read`; MCP `colliedesk_results_get`) returns the newest
current snapshot and the baseline as stored, plus `baseline_state` (`none`, `computing`, `ready`) and
hours saved, which is worked out on the read from the two snapshots (it needs both). The compute error
is shown to admins only. `GET /conversations?result=<snapshot id>:<window>.<metric>` keeps the
conversations a number counts (`EXISTS` on the members' primary key); it is not a saved view filter,
since a snapshot is replaced. A snapshot not kept in the caller's workspace answers 410
`results_refreshed` whether it was pruned or belongs to another workspace, so the answer says nothing
about other workspaces. Every time the API returns is UTC: the loader converts what the database
session returns.

## Share link and weekly email

| Decision | Why |
|---|---|
| The share page is `/r#<token>` and the token goes in the body of `POST /api/v1/results/shared` | A fragment never reaches a server, a log line or a Referer; a path would be logged on every 4xx (`request-id.go` logs the path) |
| Only the token's SHA-256 is stored; at most one active link per workspace (a partial unique index); creating one turns the old off | Revocable, unlike a signed link; a lost link is replaced, never recovered |
| The public answer is `results.Shared`, built field by field: windows of numbers, no snapshot id, no ledger source (it names the account), no compute error | `TestResultsShareLinkShowsTotalsOnlyAndCanBeTurnedOff` pins every key and that no workspace id, contact, subject or text appears |
| Off, replaced, unknown and (on the cloud) locked answer the same 404 | A guess learns nothing about a workspace |
| `last_viewed_at` is written at most once an hour; the route shares the public web form's per-address limiter; `no-store`, `no-referrer`, `noindex` | A public page must not make the database write without limit, nor be cached or indexed |
| Creating a link is `approvalRequired` for every token, and its answer is withheld from the token (`withheldResults`) | Data leaves sign-in: a person approves, and only that person sees the link |
| The weekly email is claimed per workspace week (`results_state.weekly_sent_week`, the Monday 00:00 of the workspace's timezone as an instant) before sending; a failure is logged, not retried | Two servers never send it twice; comparing instants (not local dates) survives a timezone change mid-week |
| A workspace seen for the first time starts with the next Monday | Otherwise every new workspace would get an email the moment the worker first saw it |
| The worker looks for a due weekly email every 5 minutes, not on every 15-second look | 8:00 is met within minutes; listing every workspace's timezone four times a minute would buy nothing |
| It is plain text with `List-Unsubscribe` pointing at the page's switch, sent from the platform address like import notices, only when SMTP and `MAIL_FROM_DOMAIN` are set | Same system mail as the rest of the server; self-hosted servers work the same |
