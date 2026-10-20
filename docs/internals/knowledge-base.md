# Knowledge base internals

Decisions and traps only. Code: `server/internal/store/kb-*.go`, `server/internal/httpapi/kb-handlers.go`,
`server/internal/mcpserver/catalog-kb.go` and `tools-kb.go`, `server/internal/domain/kb.go`, migration
`knowledge-base`, and the web app in `web/src/features/kb/`. Issue #90. So far: the data model, the API,
the MCP tools, the export, the editor in the web app and the Freshdesk import, which writes through
`server/internal/store/kb-import.go` (decisions in `docs/internals/freshdesk-import.md`, "Knowledge base").
The public help center is in `help-center.md`.

## Decisions

- **Markdown is the only stored format.** `body_md` is what agents write and what the help center
  renders; of raw HTML it shows only YouTube and Vimeo players (`server/internal/kbhtml`, see
  [help-center.md](help-center.md)), and the text keeps every byte of it. An import keeps the original HTML in `source_html` and the fields Colliedesk
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
- **Images and videos are one media library table.** `attachments` needs a message, and its download
  is `no-store`. `kb_media` (renamed from `kb_images` by migration `kb-media-library`, `kind` image or
  video) stores bytes in the same attachment storage, one row per workspace and SHA-256, counted in
  `counted_storage_bytes` (original and a video's poster) and `stored_storage_bytes` (those and the
  thumbnail) by statement triggers like attachments; a thumbnail or poster comes by UPDATE, so an
  update trigger moves the counters by the difference (Postgres refuses transition tables on a trigger
  with a column list, so it fires on every update). The type comes from the bytes (PNG, JPEG, GIF,
  WebP; never SVG) and the decoder of that type must read the header too; the download sends
  `nosniff` and a sandboxing CSP. `kb_article_media` links an item to every article whose version ever
  named it, and links go only with the article, so restoring an old version never shows a deleted
  item; `current` marks the links the article's text or a translation names now.
- **Uploads lose their metadata, imports keep their bytes.** `server/internal/media` drops whole
  segments or chunks, never re-encodes: a JPEG loses APP1/APP3–13/APP15, comments, an APP2
  Multi-Picture index and everything after its end-of-image marker (a phone's motion-photo video, its
  other pictures), and gets its orientation back as a minimal EXIF segment; a PNG and a WebP keep only
  the chunks that draw the picture (an allowlist, so a C2PA manifest or a new kind of metadata goes
  too); a GIF loses comments and application blocks other than the loop count. The SHA-256 is taken after, so the same photo uploaded
  twice is one item, but an image stored before the library (or imported) is a second item when its
  bytes come again. Imports keep the bytes Freshdesk served ("No data loss"); removing their metadata
  is a later, explicit action.
- **Thumbnails under a memory budget.** 320 px on the long side, JPEG (PNG with transparency), made in
  the upload request under a semaphore weighted by the decoded size (`width × height × bytes per
  pixel`, 128 MB in all): a 16-bit 40-megapixel PNG decodes to 305 MB from a 572 KB file, and a
  progressive JPEG keeps 4 bytes of coefficients per sample besides its picture (515 MB for
  6000 × 6000, against 103 MB baseline). An image over
  the budget, or one the decoder cannot read (an animated WebP), has no thumbnail and the library shows
  the original. Older and imported images get theirs from `BackfillKBMedia` with the daily purge;
  `inspected_at` marks a row tried once. `thumbnail_url` carries the thumbnail's hash, so a new
  thumbnail is a new URL under the `immutable` cache.
- **"In use" is a live article's current text.** An article in the trash and an older version only are
  counted apart (`usage.trashed_articles`, `usage.older_versions`) and do not keep an item "in use".
  `linkKBMedia` reads the article's text and every translation from the database (a translation's write
  must not unmark the main text's links), takes the named items in the trash out of it (locked in id
  order), then locks every named item `FOR SHARE` in id order, restores any a trash committed while it
  waited, inserts links and sets `current`. Shares let saves and replacements that name one item run
  side by side. A trash locks only live rows and a restore or purge only trashed ones (`FOR UPDATE`, id
  order), so none waits on a save that holds the other side. A replacement locks all its articles in
  id order, then all their media the same way, before it writes. Tried and dropped: `FOR SHARE` then
  an update deadlocked two saves of one trashed item; every named item `FOR NO KEY UPDATE` deadlocked
  a replacement against a save (34 of 40 rounds).
