# Performance budgets

Colliedesk targets a usable screen within 200 ms from a user action. The target includes network, server, data, and cached assets. The repository gates the parts it can measure deterministically.

## Inbox server budget

`TestInboxListLatencyBudget` runs against real PostgreSQL. It creates two isolated workspaces with 5,000 conversations each. Every conversation has messages and tags. The test calls the authenticated inbox endpoint 50 times with a status filter and 50 times with a search query, using the maximum page size. Each path must stay at or below 100 ms p95. The same test then runs the newer filters and sorts on the same data under the same budget: who replies next with the waiting-longest sort, three tags with `tag_match=all`, a search on the contact name, a search that matches nothing, the priority sort, several statuses and priorities, a waiting draft, a failed delivery, channel with source and assignee, and a date range with an organization (`docs/internals/inbox-filters.md`).

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

## Import status budget

`TestImportStatusLatencyBudget` holds the store calls behind `GET /api/v1/imports/{id}` to 20 ms p95
and `GET /api/v1/imports` to 100 ms p95. The import screen polls the status every few seconds while an
import runs for hours. The fixture is two workspaces with 300 finished imports each, every preview
naming 50 agents, and the newest import with a 4 MB report: the status and the list never read a
report (`/report` does), so its size must not show. Same method as the inbox activity budget: 10
warm-up calls, then 200 timed calls of the store call. No handler talks to Freshdesk; the worker does.
Do not widen either budget.

## Knowledge base search budget

`TestKnowledgeBaseSearchLatencyBudget` seeds two workspaces at the article cap (`domain.MaxKBArticles`,
2,000 articles of about 9,000 characters from a vocabulary of 20,000 words, 20 of them at the
100,000-character limit) and calls the article list 50 times per path at the largest page: a two-word
search, a short prefix, a search that matches nothing, a title search, the published articles, and
the collections. Each path must stay at or below 100 ms p95 (about 11 ms locally). A search reads every
vector of the workspace, because row-level security keeps Postgres off the GIN index; raise the cap
only with this gate, after reading `docs/internals/knowledge-base.md`.

`TestKnowledgeBaseSearchVectorCostsLinearTime` writes and rewrites one article of 100,000 characters
per run that made the `simple` parser quadratic (`_a`, `~/`, `a1@`, `a-1-`, `a-a.` and others). Each
write must stay at or below 200 ms (10–25 ms locally; unnormalized, seconds to tens of seconds). A
3-second statement timeout makes a regression fail instead of hanging the run.

## Media library budget

`TestKBMediaLibraryLatencyBudget` reads the knowledge base search budget's fixture, to which
`seedKBMediaBudget` adds 10,000 library items (`domain.MaxKBMedia`) in the first workspace, spread
evenly between the rows of 20 other workspaces of 2,000 items each (as interleaved uploads leave a
workspace's rows, one in five on a page), and five links on each of its 2,000 articles (three
current). It first calls the unused page with the visibility map as stale as right after a large
import, which reads a heap page per row, and holds it to the 100 ms list budget (11–14 ms locally on
2026-10-20); then it runs `VACUUM ANALYZE`, the steady state autovacuum keeps, and holds the first page,
a search, the unused items largest first with their summary, the items in use, and the items missing
alt text sorted by name to 25 ms p95 (4–12 ms locally). Each path is called 20 times, not 50, since the
gate shares its CI runner with every other latency gate. The design asked for 10,000 items in each
other workspace too; the measured workspace stays at the cap, but the others hold 2,000 because 10,000
took 4 s more of seeding on the latency runner, against the 120-second CI budget.

## Help center pages budget

`TestHelpCenterPagesLatencyBudget` reads the knowledge base search budget's fixture, seeded once per
test binary for both gates (`sharedKBBudget`; seeding twice cost 33 s of the 120-second CI budget):
half of the articles published, all in one collection, help centers on. It loads each public
page 50 times, HTML rendering included: the home page, the first and last page of the collection
(1,000 published articles, 50 a page), a 9,000- and a 100,000-character article, and searches with two
words, a short prefix, no match and a title. Each must stay at or below 100 ms p95 (6–16 ms locally).
A page is five short scoped transactions at most (slug lookup, settings and lock, collections,
articles, neighbours); a 304 stops after the second.

