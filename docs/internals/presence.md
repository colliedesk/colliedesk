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
429 with `Retry-After`. A script or agent token calling in a loop therefore writes each row at most
every 2 seconds, and the cap of 20 rows per person bounds the rows a loop over new tab ids can keep.
The web app waits at least 2 seconds between two announces; the MCP tool waits out one 429 itself.

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
