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
the collections. Each path must stay at or below 100 ms p95 (about 11 ms locally). Half the articles have
a position set by hand, scattered, and two more paths are called 20 times each (to spare the latency
runner): a collection's articles in their order, and the published ones in the help center's order
(the reply box's article list). On 2026-10-22 locally: 5–7 ms and 12–17 ms p95. The collection's list
took 17 ms before `kb_articles_order_idx`, which lets it read one page in order instead of sorting the
collection; the gate's run went from 5.1–5.2 s to 5.1–5.4 s, within the noise of the shared host. A search reads every
vector of the workspace, because row-level security keeps Postgres off the GIN index; raise the cap
only with this gate, after reading `docs/internals/knowledge-base.md`.

`TestKnowledgeBaseSearchVectorCostsLinearTime` writes and rewrites one article of 100,000 characters
per run that made the `simple` parser quadratic (`_a`, `~/`, `a1@`, `a-1-`, `a-a.` and others). Each
write must stay at or below 200 ms (10–25 ms locally; unnormalized, seconds to tens of seconds). A
3-second statement timeout makes a regression fail instead of hanging the run.

## AI retrieval budget

`TestAIRetrievalLatencyBudget` holds `store.AIRetrieve` (the knowledge base and the resolved
conversations an AI draft or triage suggestion may quote, `docs/internals/ai-retrieval.md`) to 200 ms
p95 warm. No screen waits for it: it runs inside a background AI task, before the provider is called.
It reads the knowledge base search budget's fixture (1,000 published articles of the 2,000 at the cap
in each workspace, 20 of them at 100,000 characters), builds their rows and adds 5,200 resolved
conversations of about 3,500 characters (more than the 5,000 a search ranks) to one workspace and
300 to the other. The query has 16 words, one in every text and one in half of them, the worst
case. Five warm-up searches, then 20 timed: 50–65 ms locally beside the other latency gates. The
window was cut twice instead of raising the budget: 20,000 conversations took 160–280 ms locally, and
10,000 took 210 ms p95 on the CI runner. If this gate fails, cut the window again.

## Intake queue budget

`TestIntakeQueueLatencyBudget` holds the store calls behind the intake queue to 100 ms p95 for a page
of 50 cards (`IntakeGatherings`: review, decided, review of one channel and scope, decided by the
rules) and 20 ms p95 for one card with its items, decisions and the 30 conversations it may join
(`IntakeGatheringByID`) and for the queue's summary (`IntakeSummaryOf`). The fixture is two workspaces
with 20,000 gatherings of three chat items each (a quarter waiting) in 200 groups, and the card's group
has 300 conversations. The candidates start from the group's items (`channel_thread_items_thread`), never
from the workspace's conversations. Five warm-up calls, then 40
timed. A card and its items are found by uuid: comparing `id::text` to a parameter skips the index and
took 17 ms on the dev host; the uuid compare takes 2 ms. Do not widen either budget.

## AI draft read budget

`TestAIDraftReadLatencyBudget` holds the reads the reply box polls, `ai.Service.GetDraft` and
`ListDrafts` and the article's monthly count (`ArticleDraftsThisMonth`), and the triage card's read
(`LatestTriage`, which loads after the conversation and polls while a suggestion is written), to 20 ms
p95 in the store, like the other per-conversation reads. Two workspaces with 10,000 drafts, 10,000
suggestions and their tasks each, 100 of each on the conversation read: 1–8 ms locally. It is in `internal/httpapi`, which the latency shard builds
anyway; a test in another package would add a test binary to the slowest shard. A
draft's own wait (`?wait=` up to 20 s) is a loop of these reads in short transactions, never a held
connection. The draft itself is written in the background: its sources are the AI retrieval budget
above, and the model's time belongs to no screen.

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
(1,000 published articles, 50 a page), a 9,000- and a 100,000-character article, searches with two
words, a short prefix, no match and a title, and the files for crawlers and AI assistants:
`sitemap.xml` and `llms.txt` (all 1,000 published articles; 17 and 19 ms p95 locally on 2026-10-28)
and the 100,000-character article's Markdown (4 ms). `TestPublicPagesMakeNoExternalCall` fails when
a help center source other than the admin's domain check names an outbound client (HTTP, DNS, SMTP). Each must stay at or below 100 ms p95 (6–16 ms locally).
A page is five short scoped transactions at most (slug lookup, settings and lock, collections,
articles, neighbours); a 304 stops after the second. Workspace A carries a full theme with about
31 KiB of custom CSS, the worst case of what every page reads and hashes into the brand sheet's name
(page p95 unchanged, and the gate took 0.4 s longer locally).

