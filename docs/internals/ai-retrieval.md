# AI retrieval

What an AI feature (a reply draft, a triage suggestion) may quote, and how it is found. The feature
calls `store.AIRetrieve(ctx, tx, workspaceID, AIRetrievalQuery)` inside the scoped transaction of the
task's own workspace, ends that transaction, and only then calls the provider. Nothing here calls a
provider or needs one: a self-hosted server without AI builds the same rows, and they never leave the
database.

## What is in it

`ai_retrieval_docs` holds one row per source, derived and rebuilt at will:

1. A published knowledge base article, once in its own language (`kb_locale` NULL) and once per
   translation, unless an admin excluded it from AI (`kb_articles.ai_excluded`). Drafts and the trash
   are never in it.
2. A resolved or closed conversation: its subject, its first customer message (1,800 characters at
   most) and its last public reply, 4,000 characters in all. Private notes are never read. The names and companies
   of that conversation's contact and of the contacts merged into it become `[customer]` and
   `[company]`: whole in any case, with or without accents, and each word of a name of two letters or
   more on its own only capitalized as a name ("Long", "Tran" for Trần), so English text about Trần
   Văn Long keeps "too long" (`TestAIRetrievalMasksNamesWithoutEatingWords`). Every email address
   becomes `[email]` and every run of six or more digits, with spaces, dots, dashes, slashes or
   brackets allowed between them, `[number]`. A conversation without a customer message yet has no row.

`folded` is `kb_search_vector` of the text after `store.FoldForAIRetrieval`: lower case, accents
dropped (NFD without combining marks; đ, ł, ø by hand), so "dang nhap" finds "đăng nhập". It is done
in Go, not with the `unaccent` extension (not IMMUTABLE, and it would be the first extension of the
schema), and in a table of its own: a generated column on `messages` rewrites that table under an
exclusive lock (13.8 s for 120,000 rows).

## Keeping it current

Triggers queue a source in `ai_retrieval_pending` when it changes: an article's title, text, status,
trash or exclusion; a translation written; a conversation's status, subject or contact while it is or
becomes resolved or closed; a reply, or an imported message, on a resolved conversation (an import
writes the conversation before its messages, each in its own transaction). New mail from the inbox
never fires one: it reopens the conversation instead. A conversation that is open, as every new one
is, costs nothing.

Rows are only added to the queue, never updated: a change that commits while the worker builds a
source leaves a row of its own, so it is built next instead of being lost under the worker's delete
(`TestAIRetrievalKeepsAChangeThatCommitsDuringABuild`). The worker (`Store.RunAIRetrieval`, every 2
seconds from `serve`) lists the workspaces with queued rows under `PrivilegeAIRetrievalQueue` (ids
only), then per workspace, in its own scope and under a transaction advisory lock, reads up to 200
rows, replaces those sources' rows and deletes the rows it read. A workspace gets 25 such batches per
pass and the next one goes on, so one large import does not hold up the others. The upgrade queues
every published article and resolved or closed conversation; 82,000 conversations took 43 s to build
locally (120,000 conversations, 360,000 messages; the migration itself 0.34 s, writes to
`conversations` waited at most 0.3 s for it).

A source that may no longer be read loses its rows in the same transaction as the change (unpublish,
trash, exclusion, reopen), and a search checks every returned row against its source again, so a
queue that is behind never hands AI what it should not read. Deletes need nothing: foreign keys
cascade from the conversation, the article and the translation (undo of an import, retention, purge of
the trash). `restore-workspace.sh` copies the rows like any other table; they match the restored data, and the
queue's ids are uuids, not a sequence, so copied rows never collide with live ones.

## Ranking

The query's words are folded and split like the text, the 16 longest kept. A search ranks two sets of
candidates: the workspace's article rows, and its 5,000 newest conversation rows. For each word,
`df` is how many candidates match it (as a prefix) and its weight is `ln((N + 1) / (df + 0.5))`; a
candidate scores the sum over the words it matches. A word in every candidate weighs almost nothing,
so no stop word list is needed in any language. `ts_rank` is not used: on stripped vectors it only
counts matches, so "the", "a" and "of" outranked the one rare word. `kbQuery` (every word must match)
is not used either: a customer's email rarely contains every one of its words in one article.

An article comes once, in its best language; five are returned by default, each cut to the run of
paragraphs (about 1,500 tokens) holding the most query words. A knowledge base whose articles' own
language totals 15,000 tokens or less is returned whole, matching articles first. Three conversations
by default, never the one being answered.

## Traps

1. Under row-level security Postgres never uses a GIN index for `@@` (`docs/internals/knowledge-base.md`),
   so a search reads every vector of its candidates. Read each once: `folded` stays in the row
   (`STORAGE MAIN`), and each vector is copied once per row (`OFFSET 0` keeps the copy from being
   inlined into every word's test). A vector fetched from TOAST for every word, or copied into a
   materialized CTE (it spills past `work_mem`), took most of a second at 20,000 rows.
2. The budget is `TestAIRetrievalLatencyBudget` (`docs/internals/performance.md`). If it fails, cut
   `aiRetrievalWindow`, never the budget.
3. Known gaps, by design until backtesting (#99) measures them: no stemming (`emails` does not find
   `email`; the prefix match saves some), scripts written without spaces barely match, and an article
   in another language than the customer's is not found unless it has a translation in theirs.
   Embeddings (pgvector) would close the last one; the official Postgres image has no pgvector, and
   Anthropic has no embeddings model.
4. Indexing runs for every workspace, AI on or off, so turning AI on finds history at once. The rows
   do not count toward the storage cap (only attachments and media do).