`TestHelpCenterHostileArticlesLatencyBudget` holds an article page to the same 100 ms when its
100,000 characters are built to be slow to parse: one line of quote or list markers (`>`, `> `,
`1. `, `+ `, mixed), 1,500 quoted players, same-named ATX and setext headings, open links and
images, and emphasis runs (goldmark's quadratic paths: up to 24 s before `tame`, `headingIDs` and
`linkify`). Each article's first visit renders it cold and is held to the budget (6–35 ms locally);
then 20 visits (not 50, to spare the shared latency runner) read the kept page (4–12 ms).
`TestHelpCenterRenderedPagesAreNeverSharedAcrossWorkspaces` keeps that memory per workspace.

## Pending agent requests budget

`TestPendingAgentRequestsLatencyBudget` holds the store query behind
`GET /api/v1/agent-requests?status=pending` to 20 ms p95. Every open tab polls it every 20 seconds
for the Requests badge and the reply drafts in the inbox. The fixture is two workspaces with 5,000
requests each from one token, 20 pending with bodies near the 64 KiB cap and the rest decided over
the last week with 2 KiB bodies. The poll selects no body (`detail=full` does). The query first
marks overdue pending rows expired, then lists; both use the `(workspace_id, status, created_at)`
index. Measured on the dev host: 3 ms. Do not widen the 20 ms.

## Conversation presence budget

`TestConversationPresenceLatencyBudget` holds the store call behind
`PUT /api/v1/conversations/{id}/presence` to 20 ms p95. Every tab with a conversation open sends it
every 5 seconds. One transaction removes the workspace's expired rows, upserts the tab's row, trims
the person to 20 rows and reads who else has the conversation open. The fixture is three workspaces
of ten people on 2,000 conversations, each person with three tabs and an agent token: 40 live rows in
the timed workspace, 16 on the timed conversation, and 40 expired rows in each other workspace. Every
statement is bounded by the workspace (`workspace_id` leads every index of `conversation_presence`).
Measured on the dev host: 2 ms. Do not widen the 20 ms.

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

On 2026-10-20 the first bundle was 203,268 bytes of 204,800. Plan and billing, the locked screen, the trial survey and their strings (and those of Report a problem) moved to their own chunks, which brought it to 200,433; the knowledge base then added only its rail link and routes (201,420). Its pages with the preview and `remark-gfm`, and the help center card in Settings are lazy parts; the rail link preloads them on hover and focus. The article editor (CodeMirror 6) is a chunk of its own, 117,572 bytes gzip plus 1,502 bytes of CSS, loaded only by an article page; `kb-chunk` is 20,890 bytes. The first bundle went from 201,430 to 201,440 bytes (+10, the names of React hooks the chunks now import) and its CSS from 14,267 to 14,263.

On 2026-10-21 the reply box got **Articles** (link a help center article in a reply). Its first version kept the button, its strings, the link format and the extended saved reply picker in the first bundle: 201,985 → 202,563 (+578). The saved reply picker and the articles picker then moved together into one lazy part, `reply-pickers` (1.7 KB gzip, plus a 4.0 KB chunk of the API calls and strings it shares with the knowledge base pages), which the composer starts loading when it mounts; the first bundle keeps the two buttons and the part's loading and failure states: 201,776 bytes (−209 against 201,985), CSS 14,267 → 14,317.

