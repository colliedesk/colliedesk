# Presence: who has a conversation open

Decisions and traps behind `PUT` / `DELETE /api/v1/conversations/{id}/presence` and the
`conversation_presence` table (migration `20261022093123-conversation-presence`), issue #101.

## A poll, not a push channel

The server has no push channel (no SSE, websocket or `LISTEN`/`NOTIFY`); the inbox learns of changes
by polling `GET /api/v1/inbox/activity` every 20 seconds. Presence follows the same model: a tab with
a conversation open sends `PUT .../presence` every `heartbeat_seconds` (5 while visible), and the
answer lists the others, so one request both announces and reads. SSE was rejected for now: it holds
one of the browser's six HTTP/1.1 connections per tab for good (the ticket-open order in
`performance.md` counts on them), needs a fan-out between processes, and needs proxy buffering off on
self-hosted installs.

## Stored in PostgreSQL, UNLOGGED

Rows live minutes at most and are true again a heartbeat later, so the table is `UNLOGGED`: no WAL,
emptied by a crash, refilled within one heartbeat. It is still in PostgreSQL rather than in memory:
during a deploy two processes run side by side and must see the same tabs; row-level security gives
the same workspace isolation as every other table; and the inbox list markers read it in the same
transaction as the activity poll. An in-memory map would be one more feature tied to a single
process, next to the export limiter (`billing.md`).

Every statement filters on `workspace_id` and passes the server clock (`Store.now`), never SQL
`now()`, so the expiry follows the same clock as the tests' `testClock`.

## Rows and keys

One row per tab: the key is `(workspace_id, session_id, client_id)`. `client_id` is random per page
load in the browser (not `sessionStorage`: "Duplicate tab" copies it, and closing one tab would remove
the other). An agent token uses the fixed `mcp-agent`; its session tells tokens apart. A person with
several tabs is listed once; an agent token is listed apart, as the person "via" the token's label.

The caller's own tabs are never listed back to it. An agent token does see the tabs of the person it
acts as: the AI must know that its person is writing a reply.

`writing_since` / `edited_at` are set while the reply box (not a private note) holds text not sent
yet. `writing_since` is when the box stopped being empty; `edited_at` moves when the client says the
text changed (`edited`). A reply untouched for minutes is a draft left open in a background tab, not
someone typing. The text itself never leaves the browser.

## Expiry and leaving

| Row | Lives without a heartbeat |
| --- | --- |
| Viewing | 15 s (three heartbeats) |
| Writing (reply not sent yet) | 150 s: browsers run timers of background tabs about once a minute |
| Agent token | 2 minutes: an AI agent works in steps, not on a timer |

Expiry is a safety net. A tab leaves at once with `DELETE .../presence/{clientId}` sent with
`fetch` `keepalive` on `pagehide`, and when it moves to another conversation the same row moves with
it. A `DELETE` removes only the caller's own row (same session), and only while the row is still on
that conversation, so a late leave from the old conversation cannot remove the new one. Each announce
removes the workspace's expired rows and the person's oldest rows past 20.

One tab announces one conversation at most every 2 seconds: the upsert only updates a row on another
conversation or one renewed at least 2 seconds ago, and an announce that updates nothing is answered
429 with `Retry-After`. Starting or ending a reply is let through after half a second instead: the
others must see it at once, not at the next heartbeat. A script or agent token calling in a loop
therefore writes each row at most twice a second, and the cap of 20 rows per person bounds the rows a
loop over new tab ids can keep. The MCP tool waits out one 429 itself; the web app spaces its
announces and calls again after `Retry-After`.

A deactivated person's rows are deleted in the deactivation transaction; reads also skip deactivated
people and revoked or expired sessions, so a signed-out tab disappears before its row expires.

## Traps