`TestHelpCenterCustomHostLatencyBudget` gives workspace A of the same fixture an active custom domain
and loads it by host name: the first request, which looks the host up (cold cache, 15 ms locally),
then the home page, the collection and an article 20 times each (warm, 8–9 ms p95), all under the same
100 ms. The host lookup adds one indexed privileged query per host name per 30 s per process; requests
to `APP_ORIGIN`'s host and the slug hosts never make it (`TestAppRequestsNeverLookUpACustomHost`
counts the statements). A name no cache knows is charged to the client first (10 a minute), so 20
random names from one client make at most 10 queries (`TestRandomHostNamesCostABoundedNumberOfLookups`),
and a domain write forgets only its own names (`TestDomainWritesForgetOnlyTheirOwnHostNames`). The gate adds about 0.5 s to the timed shard once the fixture is seeded.

`TestHelpCenterOldFreshdeskAddressesLatencyBudget` adds to the same fixture the import ledger a real
migration leaves (a `kb_article` row per article and 25,000 ticket rows in each workspace, by
`generate_series`) and times old Freshdesk addresses 20 times each: a mapped article (301), a draft
and an unknown id (302 to the search) and a folder (301, which also reads the collection tree), all
under 100 ms (3–7 ms p95 locally). It checks that `import_items_source_id_idx` is partial (knowledge
base rows only), and runs `EXPLAIN (GENERIC_PLAN)` on the store's own statement for each entity
(`store.HelpCenterLegacyTargetSQL`), so a drift in the query or a plan that cannot use the partial
index fails. Its rows are removed by a cleanup registered before the insert. Seeding takes about
0.9 s, the visits about 0.3 s. Measured on a scratch database with 505,000 ledger rows (5,000 of them
`kb_article`): the partial index is created in 73 ms and takes 264 kB; the full index it replaced took
1.1 s and 24 MB.

`TestHelpCenterHostileArticlesLatencyBudget` holds an article page to the same 100 ms when its
100,000 characters are built to be slow to parse: one line of quote or list markers (`>`, `> `,
`1. `, `+ `, mixed), 1,500 quoted players, same-named ATX and setext headings, open links and
images, and emphasis runs (goldmark's quadratic paths: up to 24 s before `tame`, `headingIDs` and
`linkify`). Each article's first visit renders it cold and is held to the budget (6–35 ms locally);
then 20 visits (not 50, to spare the shared latency runner) read the kept page (4–12 ms).
`TestHelpCenterRenderedPagesAreNeverSharedAcrossWorkspaces` keeps that memory per workspace.

`TestHelpCenterThemePreviewLatencyBudget` holds the brand preview to the same 100 ms p95. A person
waits for it 300 ms after each pause in typing, so it is a keystroke path. On the same fixture,
workspace A with a full candidate brand (logo, favicon, links, seven colors, two fonts): the home page
and the first published article (100,000 characters) are each previewed 50 times through
`POST /api/v1/help-center/theme/preview`, after one warm-up. The article is also timed cold: its first
render on the fixture's new server, held to the budget, and it must be the rendered page, not the text
shown while a render runs late. It reuses the fixture's server and adds about 1 s to the latency
shard. On 2026-10-22 locally, before the
change: home 16 ms p95 (22 KB answer), article 19 ms (127 KB), cold 24–37 ms. After it: home
9–12 ms (6 KB), article 11–13 ms (110 KB), cold 13–30 ms. The change has two parts. The reads became
one transaction sent as one batch (`TestHelpCenterThemePreviewReadsInOneTransaction` counts the
transactions: as many as reading the saved theme). `site.css` is linked by its immutable address
instead of being inlined in every answer.

## AI chat turn budget

`TestAIChatTurnSetupLatencyBudget` reads the knowledge base search budget's fixture (1,000 published
articles of workspace A, 20 of them 100,000 characters: too large to send whole) with a help chat
channel and the AI chat feature on the server's provider (the recording fake). It times 40 questions,
each from a new browser, from the request to the call of the provider: the channel and feature checks,
the chat and its turn, the set of articles and its fingerprint (read once per question, without
bodies), the excerpt search and the prompt, at or below 100 ms p95 (41 ms locally). Reading a chat back
by its cookie (`GET …/chat/session`) stays at or below 50 ms p95 (4 ms locally). The first question
measures the set of articles (and, with Claude, asks `count_tokens`) and is left out: it happens once
per set of articles and model. The time the AI takes is the provider's, not the server's. The chat page
and an article with its chat box are help center pages and keep the inbox's 200 ms p95 (4 ms and 6 ms
locally). Their scripts have budgets of their own, gzip: chat.js 25 KB (11 KB), widget.js 6 KB (2 KB),
chat.css 8 KB (6 KB), `TestTheChatFilesStayInTheirBudget`; none of them is in the web app's bundle.

## Results budget

