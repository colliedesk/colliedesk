# Performance budgets

Colliedesk targets a usable screen within 200 ms from a user action. The target includes network, server, data, and cached assets. The repository gates the parts it can measure deterministically.

## Inbox server budget

`TestInboxListLatencyBudget` runs against real PostgreSQL. It creates two isolated workspaces with 5,000 conversations each. Every conversation has messages and tags. The test calls the authenticated inbox endpoint 50 times with a status filter and 50 times with a search query, using the maximum page size. Each path must stay at or below 100 ms p95.

## Contact page budget

`TestContactPageLatencyBudget` reuses the inbox fixture (one contact owns all 5,000 conversations of
its workspace, with messages and tags) and adds 10,000 filler contacts per workspace. Three calls must
each stay at or below 100 ms p95: `GET /api/v1/contacts/{id}` (contact, count, merges),
`GET /api/v1/conversations?contact={id}&page_size=50` (the history) and a contact search with
`limit=8` (the picker and the inbox's customer matches). Measured on the dev host: 4 ms, 9 ms, 10 ms.
The history uses `conversations(workspace_id, contact_id, updated_at DESC, id DESC)`. The contact detail
no longer embeds every conversation: the unpaged list cost one query per conversation and would not
meet the budget for a customer with years of imported tickets. Do not widen the 100 ms.

## Inbox activity budget

`TestInboxActivityLatencyBudget` holds the query behind `GET /api/v1/inbox/activity` to 20 ms p95. Every open tab polls it every 20 seconds. The fixture is a workspace with 5,000 conversations, one every 20 seconds (about a day), a tenth assigned to the caller by a teammate, two messages on each (every third ends with a customer message), and a second workspace of the same size. The cursor is 30 minutes back; the server treats any cursor older than 10 minutes as 10 minutes old, so the window is 12 minutes with the lookback and the result is a few dozen rows, never the 50-row cap. The test does 10 warm-up calls, then 200 timed calls.

Method: it times the store call (`InboxActivity`: transaction, row-level security setup and the batched queries), not the HTTP request. The HTTP and session hops are the same for every endpoint and are covered by the inbox list budget; timing them here would let a noisy runner fail a budget this query does not own. Do not widen the 20 ms.

Keep each query ordered by `created_at`, `assigned_at` or `updated_at` only: a tie-break on `id` forces a sort of every row after the cursor.

The test runs in the normal Go suite and in CI. It is not behind a build tag. Set `TEST_DATABASE_URL` to run it locally.

If the gate fails:

1. Re-run the focused test on an otherwise idle host.
2. Inspect the query with `EXPLAIN (ANALYZE, BUFFERS)` using the same tenant-sized fixture.
3. Check for N+1 queries, tenant-unscoped scans, missing filter/order indexes, and repeated search scans.
4. Optimize the query or infrastructure. Do not raise the 100 ms threshold.

## Pending agent requests budget

`TestPendingAgentRequestsLatencyBudget` holds the store query behind
`GET /api/v1/agent-requests?status=pending` to 20 ms p95. Every open tab polls it every 20 seconds
for the Requests badge and the reply drafts in the inbox. The fixture is two workspaces with 5,000
requests each from one token, 20 pending with bodies near the 64 KiB cap and the rest decided over
the last week with 2 KiB bodies. The poll selects no body (`detail=full` does). The query first
marks overdue pending rows expired, then lists; both use the `(workspace_id, status, created_at)`
index. Measured on the dev host: 3 ms. Do not widen the 20 ms.

## Bulk conversation update budget

`TestBulkUpdateOfFiftyConversationsCostsNoMoreStatementsThanOneAndStaysInBudget` runs against real PostgreSQL. It updates 50 conversations (status, assignee and a tag) through `PATCH /api/v1/conversations`. Two gates:

1. The number of SQL statements for 50 ids must equal the number for 1 id. This gate is deterministic: the cost never grows with the ids, so the API never runs one statement per conversation.
2. The best of five runs for 50 ids must take at most 100 ms. The best of five keeps one scheduling stall on a shared host from failing the gate.

If the gate fails, look for a statement inside a loop over the ids. Do not raise the 100 ms threshold.

The bulk update locks the conversation rows first (`ORDER BY id`), then touches `conversation_tags`. The single-conversation tag endpoints use the same order, so the two never wait for each other. `TestBulkAndSingleTagAddsOnOneConversationDoNotDeadlock` pins this.

## MCP tool budget

`TestInboxSearchLatencyBudget` (package `mcpserver`) runs against real PostgreSQL with two workspaces and 100,000 contacts in all. The first workspace has 5,000 conversations of 3 messages each, spread over 50 customers; the rest of the contacts are filler. A client calls `colliedesk_inbox_search` (once with a status filter, once with a text search, 50 rows each) and `colliedesk_conversation_get` 50 times each through the real `/mcp` endpoint. Each path must stay at or below 100 ms p95, the same as the inbox gate above: the tools add a few small REST calls (identity, team, customers by id) to one list query.

The contact lookup by id is the call that depends on table size. It must compare uuid values so that the `(workspace_id, id)` index is used; comparing `id::text` scanned all 100,000 contacts and put the status page at 133 ms and the text search at 160 ms. If this gate fails, run the contact query by id with `EXPLAIN (ANALYZE, BUFFERS)` before anything else.

It runs in the normal Go suite and in CI (about 11 seconds, most of it seeding). Do not raise the threshold or shrink the fixture to pass.

## Ticket open (timeline read and request order)

A ticket opens on three independent reads: the list, the conversation detail and its messages (plus time entries). `inbox-route.test.tsx` pins that a ticket opened by its address requests messages, time entries, list and detail at once, before the list or the detail answers, and that these go out before the side lists (agents, tags, saved replies, email channel). A browser allows six HTTP/1.1 connections per host, so a request queued behind the side lists is a request the person waits for. `TestTimelineReadWithHeavyHTMLMailStaysInBudget` holds `GET .../messages` for 60 HTML messages with 30 remote images each (HTML rewritten to proxy addresses, no image host called) to 100 ms p95 (measured 38 ms).

Measured on a local stack with 304 conversations, cold page load of a ticket by address, Chrome, median of 12 runs (server answers in 3 to 5 ms): at 0 ms RTT the last conversation API response ends at 217 ms and is bound by the JavaScript boot (first API request leaves at about 175 ms, JS 197 KB gzip); with 60 ms RTT it ended at 601 ms before the change and 355 ms after. What code cannot remove: bundle download and boot before the first request, and one round trip. The infra levers that reach 200 ms for users far from the server: serve the app and API from a region near the users (a VN user to a Hetzner EU host pays about 250 ms per round trip alone), HTTP/2 or HTTP/3 and brotli at the proxy, and long-lived cache headers on hashed assets so only the first visit pays for the bundle.

Warm open (a click on a list row, synthetic data, 5 tickets including a 285 KB HTML report, Chrome, local stack, medians of 16 to 20 runs): the first open of a ticket in a page costs 118 to 146 ms and an open of a ticket seen a moment before costs 47 to 54 ms, the same at `ed2400b` (plain-text view) and at the head (formatted view: 143/50 ms and 140/52 ms by click, 140/52 ms and 118/54 ms by `history.pushState`). A report of 70 ms "warm" against 192 ms came from comparing runs with a warm cache against all runs including each ticket's first open; they are different measurements. `inbox-route.test.tsx` pins what keeps the warm open fast: a ticket opened from the list renders its header and composer from the row and makes no detail request, and an address that is in the list does not wait for a pending detail.

## Web bundle budget

`corepack pnpm bundle:check` reads the production Vite output. The largest JavaScript asset must be at most 200 KiB gzip. The largest CSS asset must be at most 30 KiB gzip. `make test` and the web CI job build the bundle before running this gate.

If the gate fails, inspect the generated assets and remove or lazy-load the dependency that caused the increase. Do not compare raw file size with the gzip threshold, and do not loosen the limits to pass CI.