- **The purge keeps what someone kept.** It deletes an item in the library's trash for 30 days, or one
  not `kept`, linked by no version and last uploaded 30 days ago; either way never one a live article's
  current text names. It locks the rows `FOR UPDATE SKIP LOCKED` first, then checks the rule again in a
  new statement: in READ COMMITTED a waiting DELETE rechecks only the row itself, not the `NOT EXISTS`
  on the links, so a save that committed a link while the DELETE waited would lose its item. Uploading
  in the library (`keep=true`) or naming an item or writing its alt text keeps it. A Freshdesk undo
  leaves kept images.
- **The library's trash goes by batch.** Trash, restore and purge take at most 100 ids (an agent
  request names them all, so a person approves exactly what moves); "all unused" moves 500 a call
  under one `kb_media_batches` row, so one Undo restores everything. A token below `send` calling
  `trash-unused` files a request for `POST /kb/media/trash` with the ids it would move, never "all".
  `before` (when the page was read) keeps an item uploaded since out of it. Batches go after 30 days.
- **A replacement is a new version, not new bytes.** An id always names the same bytes (cache
  `immutable`), so replacing writes the other item's id into every live article's current text and
  translations (version kind `media_replace`), and `kb_media_replacements` keeps the version each
  article got and each translation's text before and after. An undo puts back the version before
  where the replacement's version is still the newest, and a translation's text where it still holds
  the replaced text; anything changed since stays and is listed (`skipped_article_ids`). Rewriting
  every link of the new item back would have also changed one the article had before. An undo after
  the old item was purged is refused (`media_gone`). Zendesk keeps the URL instead; we do not.
- **A restore starts the 30 days again.** Taking an item out of the trash (or a save that does) sets
  `last_uploaded_at`, or an old item nobody kept would go with the next purge.
- **Limits:** 10,000 library items per workspace, the trash included (`domain.MaxKBMedia`, counted
  under `lockKB`); 5 MiB, 8,192 px on a side and 40 megapixels per image.
- **Tiers.** Reads are `read`; writes, publishing and image uploads are `send` like saved replies (a
  publish reaches customers, but it is a version and can be undone); `PUT /help-center` is `admin`.
  A lower tier's write becomes an agent request, except an image: its bytes are refused with
  `files_need_approval`, like a multipart upload.
- **Locked workspaces get 402 on every KB route.** None is in `lockedAllowed`; the export still runs.

## Editor (web app)

- **Lazy, all of it, and the editor apart.** The first bundle carries only the rail link and the three
  routes; the list, the article page, the preview with `remark-gfm` and their styles are the `kb-chunk`
  part, the editor is a chunk of its own (`features/kb/editor-part.ts`), and the Help center card in
  Settings is a small part of its own (`features/kb/kb-part.tsx`). `react-markdown` is already in the
  first bundle for messages, so the preview shares it. The list never loads the editor; an article page
  starts it with the article, and a pointer or the focus on the list or on New article preloads it.
  The first bundle's Tailwind scan skips `features/kb/editor/` (`@source not` in `styles/base.css`):
  words in its code (`italic`, `underline`) would otherwise become utilities in the first CSS.
- **The editor is CodeMirror 6 on Markdown source, byte for byte** (`editor/codemirror-editor.tsx`):
  what the person types is what `body_md` stores, so opening and saving an article an AI agent or an
  import wrote never rewrites its syntax. One exception: CodeMirror splits lines on `\r\n`, `\r` and `\n`
  and writes `\n`. The page compares bodies with line endings normalized (`sameBody`), so opening,
  publishing or leaving an article stored with `\r\n` sends nothing; its first real edit saves it with
  `\n` endings. Highlighting is the bare `@lezer/markdown` GFM parser in a
  `Language`, not `@codemirror/lang-markdown`, which imports the HTML, CSS and JavaScript languages
  (74 KB gzip) for code blocks. History, find and replace (`@codemirror/search`, Ctrl/Cmd+F), line
  wrapping. Enter does not continue a list or quote and Tab is not bound, so focus moves on: both
  belong to `lang-markdown` and the default keymap's indentation, which are left out on purpose (no
  keyboard trap, 74 KB). The toolbar only adds or removes Markdown around the selection (`editor/commands.ts`); a
  marker put on the cursor's line leaves the cursor after it. Its Tab stop is React state (`rover`) and
  falls back to Bold when the button that held it is disabled, so the toolbar never loses its stop.
