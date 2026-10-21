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
  `simple` configuration (no language is the default), made by `kb_search_vector`, which first turns
  ASCII punctuation other than `.` into spaces (see the trap on the parser). The query is built in SQL
  from `kb_search_vector(q)`, so both sides split the same way: `help@acme.example` is `help` and
  `acme.example`, `sign-on` is `sign` and `on`, a URL is its scheme, host and path words; every word
  matches as a prefix. A title `ILIKE` with `%`, `_` and `\` escaped catches the rest.
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
- **The default text search parser is quadratic in a run without whitespace.** The triggers are the
  characters of an email, a host, a URL, a file path or a hyphenated word, so the parser most likely
  reads ahead for one of those at every character and fails at the end of the run (the cause is
  inferred from the measurements, not read in its source). Measured on Postgres 17.11 on 2026-10-20,
  `to_tsvector('simple', repeat('_a', n))`: 0.34 s at 10,000 characters, 1.4 s at 20,000, 5.6 s at
  40,000, 34 s at 100,000 (the longest text), each holding one database core on every create, edit,
  import or approved request. Runs of 200 characters separated by spaces cost the same per character
  whatever the total, so the cost is the square of one run, not of the text. Fuzzing every pair of ASCII
  punctuation, letters, digits, non-ASCII letters, combining marks and spaces, triples and quintuples
  found these triggers: `_` next to a letter or digit, `/` with `.` or `~`, `@` after a word with
  digits (`a1@`), `-` between words with digits (`a-1-`), `-` with `.` (`a-a.`). `.` alone, letters,
  digits, non-ASCII text and controls stayed linear. `kb_search_vector` turns the 31 ASCII punctuation
  characters other than `.` into spaces before parsing (`regexp_replace` with the characters listed one
  by one: a class such as `[[:punct:]]` depends on the collation; `translate` gives the same text but
  made indexing ordinary text two to three times slower, 3.4 s instead of 1.2 s for the 2,000 articles
  of the search budget), so the parser sees words, numbers, hosts and versions only: every pattern
  above then costs 10–40 ms at 100,000 characters, prose 7 ms. `_` was already a separator; what changes is that an email, a URL, a
  file path and a hyphenated word are indexed as their words, not as one more lexeme. Keeping `.` keeps
  hosts and versions (`status.acme.example`, `1.2.3`) whole. `q` holds at most 200 characters, so the
  query side was never slow. `TestKnowledgeBaseSearchVectorCostsLinearTime` holds every write of these
  runs to 200 ms. Any new text search over customer text (the help center, conversations) must go
  through `kb_search_vector` or the same normalization.
- **`ListKBArticles` pages by offset** (an opaque cursor) because search orders by rank. A workspace holds
  at most 2,000 articles, so the offset stays small.
