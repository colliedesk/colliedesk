# Workspace milestones

`workspace_events` is an append-only log of the milestones in a workspace's life. The server writes
each milestone in the same transaction as the fact it records (`server/internal/milestones`), so a fact
and its milestone commit or roll back together. The view `workspace_milestones` turns the log into one
row per workspace. Reports read the view; they do not recompute milestones from raw tables.
Migration `20261114180059-workspace-milestones`.

## Why it is stored once

Recomputing "did this workspace do anything" from raw tables each time gave different answers. One
query counted an auto-created email channel as a customer action. Another filtered on
`trial_ends_at > now()` and lost every trial once it ended. A milestone is now decided once, when it
happens, by one rule in one place. A later change to the raw rows does not move it.

## Kinds

| Kind | Written when | `at` | Left out |
|---|---|---|---|
| `signed_up` | A workspace is created (cloud sign-up, `create-workspace`): `auth.insertWorkspaceTx` | `workspaces.created_at` | |
| `first_customer_message` | A channel stores an incoming message (`store` channel ingest) | the message's `created_at` | imported history (`origin` set); messages without a channel (problem reports); mail refused by the sender check (`authentication` = `fail`); Gmail's forwarding confirmation (`forwarding-noreply@google.com`); mail from one of the workspace's own agents, which is how the test message (`POST /api/v1/channels/email/test-message`) arrives; a message that arrives in a conversation already marked spam (see Traps for the backfill) |
| `import_finished` | A full import run finishes with status `OK`. The in-app job writes it in the transaction that ends the job as finished (`importjobs` worker, `results.RecordImportFinished`); the command line, Freshdesk and Help Scout, writes it right after the run and returns the error if that fails | the run's finish time | `FAILED`, `UNVERIFIED`, recent-tickets slices, dry runs |
| `first_reply` | A reply is stored and its delivery is queued (`store.createMessage`), or a failed delivery is retried and queued (`store.retryDelivery`) | when the reply was queued to leave: the outbox row's creation, which a retry sets again | notes; imported replies; the channel's own notices (`notice_kind`); messages with neither an author nor an agent token; replies in a conversation whose contact is one of the workspace's agents (the answer to a test message); a reply with no delivery row, or only a failed one (channel off, removed or not connected, customer without an address), until a retry queues it |
| `teammate_invited` | An invite is created for someone who is not a member yet (`auth.CreateInvite`) | the invite's `created_at` | an invite to an existing member (it changes their roles) |
| `trial_ended` | The retention pass sees a trial that ended at least an hour ago and proves nothing paid or complimentary was in force before the end (`retention.recordEndedTrials`) | `trial_ends_at` | a workspace where that cannot be proven |
| `paid` | An applied Polar event makes a subscription `active` (`store.ApplySubscription`: webhook and reconcile) | the subscription's `modified_at` | `trialing`, `past_due` |
| `cancelled` | An applied event sets a subscription to end (`cancel_at_period_end`) or ends it (`canceled`) | the subscription's `modified_at` | the later end of a subscription already set to end |

Every kind but `paid` and `cancelled` is recorded once per workspace. A partial unique index and
`INSERT ... ON CONFLICT DO NOTHING` keep the first occurrence. `paid` and `cancelled` repeat; the view
shows the latest of each.

`detail` holds ids and counts only (message, conversation, channel, invite, subscription ids, the
importer, the plan). It never holds a name, an address or message text. `signed_up` also holds
`{"internal": true|false}` for a cloud sign-up: whether the server counted it as the team's own
(`internal_signup.emails` / `internal_signup.networks`, the same decision that marks `trial_started`).
A report can leave internal workspaces out after the workspace and its trial record are deleted.
`create-workspace` and the backfill write no `internal` key: the command line has no such list, and the
list is server configuration that the database does not hold, so an absent key means unknown.

## Traps

- **Churn.** A workspace is churned when `cancelled_at > paid_at`. Both are the latest event of their
  kind across the workspace's subscriptions, and `paid` and `cancelled` repeat per subscription change
  (a resume or a second subscription adds a `paid` after a `cancelled`). A workspace whose `paid_at` is
  later than its `cancelled_at` is paying again.
- **A trial ends at read time.** `domain.NewAccess` decides the lock when a workspace is read; nothing
  writes it. The retention pass writes `trial_ended`: every hour and at start of `serve`, only where
  billing is on (`serve` starts it only when billing is on, and logs "workspace retention is not running"
  when its setup fails; a self-hosted install never records it). `at` is the exact `trial_ends_at`. The pass
  decides a trial only once it ended at least an hour ago, to let a payment or a reconcile that was on its way
  arrive. It writes only when it is **proven** that nothing paid or complimentary was in force before the end,
  and writes nothing when unsure: a missing milestone is found by the operator's reconcile, a false one is
  permanent. Proven means: no `paid` event dated before the end; no complimentary plan granted before it; and
  either no stored subscription, or the first billing event the server ever stored for the workspace is a
  `subscription.created` that arrived after the end. A stored subscription has no start time
  (`source_modified_at` is its last change), and a reconcile can store one that began before the end, so
  any subscription without that proof leaves its start unknown. A workspace that locked and paid later still
  gets `trial_ended`. The pass stops writing for a workspace once it has a row. It records nothing while the
  server is down or billing is off. The migration does not backfill `trial_ended`: the database does not
  know whether billing is on. The first retention pass after the upgrade records every trial that already
  ended and is proven.