On 2026-10-22 the in-app Freshdesk import (#97) brought the first bundle to 203,852 bytes, and collision detection (#101) needed about 1,100 more. Settings (its route and every section: team, email channel, saved replies, web form, help center, preferences) moved to a lazy part, `settings-route` (7,988 bytes gzip), as Plan and billing already was: the first bundle went from 203,852 to 196,712. The rail link starts loading the chunk on hover and focus; while it loads, `/settings` keeps an empty Settings page (`aria-busy`), so nothing jumps when the sections arrive. Measured with Playwright on the dev host against the e2e server: from a click on the rail link to the Settings heading 29 to 88 ms (5 runs), a full load of `/settings` 64 to 86 ms (3 runs).

If the gate fails, inspect the generated assets and remove or lazy-load the dependency that caused the increase. Do not compare raw file size with the gzip threshold, and do not loosen the limits to pass CI.

## Reply box lists (saved replies, articles)

Measured on 2026-10-21 against a local `collie serve`, Chromium, a new browser context with the cache disabled for each run, median of 5: open a conversation by its address, wait until the reply box is enabled, then a programmatic click, timed with `performance.now` in the page until the first option shows. The list's box (loading state or list) shows in the same frame as the click in every run (2–10 ms), so nothing moves when the list arrives.

| From the click to the first option | 1×, 0 ms RTT | 4×, 0 ms RTT | 1×, 150 ms RTT | 4×, 150 ms RTT |
| --- | --- | --- | --- | --- |
| Insert saved reply | 2 ms | 7 ms | 285 ms | 261 ms |
| Articles | 8 ms | 21 ms | 443 ms | 408 ms |

At 150 ms RTT the `reply-pickers` chunk was already there (its search box showed 2–11 ms after the click); the wait is data. Saved replies are fetched when the inbox opens, and were still in flight: before the lazy part the same measurement gave 239 ms at 4×, so the chunk's own requests cost about 20 ms there. Articles asks the server for the help center and the published articles, side by side, each time it opens (one round trip, then the search). Over 200 ms at that distance is the round trip, which the infra levers under Ticket open address.

On 2026-10-21 the knowledge base pages were redesigned (one head row, actions in fixed bars, the media panel in tabs, the upload tray, the library picker as a dialog). The first bundle went from 202,775 to 202,773 bytes (the rail logo became a link, `useReturnFocus` takes a flag, `dialogKeys` skips hidden controls) and its CSS from 14,385 to 14,388. The lazy `kb-chunk` grew from 37,905 to 47,238 bytes gzip and its CSS from 3,662 to 7,780; the editor chunk from 120,029 to 121,259. Measured the same way as below (local `collie serve`, Chromium, 1×, 1,440 px, median of 5, click to the first frame with content): rail → article list 51 ms, list → editor 35 ms, Articles → Media 30 ms.

## Knowledge base editor open and typing

Measured on 2026-10-20 against a local `collie serve` (PostgreSQL on the same host), Chromium, median of 5 runs, from a programmatic click (no hover preload) to the first frame with the content (`performance.now` in the page):

| From the click | 1×, 1,440 px (Side by side) | 4×, 1,440 px | 1×, 1,024 px (Write) | 4×, 1,024 px |
| --- | --- | --- | --- | --- |
| Rail → article list | 56 ms | 63 ms | 52 ms | 55 ms |
| List → editor, 30 characters | 44 ms | 43 ms | 34 ms | 42 ms |
| List → editor, 10,000 characters | 44 ms | 52 ms | 34 ms | 54 ms |
| List → editor, 99,600 characters | 40 ms | 54 ms | 38 ms | 61 ms |
| … and its preview drawn (Side by side) | 145 ms | 435 ms | – | – |

Those articles are flat text. On a structured one (sections of a heading, a paragraph with a link, a numbered and a bulleted list), measured from the click to the second frame after the editor appears, 1,440 px wide, median of 5: 19,500 characters, which opens side by side, 86 ms at 1× and 172 ms at 4× (the preview then draws in one task of 100 ms); 99,600 characters, which opens in Write (`splitLimit`, 20,000), 66 ms and 86 ms. The same 99,600 characters side by side took 269–302 ms at 4× before the limit and the preview's wait for the first frame (`previewReady`).

The editor never waits for the preview: it renders through `useDeferredValue` starting from nothing (without that start value, the first render drew the preview too and 99,600 characters took 359 ms at 4×). Typing in that article, from the key to the frame that shows it (Event Timing, 19 keys 80 ms apart): Write 40 ms median at 1× and 4×; Side by side 48 ms (max 72) at 1× and 64 ms (max 104) at 4×, with `contain: content` on both panes (before it, a key at 4× once took 272 ms). The preview's own render (react-markdown, 51–88 ms at 1× for 100,000 characters) runs 120 ms after typing stops, not between keys.

## CI phase budget

CI has a 120-second wall-clock budget from the workflow trigger, including queue, setup, all tests, security checks and smoke tests. The `ci-ok` job waits for CodeQL and every other check, then `.github/scripts/ci-budget.py` rejects an over-budget phase. Jobs also have native two-minute deadlines. A provider-green result alone is not acceptance evidence; inspect the completed run timestamps, including job cleanup.

The Go matrix partitions the inventory printed by `go test -list . ./...` into eight disjoint name sets: seven regular shards and one for the large latency fixtures (names containing `LatencyBudget`, `StaysInBudget` or `StaysFast`; run beside other packages' tests on the shared database, `TestStorageUsageStaysFastWithAHugeAttachmentTable` measured 389 ms against its 200 ms budget); the shard count comes from the matrix (`strategy.job-total`). Tests, examples and fuzz seed cases all run once; selecting a parent runs its subtests. `.github/scripts/go-test-shard.py` fails on an empty inventory and anchors each selected name. A regular shard runs its names as three disjoint patterns in three `go test` processes at once (`--parts 3`): its tests mostly wait on PostgreSQL one at a time per package binary, and `httpapi` took most of the shard (one shard's `httpapi` names took 23 seconds in one process and 13 in two, locally; in CI the shards' test phase went from 23–53 seconds to 26–42). The latency shard always runs one process. Against PostgreSQL's default `max_connections` of 100, shard 1 of 8 in three processes with `-p 4` peaked at 19 client connections (sampled every 0.3 seconds, locally, where pgxpool's default pool is 10 connections against 4 on the CI runner), so there is headroom; the other shards were not measured. `tests/ci-tools-test.py` verifies coverage, determinism and the elapsed-time boundary. PostgreSQL services use disposable tmpfs data directories; the production database configuration is unchanged.

The email-layout fixtures reload after the inbound API creates their synthetic email. This avoids waiting for the inbox's 20-second activity poll; all layout stability and security assertions remain. The web job owns the unit tests and the production bundle gate. The e2e job runs the API drift and TypeScript checks in the background (`ci-background.sh`), with go vet, while Chromium installs and its database starts; in the web job they added 12 seconds to a step Vitest already filled. A type error or API drift therefore fails the e2e job's last step, not the web job. The image and the other browser jobs build the same Vite assets without repeating those checks, so a `docker build` outside CI no longer type-checks; releases take a commit whose CI passed. The non-onboarding fixtures each own a workspace and run on three Playwright workers. Their stability observation windows remain unchanged. Onboarding separates import journeys from the remaining journeys using complementary title filters. Both jobs have separate databases and servers; their journeys remain sequential. Native file sharding grouped seven tests against three, putting every expensive import case on the busier runner.

Independent guards run before the final gate. Full vet and migration validation run once, in the background of the e2e job: on the latency runner, the last Go job in most runs, vet added 6 to 8 seconds, and beside the tests of a regular shard 9.

Every job that sets up Go first resolves the newest Go 1.27 release from setup-go's manifest (`.github/scripts/go-release.py`), restores that toolchain from the Actions cache (`go-toolchain-<os>-<arch>-<version>`, saved by the first job that misses it) and hands setup-go the exact version, so setup-go finds it in the tool cache instead of downloading it, and a cached older patch never wins over a new release. When the manifest cannot be read, setup-go resolves `1.27.x` and downloads as before. Before this, the download took 7 to 19 seconds in each of the twelve Go jobs (runs of 2026-10-20). The image's Go stage builds on `golang:1.27-alpine`, a 75 MB pull against 302 MB for bookworm, whose pull took about 25 seconds in CI before the Go stage could start; the binary is static (`CGO_ENABLED=0`) and the runtime image is unchanged.

Measuring: the phase is the run's `updated_at` minus its `created_at`. The budget step runs inside `ci-ok` and cannot see the end of its own job, so it reads a few seconds short of the run's end; a run of 122 seconds (run 39460830259) passed it. Before this layout the last eight green main pushes took 115 to 133 seconds and pull requests 95 to 123; the jobs that ended last were the Go shards, `web`, `image` and CodeQL's Go analysis, all within a few seconds of each other.

Image builds use no remote layer cache: the measured cache restore/export cost exceeded the rebuild savings. A PR builds and loads through the runner's Docker driver, which skips the builder container and the tarball export; main keeps the container builder because its push carries an SBOM attestation. Keep image boot, real installer and compose smoke checks before publishing.

GitHub Free runs at most 20 GitHub-hosted jobs at once for the whole account. A pull request starts 18 CI jobs plus the Docs site build, leaving one runner for another repository; a twenty-first job would queue and its wait counts toward the 120 seconds (measured on 2026-10-19: a 21-second queue pushed a clean run to 134 seconds). The compose smoke test therefore shares the `roadmap` runner, started in the background by `.github/scripts/ci-background.sh` so the quick gates run during its build, and `tests/ci-tools-test.py` fails when the jobs a pull request starts together exceed 19.

That limit is shared by every run, so a push to main that lands while other pull requests are testing queues behind them: on 2026-10-22 three pushes to main took 126 to 135 seconds, their jobs waiting 20 to 33 seconds for a runner while up to 20 ran, and edge did not move for four hours. A push to main therefore first asks `.github/scripts/pr-verified.py` whether a pull request run from this repository already passed on the same files: the push must be a merge commit whose tree equals its second parent's (the branch already contained main, so the pull request run tested exactly this tree), and that head must have a successful `pull_request` CI run whose head repository is this one. Then the Go shards, `web`, `e2e`, `onboarding` and CodeQL are skipped, and the run keeps `image` (it builds, checks and pushes the `sha-<commit>` image that edge moves to), `roadmap` (the issue tracker changes independently of the files) and `ci-ok`, three runners instead of eighteen. Any other push (a branch behind main, a direct push, a fork, an API error) runs every job. Of the 40 merges before 2026-10-22, 23 were up to date. Pull requests are unchanged: `pr-verified` is skipped and the other jobs start at once. CodeQL's results for main come from the weekly scheduled run and from the full runs.

Test jobs start PostgreSQL and Mailpit with `.github/scripts/ci-services.sh start` as their first step and wait for them just before the tests, instead of a `services:` block: GitHub boots services before the first step, so their 9 to 18 seconds of pull and start used to delay checkout and toolchain setup. Each main run saves the Go module and build cache under its commit (`go-<os>-<go version>-<go.sum hash>-<sha>`, from Go shard 1), and every Go and browser job restores the newest entry with the same `go.sum`, falling back to the newest entry of any `go.sum`, so a dependency change no longer starts from an empty cache and the cache follows the code instead of freezing at its first save. Browser jobs install only Playwright's headless shell.

Earlier layout: the Go test shards and both browser jobs restored the Go module and build cache keyed on `server/go.sum`; CodeQL keeps a cold build. The browser jobs export the restored `GOPATH`, `GOMODCACHE` and `GOCACHE`, and `web/e2e/support/cli.ts` passes them to every `go run` and `go build`; a test that hard-codes its own Go paths compiles cold inside its 30-second timeout. Measured on 2026-10-19 ([run](https://github.com/colliedesk/colliedesk/actions/runs/39297304159)): 111 seconds from trigger to `ci-ok`, against 122 seconds before; the browser job fell from 109 to 71 seconds and the image job from 104 to about 80. A change to `go.sum` runs cold: the first such run measured 147 seconds, including a 15-second queue.

Measured on 2026-10-19: the original PR CI took 370 seconds. Removing idle polling reduced the browser step from 248 to 56 seconds before parallel workers; removing the remote image cache reduced its build from 103 to 54 seconds. The final partition's [full run](https://github.com/colliedesk/colliedesk/actions/runs/39260278182) passed every test and security job, but the phase gate rejected its 199 seconds: 84 seconds waiting for the previous run, then 115 seconds through the final job. These numbers do **not** establish budget compliance. Go CodeQL was the longest execution job at 107 seconds. The workflow has no Go, pnpm or Docker GHA cache to warm; acceptance requires the completed phase clock, never execution time with the queue subtracted.
