# Knowledge base internals

Decisions and traps only. Code: `server/internal/store/kb-*.go`, `server/internal/httpapi/kb-handlers.go`,
`server/internal/mcpserver/catalog-kb.go` and `tools-kb.go`, `server/internal/domain/kb.go`, migration
`knowledge-base`, and the web app in `web/src/features/kb/`. Issue #90. So far: the data model, the API,
the MCP tools, the export and the editor in the web app; the public help center and the Freshdesk import
come in later pull requests.

## Decisions

- **Markdown is the only stored format.** `body_md` is what agents write and what the help center will
  render, without raw HTML. An import keeps the original HTML in `source_html` and the fields Colliedesk
  has no column for in `source_meta`; only an import writes them, the API never does, the export carries
  them, and the help center never renders `source_html`.
- **Copied from saved replies.** Versions, the 30-day trash, optimistic writes with `updated_at`, the
  version columns (person, token, approved request) and the `send` tier follow saved replies; the
  change schema is shared (`domain.KBArticleChange` is `SavedReplyChange`). A version stores title,
  slug, text, status and collection; restoring one brings back title, slug and text only, so a restore
  never publishes or moves an article by surprise.
- **Numbers are never reused.** `help_centers.last_article_number` counts per workspace, and the trash
  keeps its numbers until the purge, which does not lower the counter either. The help center address
  `/a/<number>-<slug>` therefore never points at another article. A slug may repeat; only a
  collection slug is unique, among the live collections of one parent.
- **Three levels, checked in Go.** Collections nest category, folder, subfolder like Freshdesk. Depth,
  loops and the limits are checked under one advisory lock per workspace (`lockKB`), taken by every
  write that counts rows or reshapes the tree. A collection that still holds a live article or
  collection cannot be deleted (409 `collection_not_empty`), so the trash never hides live content; an
  article or collection whose parent is in the trash cannot be restored until the parent is. Moving
  an article into a collection, and taking one out of the trash, read the collection `FOR SHARE`, so
  they wait for a delete of it in progress and then refuse.
- **`content_version` moves with every write.** `bumpKB` upserts `help_centers` in the same transaction
  as each knowledge base write, so a page cached under an older version is stale even when the newest
  `updated_at` did not change (unpublishing an older article). No row means the help center is off.
- **Limits:** 2,000 articles (`domain.MaxKBArticles`, the one place the cap lives) and 1,000
  collections per workspace, the trash included (it stops a create-delete loop), 100,000 characters
  per text. A longer text is refused, never cut. The article cap is what search can serve inside the
  budget (below); support teams of 1–10 agents rarely pass it, and an import over it is refused before
  it writes.
- **Search reads every article of the workspace.** A generated, position-stripped `tsvector` with the
  `simple` configuration (no language is the default). The query is built in SQL from
  `to_tsvector('simple', q)`, so an address, a hyphenated word or a URL splits the same way on both
  sides; every word matches as a prefix. A title `ILIKE` with `%`, `_` and `\` escaped catches the rest.
  A word inside another word, and text in scripts written without spaces, are found only at a word
  start or in a title. There is no GIN index: see the trap on row-level security.
- **Images are their own table.** `attachments` needs a message, and its download is `no-store`.
  `kb_images` stores bytes in the same attachment storage, one row per workspace and SHA-256, counted in
  `counted_storage_bytes` and `stored_storage_bytes` by statement triggers like attachments. The type
  comes from the bytes (PNG, JPEG, GIF, WebP; never SVG); the download sends `nosniff` and a sandboxing
  CSP. `kb_article_images` links an image to every article whose version ever named it, and links go only
  with the article, so restoring an old version never shows a deleted image. The purge deletes an image no
  article links once its last upload is 30 days old (`last_uploaded_at`; uploading the same bytes again
  gives the same image, touches it and needs no storage room).
- **Tiers.** Reads are `read`; writes, publishing and image uploads are `send` like saved replies (a
  publish reaches customers, but it is a version and can be undone); `PUT /help-center` is `admin`.
  A lower tier's write becomes an agent request, except an image: its bytes are refused with
  `files_need_approval`, like a multipart upload.
- **Locked workspaces get 402 on every KB route.** None is in `lockedAllowed`; the export still runs.

## Editor (web app)

- **Lazy, all of it.** The first bundle carries only the rail link and the three routes; the pages, the
  editor with `remark-gfm` and their styles are the `kb-chunk` part, and the Help center card in
  Settings is a small part of its own (`features/kb/kb-part.tsx`). `react-markdown` is already in the
  first bundle for messages, so the preview shares it.
- **A text is never cut.** The body has no `maxLength` (a browser cuts a paste to it silently); a
  counter of code points, as the server counts (`[...text].length`, not `.length`), refuses the save.
- **A lost race keeps the person's text.** On `changed_elsewhere` the editor offers "save mine over
  theirs" (reads the article again and saves with its `updated_at`; theirs stays in the history) or
  "discard mine". Every save takes the answer's `updated_at`, so a second save does not race itself.
- **Images go in by placeholder.** A pasted or dropped image is checked (type, 5 MiB), a placeholder
  with a unique `#upload-N` target marks its place, and the upload's `url` replaces it; a refusal
  removes it and shows the server's reason.
