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
| `first_customer_message` | A channel stores an incoming message (`store` channel ingest) | the message's `created_at` | imported history (`origin` set); messages without a channel (problem reports); mail refused by the sender check (`authentication` = `fail`); Gmail's forwarding confirmation (`forwarding-noreply@google.com`); mail from one of the workspace's own agents, which is how the test message (`POST /api/v1/channels/email/test-message`) arrives; a conversation marked spam |
| `import_finished` | A full import run finishes with status `OK` (`results.MarkImportFinished`: in-app jobs and the CLI, Freshdesk and Help Scout) | the run's finish time | `FAILED`, `UNVERIFIED`, recent-tickets slices, dry runs |
| `first_reply` | A reply is stored (`store.createMessage`) | the message's `created_at` | notes; imported replies; the channel's own notices (`notice_kind`); messages with neither an author nor an agent token; replies in a conversation whose contact is one of the workspace's agents (the answer to a test message) |
| `teammate_invited` | An invite is created for someone who is not a member yet (`auth.CreateInvite`) | the invite's `created_at` | an invite to an existing member (it changes their roles) |
| `trial_ended` | The retention pass sees a trial that ran out with nothing paying or complimentary in force (`retention.recordEndedTrials`) | `trial_ends_at` | a workspace that paid before its trial ended |
| `paid` | An applied Polar event makes a subscription `active` (`store.ApplySubscription`: webhook and reconcile) | the subscription's `modified_at` | `trialing`, `past_due` |
| `cancelled` | An applied event sets a subscription to end (`cancel_at_period_end`) or ends it (`canceled`) | the subscription's `modified_at` | the later end of a subscription already set to end |

Every kind but `paid` and `cancelled` is recorded once per workspace. A partial unique index and
`INSERT ... ON CONFLICT DO NOTHING` keep the first occurrence. `paid` and `cancelled` repeat; the view
shows the latest of each.

`detail` holds ids and counts only (message, conversation, channel, invite, subscription ids, the
importer, the plan). It never holds a name, an address or message text.

## Traps

- **A trial ends at read time.** `domain.NewAccess` decides the lock when a workspace is read; nothing
  writes it. The retention pass is the first writer to see it, so `trial_ended` is written there. That
  pass runs only where billing is on, every hour and at start. `at` is still the exact trial end.
  The migration does not backfill `trial_ended`: the database does not know whether billing is on,
  and a self-hosted trial never locks. The first retention pass after the upgrade records every trial
  that already ended.
- **A test message is told apart by its sender.** The product writes it from the signed-in agent's own
  address. Any mail from an address of one of the workspace's agents is left out for the same reason.
  The repo uses the same rule for the trial's first-contact exception (`contactIsProvenAgent`).
- **Self-host works the same.** Billing kinds and `trial_ended` never occur where billing is off.

## Append-only

A trigger refuses `UPDATE`, `DELETE` and `TRUNCATE` on `workspace_events` for every role, the owner
included. The app role holds only `SELECT` and `INSERT`. The table has no foreign key to `workspaces`:
retention deletion removes the workspace and keeps its log and its view row. `restore-workspace.sh`
neither empties nor copies the log, because the live log already holds every milestone, also those
after the backup. A milestone is a fact that happened. Undoing an import or deleting a message does not
undo it.

## Backfill

The migration writes the log for existing workspaces with `source = 'backfill'`, using the same rules
as the server. `paid` and `cancelled` come from the stored webhook ids (`subscription.active`,
`subscription.canceled`) at the time each arrived, with an empty `detail`. The down migration drops the
view, the table and the trigger function.

## The view is a contract

`workspace_milestones` has these columns, by name, all `timestamptz` and nullable: `workspace_id`,
`signed_up_at`, `first_customer_message_at`, `import_finished_at`, `first_reply_at`,
`teammate_invited_at`, `trial_ended_at`, `paid_at`, `cancelled_at`. Another program reads them; do not
rename or drop one. The view runs with the reader's rights (`security_invoker`), so a workspace's
transaction sees its own row only. `TestWorkspaceMilestonesViewHasItsFixedColumnsAndOneRowPerWorkspace`
pins the columns.
