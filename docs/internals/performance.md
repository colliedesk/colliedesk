# Performance budgets

Colliedesk targets a usable screen within 200 ms from a user action. The target includes network, server, data, and cached assets. The repository gates the parts it can measure deterministically.

## Inbox server budget

`TestInboxListLatencyBudget` runs against real PostgreSQL. It creates two isolated workspaces with 5,000 conversations each. Every conversation has messages and tags. The test calls the authenticated inbox endpoint 50 times with a status filter and 50 times with a search query, using the maximum page size. Each path must stay at or below 100 ms p95.

## Inbox activity budget

`TestInboxActivityLatencyBudget` holds the query behind `GET /api/v1/inbox/activity` to 20 ms p95. Every open tab polls it every 20 seconds. The fixture is a workspace with 5,000 conversations spread over a week, a tenth assigned to the caller by a teammate, and a second workspace of the same size. The cursor is 30 minutes back, so the result is a realistic handful of rows, never the 50-row cap. The test does 10 warm-up calls, then 200 timed calls.

Method: it times the store call (`InboxActivity`: transaction, row-level security setup and the batched queries), not the HTTP request. The HTTP and session hops are the same for every endpoint and are covered by the inbox list budget; timing them here would let a noisy runner fail a budget this query does not own. Do not widen the 20 ms.

Keep the query ordered by `created_at` or `assigned_at` only: a tie-break on `id` forces a sort of every row after the cursor.

The test runs in the normal Go suite and in CI. It is not behind a build tag. Set `TEST_DATABASE_URL` to run it locally.

If the gate fails:

1. Re-run the focused test on an otherwise idle host.
2. Inspect the query with `EXPLAIN (ANALYZE, BUFFERS)` using the same tenant-sized fixture.
3. Check for N+1 queries, tenant-unscoped scans, missing filter/order indexes, and repeated search scans.
4. Optimize the query or infrastructure. Do not raise the 100 ms threshold.

## Bulk conversation update budget

`TestBulkUpdateOfFiftyConversationsCostsNoMoreStatementsThanOneAndStaysInBudget` runs against real PostgreSQL. It updates 50 conversations (status, assignee and a tag) through `PATCH /api/v1/conversations`. Two gates:

1. The number of SQL statements for 50 ids must equal the number for 1 id. This gate is deterministic: the cost never grows with the ids, so the API never runs one statement per conversation.
2. The best of five runs for 50 ids must take at most 100 ms. The best of five keeps one scheduling stall on a shared host from failing the gate.

If the gate fails, look for a statement inside a loop over the ids. Do not raise the 100 ms threshold.

The bulk update locks the conversation rows first (`ORDER BY id`), then touches `conversation_tags`. The single-conversation tag endpoints use the same order, so the two never wait for each other. `TestBulkAndSingleTagAddsOnOneConversationDoNotDeadlock` pins this.

## Web bundle budget

`corepack pnpm bundle:check` reads the production Vite output. The largest JavaScript asset must be at most 200 KiB gzip. The largest CSS asset must be at most 30 KiB gzip. `make test` and the web CI job build the bundle before running this gate.

If the gate fails, inspect the generated assets and remove or lazy-load the dependency that caused the increase. Do not compare raw file size with the gzip threshold, and do not loosen the limits to pass CI.