`TestResultsComputeAndReadStayInBudget` (in `internal/httpapi`, so the latency shard builds no new test
binary) holds the background compute of one workspace's results page to 2 s, best of three: no screen
waits for it (the page reads a stored snapshot), but it holds a connection. The fixture is a busy team:
5,000 Colliedesk conversations over 30 days (a customer message and a reply each, half resolved) and
20,000 imported Freshdesk tickets in the baseline's window, with stats and time logged on a quarter.
Measured on the dev host: current numbers 130 ms, baseline 550 to 590 ms; seeding takes about 5 s. The
baseline cost is mostly writing the ~65,000 rows of the conversations behind its numbers; a foreign
key from those rows to conversations tripled it and was dropped (`docs/internals/results.md`). On the
same data the page's read (`store.ReadResults`: the state row and one snapshot of each kind) is held to
20 ms p95 over 200 calls (1.5 ms locally) and the list a number opens (`result=` on the 5,000
conversations of the 30-day volume, a page of 50) to the inbox's 100 ms p95 (5 ms locally); one seed
serves all three. Do not widen any of them.

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

## Configuration reload cost

Each `collie serve` reads which configuration version it should run every 5 seconds (one privileged
transaction: the state row, the current version's values, the products) and writes its heartbeat
(`server_processes`, one upsert and one delete): about 0.4 queries a second per process on the owner
pool, and one idle connection held by `LISTEN colliedesk_config`. A request never waits on it: the handler
swap is an atomic pointer, and settings read at each use are an atomic load.

## Web bundle budget

`corepack pnpm bundle:check` reads the production Vite output. The largest JavaScript asset must be at most 200 KiB gzip. The largest CSS asset must be at most 30 KiB gzip. `make test` builds the bundle before running this gate; in CI it runs in the e2e job, after that job's Vite build of the same commit, and a failure fails CI like any other step.

On 2026-10-20 the first bundle was 203,268 bytes of 204,800. Plan and billing, the locked screen, the trial survey and their strings (and those of Report a problem) moved to their own chunks, which brought it to 200,433; the knowledge base then added only its rail link and routes (201,420). Its pages with the preview and `remark-gfm`, and the help center card in Settings are lazy parts; the rail link preloads them on hover and focus. The article editor (CodeMirror 6) is a chunk of its own, 117,572 bytes gzip plus 1,502 bytes of CSS, loaded only by an article page; `kb-chunk` is 20,890 bytes. The first bundle went from 201,430 to 201,440 bytes (+10, the names of React hooks the chunks now import) and its CSS from 14,267 to 14,263.

On 2026-10-25 Settings → Channels (its pages, the channel registry and every channel's panels) went into lazy parts; the first bundle reached 204,742 bytes with the routes, the composer's channel notices and the nav. The agent request card imported the "Send from Gmail" part from the email channel section, which pulled the whole section (and its forms) into the first bundle; the part is now its own module (`settings/gmail-part.ts`), bringing it to 202,301.

On 2026-10-27 Autofix (the bridge to the customer's own coding agent) kept in the first bundle only the rail's Fixes link with its wrench icon and count (`useAutofixStatus`, one `GET /api/v1/autofix/status` a minute), the two routes, the slots in the conversation details and under the thread header, the Settings menu item, the request card's branch for Autofix calls, the composer's listener for a reply put in from elsewhere (`colliedesk:insert-reply`, used by Draft reply): 203,612 → 204,698 bytes (+1,086), CSS 16,828 → 16,833. Every Autofix screen (the conversation block, the send dialog, Fixes, Settings → Autofix, the request detail) is one lazy part, `autofix-chunk`: 25,451 bytes gzip plus 4,339 of CSS; the rail link preloads it on hover and focus. That leaves 102 bytes under the gate: the next feature has to move something out first. Measured with Playwright on a local `collie serve` (Chromium, 1,280 px, the Autofix routes answered by the test page, median of 5): rail → Fixes with its groups 68 ms, a click on a conversation → its Autofix block with the track 37 ms. Going to GitHub to confirm a fix leaves the app and is not counted; the page it comes back to shows the fix as Sending at once and reads it again every 3 seconds while it sends. On 2026-10-28 the Autofix end-to-end test (P6) measured it again against a real `collie serve`, PostgreSQL and the GitHub stand-in (`cmd/github-stand-in`), not routes answered by the test page: Chromium, 1,280 px, an Apple M4, assets cached, median of 5, each time read in the page (`performance.now()` checked every animation frame, since Playwright's own retries add up to 250 ms that are not the app's). Rail → Fixes with its groups 28 ms; a click on a conversation → its Autofix block with the track 38 ms; back from GitHub (the callback's redirect and a full load of the conversation's address) → the block's Sending or Sent badge 116 ms from the start of that navigation. The first bundle is 204,705 bytes (raw size unchanged; +7 gzip only from the new chunk names it references), `autofix-chunk` 25,782 bytes gzip (P6 fixes: the GitHub address is checked before leaving and a refusal is said; the issue's creator on the timeline) plus 4,312 of CSS.

On 2026-10-29 the AI chat (Settings → Built-in AI → AI chat, a chat's transcript at `/settings/ai/chats/<id>`, the Help chat channel's panels) went into the existing lazy parts: the first bundle keeps only the `help_chat` value of the conversation channel and the AI settings route as a splat (`/settings/ai/$`, one route for the page and its transcripts instead of one more). The help chat panels read the help center's address with their own small request rather than the knowledge base's API module, which would have become a shared chunk the first bundle lists. On top of the lazy-only words moved out of `messages.ts` the same day, the first bundle is 199,668 bytes, CSS 17,312.

On 2026-10-21 the reply box got **Articles** (link a help center article in a reply). Its first version kept the button, its strings, the link format and the extended saved reply picker in the first bundle: 201,985 → 202,563 (+578). The saved reply picker and the articles picker then moved together into one lazy part, `reply-pickers` (1.7 KB gzip, plus a 4.0 KB chunk of the API calls and strings it shares with the knowledge base pages), which the composer starts loading when it mounts; the first bundle keeps the two buttons and the part's loading and failure states: 201,776 bytes (−209 against 201,985), CSS 14,267 → 14,317.

On 2026-10-22 the help center card got its brand settings and preview. They are a chunk of their own
(`help-center-theme-form`, 5.9 KB gzip, 0.8 KB CSS) that the card asks for when its own chunk runs,
by a plain `import()`: through `ui/lazy-part`, or with its CSS in the card's chunk, the first bundle
would have had to export `lazyPart` to it or list one more file to preload. The library picker
comes from `kb-chunk` when someone opens it, so it adds no shared chunk. The first bundle holds the
same modules and went from 203,371 to 203,370–203,372 bytes across builds (the content hashes of the
chunk names it preloads compress differently; the refusal's `detail` moved from `ConflictError` to
`InvalidRequestError`, so a 422 keeps its field messages, at no cost). Its CSS stayed at 14,413.

On 2026-10-22 the in-app Freshdesk import (#97) brought the first bundle to 203,852 bytes, and collision detection (#101) needed about 1,100 more. Settings (its route and every section: team, email channel, saved replies, web form, help center, preferences) moved to a lazy part, `settings-route` (7,988 bytes gzip), as Plan and billing already was: the first bundle went from 203,852 to 196,712. The rail link starts loading the chunk on hover and focus; while it loads, `/settings` keeps an empty Settings page (`aria-busy`), so nothing jumps when the sections arrive. Measured with Playwright on the dev host against the e2e server: from a click on the rail link to the Settings heading 29 to 88 ms (5 runs), a full load of `/settings` 64 to 86 ms (3 runs).

On 2026-10-22 presence (issue #101: who else has a conversation open) first kept its list marks, the lines under the conversation title and the tab's heartbeat in the first bundle and went over the gate (204,929). They moved to one lazy part, `presence-view`, loaded once a conversation is open (after the ticket's own requests) or somebody else is on a listed one; the first bundle keeps the two small hooks, the API calls and the schemas: 204,078 bytes, CSS 14,641, before Settings moved out; with it, 197,547.

On 2026-10-22 Settings → AI (#89: AI providers and features) became its own lazy part, `ai-route` (9,400 bytes gzip plus 2,380 bytes of CSS), with its strings, schemas and calls. The first bundle keeps the menu link, the route and the `ai` field of the public config: 198,143 → 198,290 bytes (+147), CSS unchanged at 15,444. The page asks for nothing before it opens, and no inbox or conversation request waits for an AI provider.

The same day its Usage tab, the paused and allowance notices and the automatic-runs dialog grew `ai-route` to 12,071 bytes gzip plus 2,854 of CSS; the first bundle did not change. The usage of a month is asked for only when the tab opens. AI work runs as background tasks (`ai.Worker`): no request waits for a provider.

On 2026-10-23 the results page (#100) became its own lazy part, `results-route` (its strings, schemas, page and CSS); the first bundle keeps the rail link with its chart icon, the route, the `result` inbox filter, the public share page's route and the strings of a refreshed list: 201,489 → 202,056 bytes (+567); the share page is a lazy part of its own. Its CSS went from 15,890 to 15,893: the mobile filter chips now wrap instead of scrolling sideways, so a filter is never out of view.

On 2026-10-31 the Zalo OA pages, conversation numbers and response targets (their Settings pages are lazy parts) left 24 bytes under the gate with what the inbox itself needs (a conversation's code and target mark, the Targets filter, one timer that reads the list again when a mark comes due). The Markdown renderer of replies and notes (`react-markdown` with micromark and mdast, which `kb-chunk` shares) moved to a lazy part, `markdown-render`: the inbox starts it when it opens, before a conversation does, and a reply reads as plain text until it is there. First bundle 204,776 → 166,996 bytes; the renderer's shared chunk is 36,962 bytes gzip.

If the gate fails, inspect the generated assets and remove or lazy-load the dependency that caused the increase. Do not compare raw file size with the gzip threshold, and do not loosen the limits to pass CI.

## Operator console bundle

The operator console is a build of its own (`web/vite.operator.config.ts`, served under `/operator/`): none
of it is in the workspace app's bundle, and the workspace app's code is not in it. `corepack pnpm
bundle:check` also measures the console's first bundle (the JavaScript and CSS `operator.html` loads)
against its own ceilings, set on 2026-10-29 to the first measurement plus 5%: 78,652 bytes of JavaScript
and 9,076 of CSS gzip (`OPERATOR_JS_LIMIT`, `OPERATOR_CSS_LIMIT` in `web/scripts/check-bundle-budget.mjs`).
Never raise them to pass; move a screen into a lazy part. Measured then: 75,443 and 8,734 bytes; History,
Requests, the audit log and Operators are lazy parts of 0.8 to 2 KB each. The workspace app's first
bundle is unchanged (199,617 bytes). With the dangerous groups (the review's server guards in the first
bundle; the trial band and the Polar products table as lazy parts): 76,691 and 8,774. Measured with
Playwright on a local `collie serve` (Chromium, 1,440 px, an Apple M4, median of 5): a full load of the
overview to its numbers 41 ms; from a nav link to a settings page 16 ms.

On 2026-10-29 cloud sign-in on the console (email and code, then the authenticator) kept in the first
bundle only the sign-in page's branch, the meta read and a few strings: 77,416 → 77,843 bytes (+427), CSS
unchanged at 9,006. Clerk's form with its strings is the lazy part `cloud-sign-in`, loaded only by the
sign-in page of a server with cloud sign-in. The workspace app's first bundle: 200,409 → 200,411 (its cloud
sign-in now uses the shared `ClerkSession`).

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

CI has no wall-clock phase budget. Until 2026-10-24 it had 120 seconds from the workflow trigger, enforced by a step in `ci-ok` and two-minute job deadlines; the HC Labs owner exempted Colliedesk CI from that rule, so the gate is gone and jobs have ten-minute deadlines, which only stop a hung job. The layout below was built for the old budget and still keeps CI fast; the timings and the "120 seconds" in it are the record of that work, not a gate. CD keeps its 60-second budget (`ci-budget.py --phase cd` in `publish-edge.yml`, see `release.md`). A provider-green result is still the acceptance evidence for CI: every job in `ci-ok` passed or was skipped.

The Go matrix partitions the inventory printed by `go test -list . ./...` into eight disjoint name sets: five regular shards and three for the large latency fixtures (names containing `LatencyBudget`, `StaysInBudget`, `StayInBudget`, `StaysFast` or `CostsLinearTime`; run beside other packages' tests on the shared database, `TestStorageUsageStaysFastWithAHugeAttachmentTable` measured 389 ms against its 200 ms budget); the shard count comes from the matrix (`strategy.job-total`). Tests, examples and fuzz seed cases all run once; selecting a parent runs its subtests. `.github/scripts/go-test-shard.py` fails on an empty inventory and anchors each selected name. Each name weighs its time in CI, from `.github/go-test-timings.json`, the median over four runs (one slow runner slows every test it measures; the gates took 57 seconds a shard in one run and 18 to 25 in another); a name the file lacks (a test added since) weighs the median of the file. The shards' `go test` runs `-v`, and `.github/scripts/go-test-timings.py` reads the `--- PASS: TestX (1.23s)` lines of a run's log back into that file (its docstring has the commands; average a few green runs). A regular shard runs its names as three disjoint patterns in three `go test` processes at once (`--parts 3`): its tests mostly wait on PostgreSQL one at a time per package binary, and `httpapi` took most of the shard. Each latency shard runs its gates in one process, alone; the gates that share the knowledge-base seed (`HelpCenterPages`, `KnowledgeBase`, `KBMedia`) stay on the first latency shard so the seed is built once, and every other gate goes, longest first, to the latency shard whose gates end first. Then every regular name, longest first, goes to the part that would end first: a part's estimate is its process start (`--process-seconds 11`) plus its tests' summed times divided by `--parallel 2.7`, since a part's package binaries run at once (`go test -p`), while gates count in full, one after another. The values were fitted to run 39903507509 (`--parallel 2.7`: a regular shard's test step took about 11 seconds plus 0.37 times its fullest part's summed test times, and a latency shard's step its gates' summed times). With the suite of PR #276 that estimate was too kind to the regular shards: all five were estimated at 41 seconds and took 30 to 57, while the latency shards, estimated at 33, took 28 to 39 and took no regular names. Lowering `--parallel` to move regular names onto the latency shards was tried and undone: `--parallel 1.9` gave each latency shard about 80 regular names after its gates, and one then ran 121 seconds and hit the two-minute job limit (run 40099174970). Two timed tests whose names the markers missed ran on regular shards and failed there beside the other tests: `TestKnowledgeBaseSearchVectorCostsLinearTime` (203 ms against 200 ms, run 40098866967) and `TestResultsComputeAndReadStayInBudget`; `CostsLinearTime` and `StayInBudget` are gate markers now. A latency shard whose gates end early also takes regular names, after its gates (a `--` line in its pattern list makes the job wait for them). Before (counts, not times, with two latency shards and `--latency-share 0.5,0`), the AI pull requests' tests left the first latency shard at 109 seconds against 62 to 81 for the rest (run 39901392514): its knowledge-base gates took 27 seconds and its half share of regular tests 28 more, and the gates totalled about 100 seconds in CI. With the measured times and the AI pull requests' tests merged in, the last Go job ended at 94 to 103 seconds instead of 118 (runs 39905403828, 39906379551 and 39906928735); what is left is mostly per-runner speed (the same estimated shard took 26 to 43 seconds) and the 21 to 26 seconds of compiling a pull request that changes Go code. Against PostgreSQL's default `max_connections` of 100, shard 1 of 8 in three processes with `-p 4` peaked at 19 client connections (sampled every 0.3 seconds, locally, where pgxpool's default pool is 10 connections against 4 on the CI runner), so there is headroom; the other shards were not measured. `tests/ci-tools-test.py` verifies coverage, determinism, the time-based balance and the elapsed-time boundary. PostgreSQL services use disposable tmpfs data directories; the production database configuration is unchanged.

The email-layout fixtures reload after the inbound API creates their synthetic email. This avoids waiting for the inbox's 20-second activity poll; all layout stability and security assertions remain. The web job owns the unit tests; the production bundle gate runs in the e2e job after its Vite build. The e2e job runs the API drift and TypeScript checks in the background (`ci-background.sh`), with go vet, while Chromium installs and its database starts; in the web job they added 12 seconds to a step Vitest already filled. A type error or API drift therefore fails the e2e job's last step, not the web job. The e2e job runs its Vite build and compiles both test servers (`go build`, with and without `-tags e2e`) in the background too, so the servers' `go run` finds the packages in the cache; run serially at the start of the test step they took about 20 seconds (run 39889928477). The image and the other browser jobs build the same Vite assets without repeating those checks, so a `docker build` outside CI no longer type-checks; releases take a commit whose CI passed. The non-onboarding fixtures each own a workspace and run on three Playwright workers in each of two `e2e` shards (`--shard=i/2`; with `--fully-parallel` Playwright splits by test, not by file). In one job they took 48 of its 117 seconds and that job ended the run at 126 seconds (run 39902604105). Each shard builds the app and the servers itself, in the background while Chromium installs; a shared build would put a job in front of both. The type and API checks, vet and migration validation run on the last shard only, as does `.github/scripts/e2e-shard-check.py` in the background, which lists the tests of the unsharded run and of every shard and fails unless each test is in exactly one shard. A third shard would pass `RunnerConcurrency`. Their stability observation windows remain unchanged. Onboarding separates import journeys from the remaining journeys using complementary title filters. Both jobs have separate databases and servers; their journeys remain sequential. Native file sharding grouped seven tests against three, putting every expensive import case on the busier runner.

Independent guards run before the final gate. Full vet and migration validation run once, in the background of the last e2e shard: on the latency runner, the last Go job in most runs, vet added 6 to 8 seconds, and beside the tests of a regular shard 9.

Every job that sets up Go first resolves the newest Go 1.27 release from setup-go's manifest (`.github/scripts/go-release.py`), restores that toolchain from the Actions cache (`go-toolchain-<os>-<arch>-<version>`, saved by the first job that misses it) and hands setup-go the exact version, so setup-go finds it in the tool cache instead of downloading it, and a cached older patch never wins over a new release. When the manifest cannot be read, setup-go resolves `1.27.x` and downloads as before. Before this, the download took 7 to 19 seconds in each of the twelve Go jobs (runs of 2026-10-20). The image's Go stage builds on `golang:1.27-alpine`, a 75 MB pull against 302 MB for bookworm, whose pull took about 25 seconds in CI before the Go stage could start; the binary is static (`CGO_ENABLED=0`) and the runtime image is unchanged.

Measuring: the phase is the run's `updated_at` minus its `created_at`. Before this layout the last eight green main pushes took 115 to 133 seconds and pull requests 95 to 123; the jobs that ended last were the Go shards, `web`, `image` and CodeQL's Go analysis, all within a few seconds of each other.

The image builds from a layer cache in the Actions cache (`type=gha,scope=image`). The Go stage compiles the dependencies from `/deps.txt`, the list of non-main-module packages `./cmd/collie` imports, which a separate stage writes from the source: BuildKit keys the copied file by its content, so a commit that adds no import reuses the compiled dependencies and only the Colliedesk packages compile. Before, that compile sat after `COPY server ./` and every commit ran it cold: 52 of main's 117 seconds in the image job (run 39889234546, after the Anthropic SDK). An earlier remote cache was removed because restoring and exporting it cost more than it saved; that cache was invalidated by every commit for the same reason. Both events now use the container builder (the Docker driver cannot read the cache). CI only reads the cache: `.github/workflows/image-cache.yml` writes it after every finished CI run of a main push (`workflow_run`, red and cancelled runs too: a cold image job passes its two-minute timeout and GitHub ends the run as cancelled, so skipping those kept main cold until a write by hand; runs 39893295025 and 39893508473), outside the CI clock and after CI rather than beside it: a main push that runs every job takes 18 runners, `go-cache.yml` and Scorecard two more, and a 21st would queue a CI job. It can also be started by hand to refill an evicted cache; until it has written once, main's image job builds cold. Writing it inside the image job took 52 seconds after a dependency change (28 to compress the layers, 24 to upload them; run 39891145772), which the job's two minutes cannot hold. A pull request that changes the imported packages therefore builds them cold on every run (it cannot write an entry of its own: a writer beside its CI would be a 20th runner, over `RunnerConcurrency`'s 19, and a `workflow_run` writer runs in main's ref, where `scope=image` would replace main's entry with the pull request's), and so does the first main push after it: the image job then took 112 seconds against 72 to 79 warm (run 39892766124 against 39891583193 to 39892155463; 96 to 117 seconds before the cache, when every run was cold). Six of the 108 merges to main in the week to 2026-10-23 changed `go.sum`. A rerun does not help a red run, since the budget counts from the run's creation. Keep image boot, real installer and compose smoke checks before publishing.

GitHub Free runs at most 20 GitHub-hosted jobs at once for the whole account. A pull request starts 18 CI jobs plus the Docs site build, leaving one runner for another repository (`osv-scan` waits for `roadmap`, about 40 seconds, and takes about 20, so the second e2e shard has a runner at the start); a twenty-first job would queue and its wait counts toward the 120 seconds (measured on 2026-10-19: a 21-second queue pushed a clean run to 134 seconds). The compose smoke test therefore shares the `image` runner, started in the background by `.github/scripts/ci-background.sh` after the image is built: compose builds through the same builder, which holds every layer but the one with `VERSION`. On the `roadmap` runner it built the image again from nothing, 44 to 54 seconds after the quick gates (roadmap ended at 77 to 93 seconds). `tests/ci-tools-test.py` fails when the jobs a pull request starts together exceed 19.

That limit is shared by every run, so a push to main that lands while other pull requests are testing queues behind them: on 2026-10-22 three pushes to main took 126 to 135 seconds, their jobs waiting 20 to 33 seconds for a runner while up to 20 ran, and edge did not move for four hours. A push to main therefore first asks `.github/scripts/pr-verified.py` whether a pull request run from this repository already passed on the same files: the push must be a merge commit whose tree equals its second parent's (the branch already contained main, so the pull request run tested exactly this tree), and that head must have a successful `pull_request` CI run whose head repository is this one. Then the Go shards, `web`, `e2e`, `onboarding` and CodeQL are skipped, and the run keeps `image` (it builds, checks and pushes the `sha-<commit>` image that edge moves to), `roadmap` (the issue tracker changes independently of the files) and `ci-ok`, three runners instead of eighteen; `go-cache.yml` takes a fourth at the same time to save main's Go cache, outside the CI clock. Any other push (a branch behind main, a direct push, a fork, an API error) runs every job. Of the 40 merges before 2026-10-22, 23 were up to date. Pull requests are unchanged: `pr-verified` is skipped and the other jobs start at once. CodeQL's results for main come from the weekly scheduled run and from the full runs.

Test jobs start PostgreSQL and Mailpit with `.github/scripts/ci-services.sh start` as their first step and wait for them just before the tests, instead of a `services:` block: GitHub boots services before the first step, so their 9 to 18 seconds of pull and start used to delay checkout and toolchain setup. Every push to main saves the Go module and build cache under its commit (`go-<os>-<go version>-<go.sum hash>-<sha>`) from `.github/workflows/go-cache.yml`, which compiles every test binary from the newest entry. Until 2026-10-23 Go shard 1 saved it, but a merge `pr-verified` passes skips the shards, so main's newest entry stayed at an older commit and never held a module added since: every Go and browser job of the AI pull requests downloaded and compiled the Anthropic SDK (17 modules; 32 to 46 seconds of compiling per Go shard against about 9, runs 39878081880 and 39878438182). A workflow of its own adds nothing to the CI run's clock or to edge. A pull request whose `go.sum` has no entry yet saves once, on its own ref, from Go shard 7 (one shard pays for the upload); its later runs restore it, since a pull request reads its own ref before main. Its first run still compiles the new modules. Every Go and browser job restores the newest entry with the same `go.sum`, falling back to the newest entry of any `go.sum`, so a dependency change no longer starts from an empty cache and the cache follows the code instead of freezing at its first save. Browser jobs install only Playwright's headless shell. No restore key may match `setup-go-*`: actions/cache searches every key on the pull request's own ref before it tries main, and CodeQL's setup-go saves a build-only `setup-go-*` entry on that ref. Until 2026-10-22 that entry shadowed main's, so every Go shard of a pull request's second and later runs compiled the test packages again: 24 to 42 seconds before the first test, against about 20 with main's entry. The last fallback is `go-<os>-`, an older Go patch's entry from main. A Go shard compiles and links its test binaries (`go test -c`) before it waits for PostgreSQL, and lists them, and `go test` then reuses the cached binaries; `go test -c -o <dir>/` fails loudly if two test packages ever share a base name.

Go tests that need a migrated database clone one (`internal/database/databasetest`): each test gets `CREATE DATABASE … TEMPLATE` of a template that holds every migration and drops it with `DROP DATABASE … WITH (FORCE)`. Until 2026-10-23 each test created a schema, applied every migration into it and dropped it with `DROP SCHEMA … CASCADE`: 200 to 250 ms per test locally on tmpfs (more on Docker for Mac), and the drop took a lock per object, so parallel drops ran out of the lock table ("out of shared memory", 53200). The template is named after a hash of the migration files, the connecting role and `TEST_DATABASE_URL`'s database, so a changed migration builds a new one; the first test process builds it under an advisory lock on that database and the others wait for it. The lock covers only that database while database names are cluster-wide, hence the database in the name: two base databases on one server build two templates instead of dropping each other's half-built one. Traps: nothing may connect to the template (a clone fails while anyone is, so it is built with `ALLOW_CONNECTIONS false`); the row-level security migration names the app role after the database, so every clone shares the template's `colliedesk_app_colliedesk_tpl_<hash>`, as every test schema shared one before; every clone shares the one `instance` row the migrations insert (its install id and creation time). The migration, upgrade and isolation tests in `internal/database` and `TestMigrateCreatesTheRequestLoginFromDatabaseRequestURL` still run every migration, in a schema of their own (`databasetest.EmptySchema`). The test role needs `CREATEDB`. Clones use PostgreSQL 17's default `WAL_LOG` strategy: `FILE_COPY` was faster on tmpfs (12 against 18 ms per clone and drop, 28 against 51 ms with eight at once) but takes a checkpoint per clone, which on a disk-backed local server would flush beside the latency gates. Old templates stay on a local server until it is reset; CI's PostgreSQL is thrown away with the job.

Earlier layout: the Go test shards and both browser jobs restored the Go module and build cache keyed on `server/go.sum`; CodeQL keeps a cold build. The browser jobs export the restored `GOPATH`, `GOMODCACHE` and `GOCACHE`, and `web/e2e/support/cli.ts` passes them to every `go run` and `go build`; a test that hard-codes its own Go paths compiles cold inside its 30-second timeout. Measured on 2026-10-19 ([run](https://github.com/colliedesk/colliedesk/actions/runs/39297304159)): 111 seconds from trigger to `ci-ok`, against 122 seconds before; the browser job fell from 109 to 71 seconds and the image job from 104 to about 80. A change to `go.sum` runs cold: the first such run measured 147 seconds, including a 15-second queue.

Measured on 2026-10-19: the original PR CI took 370 seconds. Removing idle polling reduced the browser step from 248 to 56 seconds before parallel workers; removing the remote image cache reduced its build from 103 to 54 seconds. The final partition's [full run](https://github.com/colliedesk/colliedesk/actions/runs/39260278182) passed every test and security job, but the phase gate rejected its 199 seconds: 84 seconds waiting for the previous run, then 115 seconds through the final job. These numbers do **not** establish budget compliance. Go CodeQL was the longest execution job at 107 seconds. The workflow has no Go, pnpm or Docker GHA cache to warm; acceptance requires the completed phase clock, never execution time with the queue subtracted.

## Pages that need data and chunks

A page asks for its data and its chunks together, never one after another. The inbox asks for the
setup guide's data (`/api/v1/workspace/setup`) and its chunk when it opens, beside the session
request, so a guide the session says is hidden has cost one small request. The email setup page
(`/settings/channels/type/email`) starts its page chunk, its panels chunk and `GET /api/v1/channels`
when a link to it is pointed at, focused or followed (`channels-preload.ts`). The e2e test
`setup-loads-in-parallel.spec.ts` adds 150 ms to every request and fails when one of these requests
waits for another to come back. A new page with a lazy chunk and a query follows the same rule.