- **Write, Side by side, Preview.** Side by side shows only in a window at least 1,180 px wide and is
  the default there for an article up to 20,000 characters (`splitLimit`); a longer one opens in Write,
  since every pause in typing redraws its preview in one long task (measured on a structured article of
  99,600 characters at 4× CPU: 190–220 ms). The preview's first draw waits for the editor's first frame
  (a frame, then a task: `previewReady`). The preview (`markdown-preview.tsx`, react-markdown) is the only costly part:
  rendering 100,000 characters takes tens of milliseconds. It never runs on the typing path: the
  editor tells the page of typing 120 ms after it stops, the preview reads that text through
  `useDeferredValue` (starting from `null`, so opening an article shows the editor first), and both
  panes have `contain: content`, so a key does not lay the preview out again.
- **Typing reaches the page 120 ms later.** Save, Ctrl+S, the leave prompt and a restore read the
  editor at once (`handle.markdown()`), so none of them works on a stale text.
- **A text is never cut.** The body has no `maxLength` (a browser cuts a paste to it silently); a
  counter of code points, as the server counts (`[...text].length`, not `.length`), refuses the save.
- **A lost race keeps the person's text.** On `changed_elsewhere` the editor offers "save mine over
  theirs" (reads the article again and saves with its `updated_at`; theirs stays in the history) or
  "discard mine". Every save takes the answer's `updated_at`, so a second save does not race itself.
- **Images never leave a placeholder in the text.** A pasted, dropped or inserted image is checked first
  (type, 5 MiB). A widget decoration in CodeMirror's state (`editor/upload.ts`) shows "Uploading…"
  while the text stays as it was; the image then goes in as a paragraph of its own after the block the
  cursor was in (a paragraph, a table, a list), never inside a line or a table row. A refusal removes
  the widget and shows the server's reason. An upload whose place went away (a restore or "their"
  version replaced the text) still ends, so saving does not wait for it forever.
- **Only collection address names break links.** An article's address carries its number, so a new
  article slug is harmless; a collection's is not redirected, so its form warns before saving.
- **The preview shows raw HTML as the help center does, without rehype-raw.** `rehype-raw` and
  `rehype-sanitize` would add about 56 KB gzip (parse5 and its entity table) to the knowledge base
  chunk; `kb-html.ts` instead parses each raw piece with the browser's `DOMParser` (an inert document:
  no script runs, nothing loads) and applies `allowlist.json`, for 2.5 KB. Vite reads that file from
  `server/internal/kbhtml` (`server.fs.allow`), and the Dockerfile's web stage copies it. Each piece is
  parsed once and kept (closing tags and players repeat). In Chromium (dev build) a 10,000-character
  article with 3 players previews in 6 ms (4.5 ms with no HTML); 100,000 characters of nothing but HTML
  take about 100 ms, or about 260 ms with 900 players, nearly all of it the browser creating the iframes.
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
  with the same bytes share a file, and so can a thumbnail or poster. `DiscardObject` checks
  `kb_media.storage_key`, `thumb_storage_key` and `poster_storage_key`; without that, deleting the
  attachment would delete the image's bytes.
- **A workspace's library rows are spread over the table.** Uploads of every workspace interleave, so
  reading 10,000 rows of one workspace touched about 9,800 heap pages (16 ms for a search, 29 ms HTTP
  for the unused page). The library index carries `size`, `poster_size`, `kept`, `kind` and `name`
  (not `alt`: 500 four-byte characters with the name could pass the 2,704 bytes a btree entry holds),
  and a page selects its ids from it before reading the rows, so the unused page and its summary read
  the index alone once the visibility map is current. Until autovacuum has run after a large import
  they read a heap page per row (32 ms on CI; the gate holds that case to the 100 ms list budget).
- **A filter not given is left out of the SQL.** `($5='' OR m.alt ILIKE …)` folds away in a custom
  plan, but after five runs of a prepared statement Postgres may keep a generic plan, which cannot drop
  the branch and reads every row's alt text from the table: the unused page took 17 ms through the API
  against 5 ms for the same query run once. `ListKBMedia` writes only the conditions a request has.
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