- **A test message is told apart by its sender.** The product writes it from the signed-in agent's own
  address. Any mail from an address of one of the workspace's agents is left out for the same reason.
  The repo uses the same rule for the trial's first-contact exception (`contactIsProvenAgent`).
- **The help-center chat counts self-tests.** The chat is anonymous: a visitor types an email address and
  the server holds no session or agent identity for it, and a typed address proves nothing. A team member
  who tries the chat of their own workspace therefore records `first_customer_message` (the hand-off
  creates the conversation). Only the email channel's test message is recognised.
- **Spam in the backfill.** The server leaves out a message that arrives in a conversation already marked
  spam (the status at arrival). The stored rows do not say what the status was then, and a conversation's
  status now can be a later decision, so the backfill does not exclude spam at all. A workspace whose first
  inbound message was spam can therefore differ between the backfill and the live rule.
- **A refused milestone never changes an import.** For an in-app import the milestone is written in the
  same transaction as the end of the job. If that write is refused, the whole end rolls back: the job is
  not failed, its report is not lost, and it stays claimed until its lease runs out; the next run runs the
  full pass again (the ledger skips what is done, so it is quick; the trial extension and the links are
  written by every run) and ends the job, writing the milestone with it. A refusal that never goes away
  keeps the job from ending until the crash guard gives it up, which says so in the job's error. The
  command line is operator-facing: it returns the error after the run and the operator runs it again.
- **Import time in the backfill.** An in-app import job keeps its own finish time: the backfill uses the
  first job that finished `OK`. An import run from the command line keeps no run record; only the results
  baseline's anchor, the finish of the first full run of that source, survives. The backfill uses it only
  when no OK job exists and the baseline's last run is `OK`, and marks the row `"approximate": true`:
  when an earlier run failed, the true first OK finish is later than the anchor and not stored.
- **Billing in the backfill.** The log of each transition is not stored, only the last state of each
  subscription and the webhook ids. The backfill writes at most one `paid` and one `cancelled` per
  workspace. `paid` comes from a subscription whose status is `active` now, or that has a
  `subscription.active` webhook behind it (a webhook names no subscription, so it counts only for a
  workspace with a single stored subscription, and never for one still `incomplete`). `past_due`,
  `unpaid`, `paused` and `trialing` alone are no payment. A subscription made active by the reconcile (no
  webhook) counts at the time it last changed, and a revoke counts as a cancel. A cancel followed by a
  resume is not replayed.
- **Self-host works the same.** Billing kinds and `trial_ended` never occur where billing is off.
- **Deploy order.** The cloud runs `collie migrate && collie serve` in one container, and a compose
  update replaces the container, so the old binary does not serve while the migration runs. A rolling
  setup that keeps the old binary serving would still work: the old code never writes `workspace_events`,
  and the milestones it misses are only those of that window.

## Erasure

The log holds ids, counts and times only: no name, address, message text or file. That is deliberate:
a deleted workspace must keep its history for reports, and an id alone does not identify a person.
If erasing a workspace's rows is legally required, an operator does it on purpose, as the table owner:

```sql
BEGIN;
ALTER TABLE workspace_events DISABLE TRIGGER workspace_events_append_only;
DELETE FROM workspace_events WHERE workspace_id = '<workspace id>';
ALTER TABLE workspace_events ENABLE TRIGGER workspace_events_append_only;
COMMIT;
```

The trigger is back on before the commit, so no other session sees it off. Nothing in the server does
this: a bug or a request must not be able to.

## Append-only

A trigger refuses `UPDATE`, `DELETE` and `TRUNCATE` on `workspace_events` for every role, the owner
included. The app role holds only `SELECT` and `INSERT`. The table has no foreign key to `workspaces`:
retention deletion removes the workspace and keeps its log and its view row. `restore-workspace.sh`
neither empties nor copies the log, because the live log already holds every milestone, also those
after the backup. A milestone is a fact that happened. Undoing an import or deleting a message does not
undo it.

## Backfill

The migration writes the log for existing workspaces with `source = 'backfill'`, using the server's
rules where the stored rows allow it; the differences are listed in Traps (spam, import time, billing).
`paid` and `cancelled` carry an empty `detail`. The down migration drops the view, the table and the
trigger function.

## The view is a contract

`workspace_milestones` has these columns, by name, all `timestamptz` and nullable: `workspace_id`,
`signed_up_at`, `first_customer_message_at`, `import_finished_at`, `first_reply_at`,
`teammate_invited_at`, `trial_ended_at`, `paid_at`, `cancelled_at`. Another program reads them; do not
rename or drop one. The view runs with the reader's rights (`security_invoker`), so a workspace's
transaction sees its own row only. `TestWorkspaceMilestonesViewHasItsFixedColumnsAndOneRowPerWorkspace`
pins the columns.