- **Only collection address names break links.** An article's address carries its number, so a new
  article slug is harmless; a collection's is not redirected, so its form warns before saving.
- **History and trash are the saved reply panels** (`saved-reply-panels.tsx`), made generic with
  optional props; the change line reads the same, with `import` added.

## Traps

- **Default privileges grant UPDATE and DELETE on every new table.** The schema's default privileges
  give the app role all four rights, so `GRANT SELECT, INSERT` alone does not make a table append-only.
  The migration revokes all rights on `kb_article_versions` first; `TestKnowledgeBaseVersionsAreAppendOnlyForTheAppRole`
  pins it. (`saved_reply_versions` was created with only the grant, so the app role can still change it.)
- **A generated column cannot be copied in.** `restore-workspace.sh` skips columns with
  `attgenerated` set; the database computes `kb_articles.search` again.
- **One file can back several rows.** Storage keys are content addresses, so an image and an attachment
  with the same bytes share a file. `DiscardObject` checks `kb_images` too; without that, deleting the
  attachment would delete the image's bytes.
- **An agent request holds at most 64 KiB of JSON.** A read or draft token proposing a long article gets
  413; a send token writes it directly.
- **Row-level security turns GIN off for `@@`.** Postgres uses a condition as an index condition on a
  table with row-level security only when its function is leakproof, so the policy is checked first;
  `ts_match_vq` (`@@`) is not (`pg_proc.proleakproof`), and marking it needs a superuser. Every
  workspace table forces row-level security, privileged paths included (policy `privileged_access`),
  so a GIN index on the vector is never used, whatever the query shape (`UNION`, materialized CTE,
  `enable_seqscan = off`, planner costs) or index (`btree_gin` composite). Measured on 2026-10-20 with
  10,000 articles of about 9,000 characters (vocabulary of 20,000 words, 20 articles of 100,000):
  without row-level security the GIN index answers in 0.8–1.4 ms; reading the vectors takes 45 ms
  (34 ms with positions stripped) and the HTTP search 60–80 ms p95 locally, too close to the 100 ms
  budget. At the 2,000 cap the gate measures about 11 ms p95. Ways to raise the cap, each needing
  the owner: (1) a search index table without row-level security, reached only through a
  `SECURITY DEFINER` function that takes the workspace from `app.workspace_id` (breaks the rule that
  every workspace table forces row-level security, which the isolation gate and the upgrade check
  enforce); (2) a leakproof match function installed by a superuser (self-hosters would need one);
  (3) an external search engine.
- **No object literal inside an API path in the web app.** `TestEveryAPIPathTheWebAppCallsIsARoute`
  reads a path literal up to its first `}`, so `` `/kb/articles${query({ cursor })}` `` reads as a path
  that is not a route. `kb-api.ts` builds queries through small helpers called with variables.
- **`ListKBArticles` pages by offset** (an opaque cursor) because search orders by rank. A workspace holds
  at most 2,000 articles, so the offset stays small.