1. **The conversation foreign key locks.** Inserting a presence row takes `FOR KEY SHARE` on the
   conversation row through `(workspace_id, conversation_id)`. A writer that locks conversations must
   use `FOR NO KEY UPDATE` (as the `last_public` trigger and the bulk update do), never `FOR UPDATE`,
   or every open tab's heartbeat waits on it.
2. **Presence is not data.** It is not exported (`docs/user/export.md`), and a workspace restore copies
   whatever rows the backup held; they expire within minutes.
3. **The read tier.** `PUT` and `DELETE` change nothing anyone wrote, so a read token may announce
   itself. Do not give presence a side effect that does.

## Warning before sending

Every reply (an `outgoing` message) gets the next `messages.reply_number` of its conversation, 1, 2,
3... (migration `20261022100554-reply-numbers`); the newest is `latest_reply_number` of the
conversation in the API, read as `max(reply_number)` through the unique index. A reply sent with
`seen_reply_number` below it answers 409 `reply_conflict` with the newer replies; nothing is stored. The
web app takes the mark when the reply box stops being empty (the newest reply on screen then), so a
teammate's reply that arrived on screen while you wrote is still asked about once; **Send anyway** and
**Edit reply** both move the mark to the latest number. The MCP tool `colliedesk_reply_send` requires
the mark (`latest_reply_number` of `colliedesk_conversation_get`); the REST API takes it as optional so
older clients keep working. Approving a reply draft checks the approver's mark before the draft is
claimed, so a refusal leaves it waiting; the replay checks again under the lock, and a reply in between
(rare) fails the draft with 409.

Numbers, not times. `created_at` is taken when a transaction starts and the row is visible when it
commits, so "replies after the time I started" misses a reply that committed late; an import also
writes old times. Two replies at once with the same mark never both go
(`TestTwoRepliesSentAtOnceWithTheSameMarkOnlyOneGoes`).

The migration numbers the existing replies in one pass: 3.4 s on the dev host for 50,000
conversations and 400,000 messages (200,000 replies); it locks `messages` while it runs.

Traps:

1. **The number comes from a trigger, under an advisory lock.** `messages_number_reply` (row-level,
   `BEFORE INSERT`) takes the transaction-scoped advisory lock `reply_number:<workspace>:<conversation>`
   and gives `max + 1`, so every writer gets numbers without knowing about them, importers and older
   images included. `CreateReply` takes the same lock (`store.lockReplyNumbers`, the same key) before it
   checks the mark and keeps it to the insert. Two designs were dropped: a row-level trigger that
   updated a counter on the conversation row locked conversations in insert order and deadlocked with a
   contact merge (`TestAMergeAndMailOnTheSameConversationsNeverDeadlock`); a statement-level trigger
   that numbered the rows after the insert had to update them again, which fired the `last_public`
   update trigger over a transition table of every row and ran for minutes on a 100,000-row insert
   (`TestTrialCapCountStaysFastOnALargeOutbox`).
2. **Lock order.** A writer of a reply takes the numbering lock first and conversation row locks after
   (the `last_public` trigger at the end of the insert, then `updated_at`). Keep it so: taking a
   conversation row lock and then inserting a reply in the same transaction can deadlock with one that
   holds the numbering lock. Importers write each message in a transaction of its own
   (`TestAnImportedReplyAndAPersonsReplyAtOnceAreNumberedInTurn`).
3. **One lock per conversation.** An insert of replies into many conversations at once takes one
   advisory lock per conversation until commit, from the shared lock table (`max_locks_per_transaction`).
   Write many conversations' replies in batches, as the importers do.
4. **A re-run import numbers old replies last.** A Freshdesk reply imported into a conversation that
   already has native replies gets the next number though its time is older. The conflict lists newer
   replies by `created_at` with their `origin`, so nobody reads it as just written.
5. **A restore copies the numbers.** `restore-workspace.sh` copies with triggers off
   (`session_replication_role = replica`), so the numbers come from the backup and the next reply
   continues after the backup's last one.
