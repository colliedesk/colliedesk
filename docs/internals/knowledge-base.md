# Knowledge base internals

Decisions and traps only. Code: `server/internal/store/kb-*.go`, `server/internal/httpapi/kb-handlers.go`,
`server/internal/mcpserver/catalog-kb.go` and `tools-kb.go`, `server/internal/domain/kb.go`, migration
`knowledge-base`. Issue #90. This first part is the data model, the API, the MCP tools and the export;
the editor, the public help center and the Freshdesk import come in later pull requests.

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
  article or collection whose parent is in the trash cannot be restored until the parent is.
- **`content_version` moves with every write.** `bumpKB` upserts `help_centers` in the same transaction
  as each knowledge base write, so a page cached under an older version is stale even when the newest
  `updated_at` did not change (unpublishing an older article). No row means the help center is off.
- **Limits:** 10,000 articles and 1,000 collections per workspace, the trash included (it stops a
  create-delete loop), 100,000 characters per text. A longer text is refused, never cut.
- **Search uses Postgres's own words.** A generated `tsvector` with the `simple` configuration (no
  language is the default) and a GIN index. The query is built in SQL from `to_tsvector('simple', q)`, so
  an address, a hyphenated word or a URL splits the same way on both sides; every word matches as a
  prefix. A title `ILIKE` with `%`, `_` and `\` escaped catches the rest. A word inside another word,
  and text in scripts written without spaces, are found only at a word start or in a title. `pg_trgm`
  waits for measurements.
- **Images are their own table.** `attachments` needs a message, and its download is `no-store`.
  `kb_images` stores bytes in the same attachment storage, one row per workspace and SHA-256, counted in
  `counted_storage_bytes` and `stored_storage_bytes` by statement triggers like attachments. The type
  comes from the bytes (PNG, JPEG, GIF, WebP; never SVG); the download sends `nosniff` and a sandboxing
  CSP. `kb_article_images` links an image to every article whose version ever named it, and links go only
  with the article, so restoring an old version never shows a deleted image. The purge deletes an image no
  article links once it is 30 days old.
- **Tiers.** Reads are `read`; writes, publishing and image uploads are `send` like saved replies (a
  publish reaches customers, but it is a version and can be undone); `PUT /help-center` is `admin`.
  A lower tier's write becomes an agent request, except an image: its bytes are refused with
  `files_need_approval`, like a multipart upload.
- **Locked workspaces get 402 on every KB route.** None is in `lockedAllowed`; the export still runs.

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
- **`ListKBArticles` pages by offset** (an opaque cursor) because search orders by rank. A workspace holds
  at most 10,000 articles, so the offset stays small.
