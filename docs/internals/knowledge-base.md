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
  version columns (person, token, and the approved request of a change from before requests were
  removed) follow saved replies; the
  change schema is shared (`domain.KBArticleChange` is `SavedReplyChange`). A version stores title,
  slug, text, status and collection; restoring one brings back title, slug and text only, so a restore
  never publishes or moves an article by surprise.
- **Numbers are never reused.** `help_centers.last_article_number` counts per workspace, and the trash
  keeps its numbers until the purge, which does not lower the counter either. The help center address
  `/a/<number>-<slug>` therefore never points at another article. A slug may repeat; only a
  collection slug is unique, among the live collections of one parent.
- **A home and extra collections.** `kb_articles.collection_id` stays the article's only home: the
  breadcrumb, its manual position, versions, the collection delete guard and every import use it, so
  the API and the export stayed backward compatible. `kb_article_collections` adds the other
  collections; both foreign keys carry `workspace_id`, so a row cannot cross workspaces. A link stays
  while its collection is in the trash (the API hides it, a restore shows it again); a purge cascades.
  The help center lists an extra after the collection's own articles, by title, with no position of
  its own: ordering extras by hand is left out until someone asks. Lists group by the collection the
  article is listed in (`PublicKBArticle.CollectionID` is that one in lists, the home on the article
  page); the sitemap, llms.txt, search and AI read `kb_articles` alone, so each names an article once.
- **Six levels, checked in Go.** Collections nest up to `domain.MaxKBDepth` (6) levels; the Freshdesk
  import still places folders three deep (`kbCollectionLevelMax`) so a re-run lands where the first one
  did. The help center folds the middle of a breadcrumb deeper than three levels into a `<details>`
  (Home, the first level, "…", the last two: `helpcenter/crumbs.go`), since its CSP runs no script;
  the BreadcrumbList structured data keeps every level. Depth,
  loops and the limits are checked under one advisory lock per workspace (`lockKB`), taken by every
  write that counts rows or reshapes the tree. A collection that still holds a live article or
  collection cannot be deleted (409 `collection_not_empty`), so the trash never hides live content; an
  article or collection whose parent is in the trash cannot be restored until the parent is. Moving
  an article into a collection, and taking one out of the trash, read the collection `FOR SHARE`, so
  they wait for a delete of it in progress and then refuse.
- **`content_version` moves with every write.** `bumpKB` upserts `help_centers` in the same transaction
  as each knowledge base write, so a page cached under an older version is stale even when the newest
  `updated_at` did not change (unpublishing an older article). No row means the help center is off.
- **Order by hand is a position per sibling, 0 until set.** `kb_articles.position` (migration
  `kb-article-position`) and `kb_collections.position` sort siblings before the name or title, so 0
  everywhere is the title order every list had before: the migration rewrites no row. `PUT
  /api/v1/kb/order` takes the whole list of a parent's live children (collections and, under a
  collection, articles, drafts included) and numbers them 1..n under `lockKB`; a list that misses or
  adds one is 409 `order_stale` with the lists as they are, never a partial order. The web app reads
  the parent's whole article list first when it does not have it (a parent's articles while another
  collection is shown, or more than the loaded page, at `limit=200`), so a move is one `PUT`; only a
  list that went stale meanwhile is moved again in the server's lists and sent once more, and a row
  that is gone from them is reported, never sent as an unchanged order. Positions are not versioned
  and do not touch `updated_at`, so reordering never makes someone's open edit `changed_elsewhere`;
  an order that moves nothing does not bump `content_version`. A new, moved, imported (Freshdesk,
  including an article that changed folder on a rerun) or restored item, and a new collection
  without a position, goes after the last sibling only once the parent was ordered (max position >
  0); otherwise it stays 0 and falls in by title.
- **Lock order of an order by hand: `lockKB`, then article rows, then collection rows.** Moving an
  article locks its row, then reads the target collection `FOR SHARE`; `SetKBOrder` once updated the
  collections first and deadlocked against it (40P01, answered 409 `conflict`). Now `readKBOrder`
  locks the members `FOR NO KEY UPDATE`, articles before collections, and every write that places an
  item (article move, restore from the trash, reimport) takes `lockKB` before its row lock. Taking
  `lockKB` after the row lock brings the deadlock back. The locked read also drops a row that moved
  away while it waited (409, not a stale place written into the other collection), the `UPDATE`
  filters on the parent and `deleted_at`, and a place computed under `lockKB` sees an order committed
  just before it. `kb_order_race_test.go` holds each race open with a sleeping trigger. Every list sorts with
  the same keys (`kbArticleOrder`, `kbCollectionOrder` in `store/kb-order.go`); the article list
  without a collection uses the tree's depth-first path (`kbTreeOrder`) when `sort=manual`.
  `kb_articles_order_idx` serves a collection's list in order without sorting it.
- **A collection's article order is a sort mode, not a rewrite of positions.**
  `kb_collections.article_order` (`manual`, `title`, `newest`, `updated`) picks the keys in
  `kbArticlesBy` (`store/kb-order.go`): each mode is a `CASE WHEN mode=… THEN key END`, so one SQL
  serves every collection of a tree list at once, and `lower(title), number` close every mode so a
  page boundary never moves. Positions stay as they were, so `manual` brings the hand order back;
  `PUT /api/v1/kb/order` still accepts an order under another mode (it is stored, not shown), and the
  web app hides the move buttons instead of refusing. `newest` reads `COALESCE(published_at,
  created_at)`, so a never-published draft sorts by when it was written. Trap: the help center lists
  an article from another home (`a.extra`) under the listing collection's mode, not its home's (`lc`
  joins on `a.listed`), and a translated page sorts `title` by `t.title`. A non-manual collection
  list is not served by `kb_articles_order_idx`; at 2,000 articles per workspace the sort stays inside
  the budget.
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
- **The library's trash goes by batch.** Trash, restore and purge take at most 100 ids; "all unused"
  moves 500 a call under one `kb_media_batches` row, so one Undo restores everything.
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
- **Videos are not converted, and the server reads them itself.** Only what every browser plays as it
  is: MP4 (H.264, AV1; AAC) and WebM (VP8, VP9, AV1; Opus, Vorbis). `server/internal/media` walks MP4
  boxes and WebM elements (depth 8, 10,000 boxes) for the brand, codecs, size (a quarter-turned track
  matrix swaps it) and length; `http.DetectContentType` names neither a `.mov` (`qt  `) nor an MP4
  branded `isom` alone. HEVC is refused with how to export H.264: browsers play it only on some
  hardware. No ffmpeg in the image (tens of MB, licences, minutes of CPU a video).
- **Location leaves a video in place.** MP4 `udta`, `meta` and `uuid` boxes (top level, `moov`,
  `trak`) become `free` with zeroed content; a track that is not picture, sound or timecode (GoPro
  GPS, subtitles) has its samples zeroed (from `stsz`, `stsc`, `stco`/`co64`) and its `trak` freed.
  Sizes and offsets never move, so nothing is rewritten. A file where that is not safe is refused:
  `stz2`, missing tables, a `dref` to another file, a fragmented file with such a track, a sample
  outside `mdat` or overlapping picture or sound. WebM `Tags`, `Attachments` and the title become
  `Void`; a WebM track that is not picture or sound is refused, and the walk stops at a cluster of
  unknown size (a browser's recording, which writes no tags).
- **Uploads in parts, one replica.** `POST /kb/media-uploads` reserves room (the plan's storage and the
  disk, counting what the open uploads still need), then parts of 8 MiB go to
  `<ATTACHMENTS_DIR>/.incoming/media-<id>` with `Content-Range`; a part not at the offset is 409 with
  `Upload-Offset`. Complete checks the file, neutralises it in place and renames it to its content
  address (`attachments.Staging.Adopt`), so a video never needs twice its size on disk. One part of an
  upload at a time and six of a workspace, held in memory: the server runs as one process (like
  `billing.md`'s single replica). Each part must arrive within 120 s (`SetReadDeadline`), against a
  client sending a byte now and then. Expired uploads go with the daily purge, their files too, and
  so does a staged file older than an hour that no open upload names (a part written while its
  upload was cancelled; cancel also waits for a part in flight). The disk check counts what every
  workspace's open uploads still need (`PrivilegeKBUploadDisk`), one start at a time under a mutex
  (one process), so workspaces starting together never promise the same room twice; counting the
  database rather than memory survives a restart. Past 200 uploads in progress on the server a new
  one is 503 `server_busy`: a safety valve, not a share (about 67 workspaces with 3 open uploads each
  can fill it until theirs complete or expire), and checked before the workspace's own plan, so a
  busy or full server answers first (counting staged files instead let seven
  trial workspaces block every other one). Complete checks the cap and the plan before it renames the file, so a refusal
  keeps the parts for a retry, and a lost file is 409 `upload_lost`.
- **Neutralising MP4 is an allowlist.** Each container keeps only the boxes a player reads
  (`keptBoxes` in `media/video.go`); any other box, a dashcam's `gps ` included, becomes `free` with
  its content zeroed, as does what `free`, `skip` and `wide` hold. The creation and modification
  times of `mvhd`, `tkhd` and `mdhd` and WebM's `DateUTC` are zeroed. The sample tables are read in
  one pass over `stsc` (one per chunk was quadratic: 20 minutes of CPU for a 32 MB file).
- **Video bytes seek.** `GET /kb/media/{id}` serves through `http.ServeContent` when storage hands back
  a seeker (local disk): ranges, `If-Range`, `ETag` = SHA-256, `immutable`. A poster has its own
  address `/poster/<last 8 hex of its content address>`, so a new poster is a new URL and an old one
  is 404; it counts toward the plan, and its thumbnail is the video's in the library.
- **Limits:** 10,000 library items per workspace, the trash included (`domain.MaxKBMedia`, counted
  under `lockKB`); 5 MiB, 8,192 px on a side and 40 megapixels per image.
- **Permissions.** Reads need `kb_articles:read`/`kb_media:read`; writes `kb_articles:create`,
  `:update`, `:delete` and `kb_media:*`; anything the help center shows also `kb_articles:publish`
  (a publish reaches customers, but it is a version and can be undone); `PUT /help-center` needs
  `help_center:update`. A call without the permission is 403 `permission_missing`. Saving or
  discarding the unpublished changes of a published article changes nothing customers read, so it
  needs `:update` only; publishing them needs `:publish` ([Drafts](#drafts)).
- **Locked workspaces get 402 on every KB route.** None is in `lockedAllowed`; the export still runs.

## Drafts

Code: `server/internal/store/kb-drafts*.go`, migration `kb-article-drafts`, MCP
`catalog-kb-drafts.go`, web `article-draft.tsx` and `line-diff.ts`. Issue #673.

- **One row of unpublished changes per published item.** `kb_article_drafts` holds the full text
  (title, slug, body, SEO fields, collections, On this page switch) of the next version. `locale` is NULL for the article's
  own text and a language for a translation; two partial unique indexes (per article where `locale IS
  NULL`, per article and locale otherwise) keep one row each. One table with a locale beat a
  `translation_id` column: one index shape, one export file, one cascade. `collection_id`,
  `extra_collection_ids` and `show_contents` NULL mean "keep as it is"; a translation row has none of
  them, since the article holds its collections and its On this page switch. A row exists only while its item is published:
  a never-published article is still written directly, and unpublishing folds the row into the
  article and deletes it.
- **Save never touches the live row.** `PATCH` on a published article upserts the row (based on the
  row, else the live article) and records a version of kind `draft`; `kb_articles.updated_at`,
  `published_at`, `help_centers.content_version` and every public read stay as they were, so no
  `bumpKB`. Publish (`POST …/draft/publish`, in `routesReachingOutside`) copies the row into the
  article in one transaction, records an `edit` version, marks translations outdated as an edit does,
  deletes the row and bumps the help center. Discard (`DELETE …/draft`) deletes the row and relinks
  media; versions stay.
- **One concurrency token per article** (`kbArticleToken`): the later of the article's and the draft's
  `updated_at`. Every read (get, list, search, trash, `?version=draft`) shows it as `updated_at`, and
  every writer (save, publish, discard, restore, delete, undelete) checks it, so a publish never puts
  live text its caller has not read and a client never has to tell the two apart.
- **Public reads see only live rows.** Help center pages, `.md`, search, `llms.txt`, sitemap and the
  help chat read `kb_articles` and `kb_article_translations`, never the drafts; the chat cites the
  newest non-`draft` version. Image and video serving checks the live text and published translations
  (`shownTextNames`), so an item only a draft shows is not public; `linkKBMedia` still counts drafts,
  so the library never purges it as unused.
- **Autosave is a save with no version.** `PATCH` with `autosave: true` takes only `title` and
  `body_md` (any other field is 400, so an autosave never publishes, unpublishes or moves an article).
  On a published article it upserts the draft row without a `draft` version; on a never-published one
  it writes the row in place with no version and no help center bump (`changeKBArticleLocked` with an
  empty kind), and still links the media the text names. The token check is the same as a save's, so a
  stale `updated_at` is 409 `changed_elsewhere`. The next save without `autosave` records the version.
  The MCP tool keeps saving with a version: an agent's write is a decision, not keystrokes.
- **Restore on a published article writes the draft**, needs `:update` only, and keeps the old rule
  that a restore never publishes.
- **Delete and undelete keep the row**: it goes to the trash with the article and comes back with it;
  a purge cascades.
- **Media replacement rewrites drafts too** (`replaceInKBDraft`), so a later publish never brings the
  replaced item back. The undo puts the old text back only while the draft still holds the replaced
  text; otherwise the article is listed as skipped. The undo's "previous version" of an article skips
  `draft` versions, so it restores the live text.
- **A Freshdesk rerun leaves an article with a draft alone**, live text and draft, and lists it under
  `kb_article_changed_in_both`. `KBImportedLastSQL` (rerun, undo, link rewrite) skips `draft`
  versions and checks for a draft row instead, so a discarded draft leaves the article the import's
  again: nothing of the person's work is lost by taking the next Freshdesk change.
- **No slug uniqueness check** on a draft: the address carries the number, and article slugs are not
  unique today.
- **Export** writes `kb-article-drafts.ndjson`; the versions file carries the `draft` kind.
- **Agents:** `kb_article_update` saves the draft, `kb_article_get` returns `draft` in the same call
  (no separate draft tool, the tool list is near its budget), and `kb_article_draft_publish` /
  `kb_article_draft_discard` act on it; with `locale` they act on a translation's row
  (`…/translations/{locale}/draft[/publish]`).
- **A translation's row follows the article's rules** (`kb-translation-drafts.go`). Its token is the
  later of the translation's and the row's `updated_at`; save, autosave, restore and media replace
  write the row, unpublish folds it into the translation, delete and undelete keep it. Its
  `attachment_ids` are the files the row shows, copied to the translation on publish. A Freshdesk
  rerun or undo treats a translation with a row as changed in Colliedesk.
## Votes

Migration `20261120073704-kb-article-votes`; code `helpcenter/votes.go` (form, cookie, redirect),
`helpcenter/vote-text.go` (words per language), `store/kb-votes.go`, `httpapi/kb-vote-handlers.go`,
MCP `catalog-kb-votes.go`, export `store/export-kb-votes.go`, web `features/kb/article-votes.tsx`.

- **No script.** The box is two forms that `POST <article>/vote` and get a 303 to
  `?voted=yes|no|sent#helpful`; the CSP is unchanged. The POST must carry an `Origin` equal to the help
  center's own (`AddressOrigin`), so another site cannot vote for a reader. Once the origin passes, a
  vote is charged to its own limiter (20 a minute per address, `helpCenterVotesPerAddress`), apart from
  the search limiter, so searching never blocks voting and a foreign form costs nothing.
- **At most 10 new readers per article, address and day.** A new reader is one with no current vote on
  the article; past 10 from one address (IPv4 /32, IPv6 /64, `config.Client`) in 24 hours, the vote gets
  the same 303 and thanks but writes nothing, so a bot clearing its cookie learns nothing. A reader
  already counted still changes their vote. Only `HMAC-SHA256(kb_vote_address_key, bucket)` is stored,
  in `address`; the raw address never is. The key is a server secret (`ServerSecret`), not in the
  export. The check runs under the article's row lock in `kb_article_vote_versions`, so two votes at
  once cannot both pass it.
- **One vote per reader and article.** The reader is a random 32-byte id in a first-party, HttpOnly,
  SameSite=Lax cookie (`__Host-cd_reader` on a host of its own, `__Secure-cd_reader_<slug>` with the
  `/help/<slug>` path, no prefix on plain http), kept 365 days. Only `sha256("reader\x00" + cookie)` is
  stored, in `voter`; the unique index is on (workspace, article, voter) where `reset_id IS NULL`, so a
  second vote updates the first. Every reader is anonymous: there is no contact column until readers
  can sign in (#290).
- **Masking before storage.** `missing` goes through `textmask.Mask` (emails, runs of 6+ digits), then
  is cut to `domain.MaxKBVoteMissingChars` (500). The raw line is never stored or logged.
- **Count = own votes + imported thumbs.** Freshdesk's `thumbs_up`/`thumbs_down` in `source_meta` of
  the article and its published, non-deleted translations count only while the article has no reset: a reset is a fresh start,
  and imported thumbs cannot be closed one by one.
- **Reset closes, never deletes.** It inserts a `kb_article_vote_resets` row with the counts the page
  showed and sets `reset_id` on the current votes. The table is append-only for the app role (UPDATE and
  DELETE revoked; `TestArticleVoteResetsAreAppendOnlyForTheAppRole`); rows go only by cascade when the
  article is purged. A reset with nothing counted stores nothing, and the idempotency key makes a double
  press one reset. A trashed article keeps its votes to read and cannot be reset.
- **Cache.** A vote or reset bumps the article's row in `kb_article_vote_versions`, which is part of
  that article's page ETag (`articleTag`): its count is never a stale 304, and other articles keep
  theirs. The page reads the version before the article, so a count newer than the tag only costs a
  refetch, never a stale page under a new tag.
- **Export.** `kb-article-vote-resets.ndjson`, then `kb-article-votes.ndjson` with closed votes too:
  after a reset the export is the only way to read them. `voter` is exported hex.

## Searches

Migrations `20261122031955-help-center-searches` and `20261122075809-kb-search-help-center`; code `helpcenter/search-log.go` (who is kept, the
signed link, the open), `helpcenter/pages.go` (`search`, `article`), `store/kb-searches.go`,
`store/kb-search-report.go`, `httpapi/kb-search-report-handlers.go`, MCP `catalog-kb-search-report.go`,
retention `cmd/collie/kb-search-retention.go`, export `store/export-kb-searches.go`, web
`features/kb/searches-page.tsx`.

- **No script.** The open is counted from the link, not from the page: result links of a kept search
  carry `?s=<id base36>.<signature>`, and the article handler records the open, then answers 302 with
  `Cache-Control: no-store` to the canonical address. The canonical link, the sitemap and
  `rel=canonical` never carry `s`. The CSP is unchanged.
- **Only readers.** A search is kept only for `pageview.IsReader` (no bot, prefetch, HEAD or signed-in
  teammate previewing) and only with words. A team member's search page has plain links.
- **What a row holds.** `query` is `textmask.Mask` (emails, runs of 6+ digits) with spaces collapsed;
  `query_key` is `FoldForAIRetrieval` of it (case and accents), the report's grouping. Words empty once
  masked keep nothing. No address, cookie, user agent or email is stored, so a row names no reader.
  `help_center_id` is the help center that served the search (`v.site.HelpCenterID`, written as `$H`
  through `onHelpCenter`; "" is the primary). The FK `(workspace_id, help_center_id)` refuses a help
  center of another workspace, and deleting a help center deletes its searches. Searches kept before
  the column belong to the primary. The report and the export still cover every help center of the
  workspace together, and the export carries no help center id: it carries only the primary.
- **The signature.** `HMAC-SHA256(kb_search_link_key, workspace_id + "\x00" + id)`, cut to 16 base64url
  characters (96 bits). The key is a server secret (`ServerSecret`) like the vote key, shared by every
  replica, never exported. A link of another workspace, a forged one or one older than 24 hours
  (`kbSearchOpenWindow`) changes nothing; only the first open of a search counts. The update joins
  `kb_articles` in the same workspace.
- **Cache.** A kept search is a new row each time, so its page has no ETag and is never a 304; it is
  `private, max-age=300`, so Back in the reader's browser shows the same links without a new row and a
  shared cache never stores it. A browser that prefetches result links, or a 304 from an earlier
  page, undercounts opens; the report never overcounts.
- **The cap.** The search limiter (per client address) is unchanged. On top of it a workspace keeps at
  most `KBSearchesPerMinute` (120) searches a minute, with one `INSERT … SELECT … WHERE count < cap`:
  the count and the insert read one snapshot, so searches at the same instant can pass it by as many
  as run at once. A search past the cap, or one whose write failed, is still answered; it is only not
  kept and its links carry no `s`.
- **Retention.** `DeleteOldKBSearches` (`PrivilegeKBSearchRetention`) deletes rows older than
  `kb_searches.retention_days` (default 180, 1 to 3650, `kb_search_retention_invalid`), at start and
  daily from `serve`. The FK to the article is `ON DELETE SET NULL`: a purged article leaves the search.
- **The report.** `GET /api/v1/kb/searches/report` and `colliedesk_kb_search_report`, `kb_articles:read`.
  The last `KBSearchReportDays` (7) up to the read, three lists of at most `MaxKBSearchReportRows` (20)
  groups: top, no results (`results = 0`), nothing opened (`results > 0`, nothing opened). Each group
  shows its newest words and result count. A search shows as soon as its row is written.
- **Export.** `kb-searches.ndjson` (query, results, opened article, `created_at` UTC), after the article
  views; `query_key` is not exported, it is derived.

## Gaps

Migration `20261122170943-kb-gaps`; code `store/kb-gaps.go` (list, read, change),
`store/kb-gaps-worker.go` (`BuildKBGaps`, `RunKBGaps`), `store/kb-gaps-sources.go` (what each kind
reads), `store/kb-gaps-place.go` (filing a signal, the automatic fix), `store/kb-gaps-match.go`,
`store/kb-gaps-topics.go`, `httpapi/kb-gap-handlers.go`, MCP `catalog-kb-gaps.go`, web
`features/kb/gaps-page.tsx` and `gap-signals.tsx`.

- **Triggers, not handlers.** Every signal comes from a trigger on its source table
  (`kb_searches`, `kb_article_votes`, `ai_chat_sessions`, `ai_chat_turns`, `ai_chat_feedback`,
  `kb_articles` on publish) that queues a row in `kb_gap_pending`; the worker reads the source, masks
  and folds the text and files it. A new place that writes a source needs no gap code.
- **The reader writes votes.** The help center reader has no grant on the gap tables. The vote
  trigger's function is `SECURITY DEFINER`, owned by the app role with a fixed search path, so the
  reader fires it and the app role's workspace isolation still binds the row it queues. It queues
  only inside the voting workspace's own transaction. Never widen the reader's grants for a gap.
- **Agent-side tables.** `kb_gap_pending`, `kb_gaps` and `kb_gap_signals` use
  `secure_workspace_table`, not `secure_help_center_table`: they cover every help center of the
  workspace and the public help center never reads them.
- **Workspace deletion.** Every trigger that writes guards with `WHERE EXISTS` on `workspaces`, so a
  cascade from a workspace being deleted writes nothing; the chat delete trigger likewise skips the
  recount while its workspace goes.
- **The worker.** `serve` runs `RunKBGaps` every 30 s. One transaction at a time changes a
  workspace's gaps (`lockKBGaps`, a transaction advisory lock); another process skips that workspace.
  A failing workspace keeps its queue and does not hold up the others. A source still held by
  another transaction stays queued.
- **Matching.** `kbGapScore` weighs matched words by length (a word of 4 or more characters matches
  one it starts); `KBGapMatchThreshold` (0.6) joins a signal to a gap. Candidates come from the GIN
  index on `kb_search_vector(query_key)`, gaps of every status, so a dismissed gap keeps counting.
- **The title.** `kb_gap_recount` sets the title to the most common text of its signals, the newest
  on a tie, and recounts askers, first and last seen. A gap left without signals is deleted. Asker
  keys never leave the server.
- **Fixed and reopened.** Publishing an article whose title scores at least the threshold against an
  open gap marks it fixed automatically. A fixed gap asked for more than 7 days after the fix
  (`kbGapReopenAfter`) opens again; a person's change clears `fixed_automatically`.
- **Recurring topics.** Once a day per workspace, only with at least 20 conversations resolved in 30
  days (`kbGapTopicsMinConversations`), a resolved conversation whose subject no published article
  title matches becomes a signal. No AI is called.
- **Retention and export.** Signals stay after their search ages out or their article is purged
  (they are already masked). A deleted chat deletes its signals. Gaps are not in the export.

### Drafting an article from a gap

Code: `ai/kb_gap_draft.go` (`DraftFromKBGap`), `httpapi/kb-gap-draft-handlers.go`,
`store/kb-article-gap-source.go`, `store.AIRetrieveConversations`, MCP `colliedesk_kb_gap_draft`, web
`features/kb/article-sources.tsx`.

- **Same plumbing as a reply draft.** The draft slot's model, the same allowance gate and usage row,
  and the same refusals (`writeAIError`: 409 `ai_feature_off`, 402 `ai_allowance_reached`). The gate
  runs before the gap is read, so with AI off an unknown gap answers 409, not 404.
- **Only masked text reaches the model.** The gap's signals are masked when filed; conversations come
  from `ai_retrieval_docs` (masked when indexed), resolved only, this workspace only, at most
  `KBGapDraftConversations` (8). Customer text sits inside `<untrusted_customer_content>` and
  `<past_conversation>`, framed as data in the system turn, like the inbox draft.
- **One paid answer per press.** The route goes through `createOnce` keyed on the gap and the
  collection; a refusal is never remembered (`errAnswered`), so the same key after a failure asks again.
- **The source is on the article.** `source_meta.gap` holds `id` and `conversation_ids`, best match
  first; answers carry `gap_source` (`gap_id`, each conversation's `display_ref` and subject). `addressKBArticleTranslations`,
  which every whole-article answer passes, empties the list for a caller without `conversations:read`.
  A conversation deleted later drops out of `gap_source`; its id stays in `source_meta`. In MCP,
  `colliedesk_kb_gap_draft` and `colliedesk_kb_article_get` carry it (`kbGapSourceView`, each subject
  under `untrusted_customer_content`); the other article tools leave it out.
- **Trap: `source_meta` used to mean imported.** The primary-language check of a Freshdesk import
  counted any article with `source_meta` as imported. It now counts one with a `gap` key as the
  team's own; a new `source_meta` key that is not an import needs the same check.
- **Never published by AI.** The article is always created as `draft`; publishing is the normal call
  with `kb_articles:publish` and the review gate.

## Views and uses

Migration `20261121091907-kb-article-views-and-uses`; code `pageview/` (who is a reader, the event),
`helpcenter/page-views.go` (emits it), `kbviews/counter.go` (counts in memory), `store/kb-article-views.go`
(the write), `kblinks/find.go` and `store/kb-article-uses.go` (uses), `store/kb-article-stats.go`
(`GET /api/v1/kb/articles/{id}/stats`), MCP `catalog-kb-stats.go`, web `features/kb/article-stats.tsx`.
Results reads the same tables (docs/internals/results.md › Help center).

- **One rule for a reader.** `pageview.IsReader` is the only check: GET (not HEAD), status 200, not a
  bot (`pageview/bots.go`, one list, matched on the lower-cased User-Agent; an empty User-Agent is a
  bot), not a prefetch (`Sec-Purpose` or `Purpose` naming `prefetch`), not the team. A later counter or
  analytics consumer takes the same `pageview.Event`, so two numbers never disagree on what a visit is.
- **HeadlessChrome is a bot.** It is in the list, and Playwright's default User-Agent says it: an e2e test
  that expects a view sets a desktop browser's User-Agent.
- **The team is the session cookie.** `Config.Team` is `signedIn`: a request that carries a Colliedesk
  session cookie is a team member previewing, without a database read on the public page. A reader who
  is also signed in to the app in the same browser is not counted; that is the price of no query.
  The cookie is host-only, so on a custom domain or a `<slug>.<domain>` host the browser does not send
  it and a signed-in team member's view counts as a reader's.
- **Only article pages count today.** Home, collection and search pages emit the event too (with the
  search words); the counter keeps only `Kind == Article`. Help-chat-only and agents-only articles answer
  404 and emit nothing.
- **A reader once per 30 minutes.** The reader key is `HMAC-SHA256(day key, client address, UA, local
  day)`, per article; the client address is the one the site already trusts (`Config.Client`, else the
  connection's). The key is random, lives in memory only and changes at each UTC day (`Options.NewKey`,
  injectable with the clock `Options.Now`); the readers of the old key are forgotten with it.
  At most 200,000 readers are remembered (`maxReaders`, `Options.MaxReaders`), so rotating User-Agents
  cannot grow the memory. A full memory first forgets readers older than 30 minutes (at most once a
  minute, `pruneFullEvery`); a new reader it still cannot remember is not counted, and the next flush
  logs one warning with how many views were left out. The cost is an undercount under a flood, never a
  reader counted once per page.
- **Never on the page's path.** `PageViewed` only adds to a map under a mutex. `Run` writes every 30 s
  (`FlushEvery`, `COLLIEDESK_E2E_VIEW_FLUSH_MS` in the e2e build only) and once more after
  `server.Shutdown` with 5 s of its own (`stopServing`), even when the shutdown failed or used all of its
  10 s; a failed write puts the counts back for the next one. A server
  killed with SIGKILL loses at most 30 s of views.
- **One statement, privileged.** The write is one `INSERT … SELECT FROM unnest(…) JOIN kb_articles … ON
  CONFLICT DO UPDATE SET views = views + EXCLUDED.views` across workspaces, under
  `PrivilegeKBArticleViews`. The join skips counts of an article purged since the view; the others still
  add up. `day` is the calendar day in the workspace's timezone at the view (`workspaces.timezone`);
  changing the timezone later does not move stored days.
- **Nothing about the reader is stored.** `kb_article_views` is (workspace, article, locale, day,
  views). The address, User-Agent and referrer never leave memory.
- **A use is a sent reply.** `recordKBArticleUses` runs inside the transaction that inserts an outgoing
  message (a note, a notice or an unsent draft never does). `link` is an article address in the body on
  one of the workspace's own help center hosts only (`kblinks.Site`): `/help/<slug>/a/<number>-…` on this
  server's origin, `/help/a/<number>-…` on `<slug>.<HELP_CENTER_DOMAIN>`, any path on a verified custom
  domain. A host that merely starts with the slug, or the help center path on another host, is not one.
  An old Freshdesk address (`/support/solutions/articles/<id>`) counts on those hosts and on the hosts the
  workspace imported from (`import_jobs.domain` and `options.old_help_center_domain`), when the import
  ledger maps the id (`import_items`, `kb_article`, status `done`). An import run with the CLI leaves no
  `import_jobs` row, so its old Freshdesk addresses are not counted: an undercount, never a false use. `ai_source` is each
  `ai_draft_sources` article of the draft the reply was sent from. One row per message, article and
  kind; a reply that both links and quotes an article has two rows and is counted once (`count(DISTINCT
  message_id)`). A reply whose delivery fails later still counts: the agent used the article.
- **A trashed article keeps its stats.** The stats route reads it; a purge cascades the rows away.
- **The stats route** is `kb_articles:read`. Without `from`/`to` it is the last 30 days, today included
  (`KBArticleStatsDefaultDays`); with them, the days `[from, to)` in the workspace timezone, at most 366.
  It lists the newest 50 replies (`MaxKBArticleStatsReplies`) and `replies_total`. Another workspace's
  id is 404, like an unknown one. `colliedesk_kb_article_get` carries `views_30d` and `uses_30d` from
  the same read.
- **Export.** `kb-article-views.ndjson` (article, locale, day, views) and `kb-article-uses.ndjson`
  (article, conversation, message, kind, `created_at` UTC), after the votes.

## Analytics

Migration `20261121171648-help-center-analytics`; code `hcanalytics/` (check, queue, send, test event),
`store/help-center-analytics.go`, `httpapi/help-center-analytics-handlers.go`, MCP
`catalog-help-center-analytics.go`, web `features/kb/help-center-analytics.tsx`. User page:
docs/user/knowledge-base.md › Analytics.

- **Server-side, never a script.** The sender is one more `pageview.Consumer` next to the view counter
  (`pageview.Fanout` in `cmd/collie/generation.go`), so it sends exactly the visits `IsReader` counts.
  The public pages, their headers and their CSP are byte for byte the same with analytics on or off
  (`TestTheHelpCenterPageIsByteIdenticalWithAnalyticsOnAndOff`); a help center source still names no
  outbound client (`TestPublicPagesMakeNoExternalCall`).
- **Never on the page's path.** `PageViewed` reads the target from memory and puts the delivery in a
  buffered channel of 10,000 (`Options.QueueSize`) without waiting: a full queue drops its oldest
  delivery and counts it (`Dropped`). Four senders (`Options.Senders`) drain it with a 5 s timeout per
  request (`Options.Timeout`). A hanging service never slows a page
  (`TestAPageNeverWaitsOnAHangingAnalyticsServiceAndTheFailureIsShown`). Queued deliveries are lost at
  shutdown; the results are flushed.
- **Targets live in memory.** `Run` loads the enabled targets at start and every 30 s (`FlushEvery`)
  under `PrivilegeHelpCenterAnalytics`, which lists only workspace ids; each target and its sealed API
  secret are read in their own workspace. A save calls `Reload` for its workspace at once.
- **Old settings never send.** Each `Reload` raises a generation and stamps its workspace; a full
  load keeps what a later `Reload` loaded, since its own read may hold the settings before that save
  (`reloadAll`). A delivery carries the target it was made for, and a sender checks it is still the
  loaded one before the send, before a retry and before recording the result (`current`): a save that
  turns analytics off, removes the secret or changes the service drops what waits with the old
  settings. Only a request already on the wire can still arrive.
- **One failing service never holds the others.** `PauseAfter` (5) failed sends in a row pause a
  workspace for `PauseFor` (5 min): its page views are not queued, it is logged, and the failure that paused it is the last error.
  New settings end the pause. A retry goes back in the queue only when there is room, since a newer
  page view is worth more; a full queue logs how many it dropped at each flush.
- **Retries and results.** A failure is queued again after 10 and 30 s (`RetryDelays`). The last
  accepted send and the last error per workspace are kept in memory and written in one
  `UPDATE … FROM unnest` each flush and at shutdown; a failed write merges them back for the next
  flush. A target carries `Version` (`updated_at` in microseconds) and the UPDATE matches
  `h.updated_at = r.version`, so a result of older settings never lands on the ones saved after
  them. A save clears the results. Times are returned in UTC.
- **A target the store cannot read** comes back with only `Problem` set: a secret no key of
  `MAILBOX_TOKEN_KEYS` opens, or a read error in the full load. The full load keeps the target already
  loaded; with none, the problem becomes the last error. The test answers 409
  `analytics_secret_unreadable` with the same words. A full-load problem has no version, so it is never
  written.
- **Reader ids.** GA4's `client_id` is `HMAC-SHA256(day key, workspace + client address + UA)`, the
  `session_id` comes from the same sum; the key is random, in memory, new each UTC day (as the view
  counter's). Two workspaces never see the same id for one reader, and GA4 counts one session per
  reader per UTC day. GA4 never gets the address or the UA. Plausible needs them to make its own daily
  id, so they go in `X-Forwarded-For` and `User-Agent` of the request and nowhere else.
- **Decided, not built.**
  - A save whose `Reload` fails still answers 200 and logs it: the settings are saved, and the next
    full load (30 s) applies them.
  - With several server instances, the others pick a save up at their next full load, within 30 s.
  - The full load reads each enabled workspace in its own transaction (N+1). It runs every 30 s,
    only for enabled workspaces, and keeps each secret opened in its own workspace.
  - A team member is recognised by the `colliedesk_session` cookie alone, as the view counter does
    (#347). On a custom domain or `<slug>.<domain>` that cookie is absent, so their views are sent too.
  - The test event may go to any public `https` host and port. It needs `help_center:update`, the
    address must pass `netguard` and its errors name no answer body, so it is not limited to port 443.
- **Outbound guard.** The client dials through `netguard`, so a Plausible base URL whose name resolves
  to a private, loopback or link-local address is refused at send; `CheckBaseURL` refuses such a literal
  address, `localhost` and plain `http` at save. Redirects are not followed. Tests reach a local stub only
  through `Options.AllowDial`.
- **The secret.** Sealed with the key ring under the context `help_center_analytics:ga4_api_secret:<workspace>`
  (`ErrSealKeysMissing` without `MAILBOX_TOKEN_KEYS`); never in an answer, a log or the export (only
  `ga4_api_secret_set`). GA4 on without a secret is refused (`hcanalytics.SecretMissing`) and the
  transaction rolls back.
- **The test event** goes out at once, outside the queue, as the person (their UA and address), for the
  page `/colliedesk-analytics-test`. GA4 is first sent to `/debug/mp/collect`, which names a bad
  measurement ID but never checks the API secret, then to `/mp/collect`. Plausible answers 202 to
  everything; `X-Plausible-Dropped` means its bot filter dropped the event.
- **Export.** `help-center.ndjson` has `analytics`. Saving analytics creates the workspace's
  `help_centers` row (off) when there is none, as Search Console's save does, so the line is written.

## Translations

Migration `20261105100000-kb-translations`; code `store/kb-translations.go`, `httpapi/kb-translation-handlers.go`,
MCP `catalog-kb-translations.go`. Product decision (2026-11-05): a translation is a language version of
the same article, as Freshdesk does it, never an article of its own.

- **One row per article and language** (`kb_article_translations`, key workspace, article, locale),
  with its own slug, status, search fields, `outdated` flag and soft delete. The key stays, so a
  translation in the trash blocks a new one in that language (`translation_exists`): restore it.
- **Versions in a table of their own** (`kb_article_translation_versions`), append-only like
  `kb_article_versions`. Not a `locale` column on the article's versions: every "newest version of the
  article" read (import rerun and undo, `last_change`) would then see translation versions. The
  migration gives every existing translation version 1 of kind `import`; a translation with no version
  at all is treated as the import's (`KBTranslationImportedLast`).
- **Languages.** The primary language is `help_centers.locale`; the others are `help_center_languages`
  (visible false by default, so the upgrade adds every imported language hidden and changes nothing
  public). Creating or publishing a translation needs a listed language other than the primary one;
  a translation already in an unlisted language (an import from an account with another primary
  language) stays readable, editable and deletable and is listed as `unlisted`. Dropping a language
  that holds a translation or collection name (trash included) is refused (`language_in_use`), so a
  translation is never left without its language by a settings change.
- **Outdated is set in Go, not by a trigger.** `markKBTranslationsOutdated` runs in `changeKBArticle`
  (edit, version restore) and `ReimportKBArticle`, comparing the title and text before and after. A
  media replacement and an import's link rewrite change the article and its translations alike, so
  they must not mark anything outdated; a trigger on `kb_articles` could not tell them apart. Clearing
  is explicit (`PATCH outdated false`), recorded as `mark_current`, or with an edit in the same call.
- **Publishing a translation needs `kb_articles:publish`** whenever it is or becomes published, like
  an article. Marking it up to date or outdated alone does not change what customers read and does not.
  Showing or hiding a language does (`HelpCenterLanguagesShownChange`).
- **Media.** `linkKBMedia` already reads every translation's text, so images of a translation are
  linked to the article and survive the purge; a media replacement records a `media_replace` version
  of each translation it rewrote.
- **Collection names** (`kb_collection_translations`) have no versions, like collections. `source_meta`
  is set while a row is as an import wrote it; a person's write clears it. The import adds a missing
  name back on every run and replaces only its own, so deleting an imported name is undone by the next
  run (owner decision 2026-11-05), and undo removes only rows still the import's.
- **Purge.** `PurgeKBTrash` deletes translations 30 days in the trash (`PrivilegeKBPurge`); versions
  go by cascade, as do an article's translations with it.

## Editor (web app)

- **Lazy, all of it, and the editor apart.** The first bundle carries only the rail link and the three
  routes; the list, the article page, the preview with `remark-gfm` and their styles are the `kb-chunk`
  part, the editor is a chunk of its own (`features/kb/editor-part.ts`), and the Help center card in
  Settings is a small part of its own (`features/kb/kb-part.tsx`). `react-markdown` is already in the
  first bundle for messages, so the preview shares it. The list never loads the editor; an article page
  starts it with the article, and a pointer or the focus on the list or on New article preloads it.
  The first bundle's Tailwind scan skips `features/kb/editor/` (`@source not` in `styles/base.css`):
  words in its code (`italic`, `underline`) would otherwise become utilities in the first CSS.
- **The Markdown editor is CodeMirror 6 on Markdown source, byte for byte** (`editor/codemirror-editor.tsx`):
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
- **A lost race keeps the person's text and shows both.** On `changed_elsewhere`, from a save or an
  autosave, the editor and a translation's form show the two texts line by line (`conflict-compare.tsx`,
  the draft's `line-diff.ts`) with "Save mine over theirs" (reads the item again and saves with its
  `updated_at`; theirs stays in the history) or "Discard mine and show theirs". Autosave stops until
  the person picks one. Every save takes the answer's `updated_at`, so a second save does not race itself.
- **Autosave runs 3 s after typing stops, and on blur, `visibilitychange` to hidden, `pagehide` and
  coming back online** (`use-autosave.ts`, `article-autosave.tsx`, `translation-autosave.ts`). A new
  article or translation (no row yet) is not autosaved: it needs a Save to exist. A translation sends
  only `title` and `body_md` with `autosave: true`, keeps no device copy, and its other actions wait
  for an autosave in flight so they never send the same `updated_at`. A save while the page
  closes uses `keepalive` below 60,000 bytes (the browser holds at most 64 KiB of keepalive bodies);
  a longer text relies on the device copy.
- **The device copy is per workspace, person and article.** `unsaved-store.ts` keys it
  `colliedesk:kb-unsaved:<workspace>:<agent>:<article>`, so a shared browser never offers one
  person's text to another or across workspaces; the editor waits for `/me` before it reads it. It is
  written on every change and removed once a save is confirmed, unless the text changed after that
  save. On open it is offered back only when it is newer than the server copy and differs from it.
- **Full screen is a class on the editor, not the Fullscreen API**, so the toolbar, dialogs and menus
  stay in the same document. Escape leaves it, except when a dialog, menu or link search is open: each
  prevents the Escape it handles, and the editor leaves full screen only on an Escape nobody took.
- **Pasted HTML becomes Markdown with the Freshdesk import's rules** (`editor/paste-html.ts`, ported
  from `server/internal/freshdesk/solutions-markdown.go`). Both sides run the cases in
  `server/internal/freshdesk/testdata/html-markdown-cases.json` (vitest and
  `TestArticleMarkdownMatchesTheSharedPasteCases`), so a rule change in one fails the other. The paste is
  stricter than an import: players, frames, forms and SVG are dropped, an image keeps only an
  `http(s)`, `data:` or `blob:` address, underline and colours become plain text (on a copied page
  they are styling, rarely meaning; the person adds them with the tools), styled spans become bold or italic only from weight and style, and a table without a
  header row gets an empty one (GFM needs one). A relative image address from a web page stays as
  written and shows as a broken image. The result is Markdown, rendered later by kbhtml's allowlist,
  so no pasted tag reaches the help center that a typed one could not.
- **Plain text wins in code and from code editors.** Inside a fenced block or inline code, and for a
  clipboard that carries `vscode-editor-data`, the paste is the plain text. In the Markdown editor, after a
  formatted paste a bar offers "Keep formatting · Paste as plain text"; each swaps the inserted range, as one undo step.
- **Pasted images go to the media library.** `data:` and `blob:` images are uploaded like a dropped
  file (type and 5 MiB checked first); a web address goes to `POST /api/v1/kb/images/import`, which the
  server downloads through the same `netguard` dialer as mail images: private, loopback, link-local and
  metadata addresses are refused after DNS and on every redirect (403 `address_refused`), with the
  same size and type limits as an upload. An image that fails is left out of the text and the editor
  says why; the rest of the paste goes in. The e2e pastes a `data:` image because the guard refuses the
  test server's own loopback address; the unit tests cover the web address path.
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

### Visual editor

- **Two editors on one stored text.** `body_md` stays Markdown; the visual editor
  (`editor/visual/`, Tiptap 3 on ProseMirror, a chunk of its own with no CodeMirror) reads it into a
  document and writes it back. Visual is the default; the choice is the person's, stored on the server
  (`agents.kb_editor`, `PUT /api/v1/agents/me/kb-editor`, read in `GET /api/v1/auth/me`), so it follows
  them to every device. Both editors share one toolbar (`editor/editor-frame.tsx`), one handle
  (`markdown()`, `focus()`), autosave and the conflict view, so the page does not know which one runs.
- **The schema holds exactly what can be stored** (`visual/schema.ts`): CommonMark and GFM blocks and
  marks, callouts, images, and the formatting forms below. No node exists that Markdown and those forms
  cannot write, so the visual editor never saves something the source could not hold.
- **What it cannot show stays as written.** `markdownToDoc` (`visual/from-markdown.ts`) turns a top-level
  block it cannot show as itself (raw HTML other than the forms, a footnote, a link reference
  definition, a video, a table that is not a grid) into a read-only Markdown block: a dashed box with the
  source, saved byte for byte. Converting it would rewrite what an import or an agent wrote; the person
  edits it in Markdown.
- **Conversion goes through mdast.** Reading uses the micromark/mdast parse (`mdast-util-from-markdown` with GFM) that the preview's remark uses; writing
  builds mdast and prints it with `mdast-util-to-markdown` (`visual/to-markdown.ts`), with the forms
  written by `tagString` in the server's spelling. Three gates keep it honest:
  `server/internal/kbformat/testdata/visual-editor.md` holds every element, and the visual editor saves it
  to the same bytes (`convert.test.ts`) and `Normalize` leaves it as it is
  (`TestNormalizeKeepsWhatTheVisualEditorWrites`); any text saved twice changes no more after the first
  save; and a corpus of other spellings (setext headings, `__bold__`, `*` and `+` lists, indented code)
  renders the same page after a visual save (`save-shows-the-same.test.tsx`), though its source changes
  to the editor's spelling. While the document equals what it read, `markdown()` answers the stored text
  itself (`visual-editor.tsx`), so opening an article in the visual editor and leaving it sends nothing.
- **A table stays GFM when GFM can hold it** (`visual/to-markdown-table.ts`): one header row over plain
  rows, every cell one paragraph with no hard break, merge, background, alignment or indent of its own,
  and each column one alignment. Anything else is written as an HTML table in the forms below. A GFM
  table is what an agent or a person reads most easily in the source, so it is never given up for less.
- **Align and indent belong to top-level paragraphs and headings**, and to selected cells of a table in
  the visual editor. A wrapper inside a list item, a quote or a callout would need HTML around Markdown
  inside a container, which CommonMark parses differently by indentation; the tools are off there. The
  server still keeps a wrapper it finds inside one (`normalize.json`), so nothing typed by hand is lost.
- **A paste in the visual editor uses the Markdown editor's converter** (`visual/visual-paste.ts`,
  `pastedMarkdown`), then reads the Markdown back: what shows after a paste is what is saved. There is no
  "Keep formatting" bar: `Ctrl+Z` takes the paste back and `Ctrl+Shift+V` pastes plain text.
- **Side by side is Markdown only.** The visual editor already shows the page; a preview beside it
  would show the same thing twice. A stored Side by side falls back to Write there.

## Formatting beyond Markdown

The help center keeps a small set of HTML forms beyond Markdown (`server/internal/kbhtml/allowlist.json`,
`"formatting"`), so the visual editor can underline, highlight, colour, align, indent and merge cells
without a `style` attribute: the help center's CSP and its page CSS stay as they are, and the colours are
the theme's classes (`kb-color-*`, `kb-bg-*` in the help center's `site.css`), right in light and dark and
in a workspace's custom CSS.

| Form | Means |
| --- | --- |
| `<u>…</u>`, `<mark>…</mark>` | underline, highlight |
| `<span data-color="accent\|success\|warning\|danger\|muted">…</span>` | text colour from the palette |
| `<p data-align="center\|right" data-indent="1..4">`, blank line, Markdown, blank line, `</p>` | a top-level paragraph's alignment and indent; `<h1>`…`<h6>` the same for a heading |
| `<table>` with `<th>`/`<td>` and `colspan`, `rowspan` (each up to 20), `data-align`, `data-bg` | a table GFM cannot hold |
| `<figure data-width="small\|medium\|full\|1..4096" data-align="left\|center\|right\|inline" data-border>`, blank line, an image line (or a link holding only an image), optional `<figcaption>` with Markdown between blank lines, `</figure>` | an image's size, position, frame, caption and link |

- **One spelling.** `kbformat.Normalize` (`server/internal/store/kb-body.go`, on every save of an article,
  translation, draft or template) lowercases the tags, writes double quotes, keeps only those attributes,
  in the order `colspan`, `rowspan`, `data-align`, `data-indent`, `data-color`, `data-bg` (a figure's:
  `data-width`, `data-align`, `data-border`), drops a colour outside the palette, a span of 1, and a figure
  width or alignment outside `figure` in the allowlist (a width with a leading zero too). The editors write that spelling (`editor/format-tags.ts`
  `tagString`), so a save never changes what the person sees in the source. The cases are
  `server/internal/kbformat/testdata/normalize.json`, run by Go and by vitest (`format-tags.test.ts`).
- **One render.** `kbformat.Extension` renders the forms in the help center's goldmark with the classes
  above (`kb-align-*`, `kb-indent-*`, `kb-color-*`, `kb-bg-*`); the preview and the visual editor give
  the same classes their look in `editor/formatting.css`. `testdata/render.json` is the expected HTML of
  each form, run by `server/internal/helpcenter/formatting_test.go` and by vitest
  (`markdown-preview.test.tsx`).
- **A figure's classes.** `kbformat` (`figure.go`) renders `kb-figure`, `kb-figure-<width>`,
  `kb-figure-<align>` and `kb-figure-border`; a width in pixels becomes the image's own `width`
  attribute, the one dimension the sanitizer already allows, so no `style` is needed and the CSP stays
  byte-identical. A run that breaks the shape is left as plain blocks, so its image and caption still
  show. A figure set back to Original size and Default stays a bare `<figure>` (rendered as `kb-figure`
  only); the editor never unwraps it, so the image keeps its place.
- **Annotate is a copy plus a one-article replace.** The dialog (`features/kb/annotate/`) draws on a
  canvas at the image's natural size from its own bytes (an object URL, so the canvas is never tainted).
  Blur is `pixelate` over `getImageData`, burned into the exported bytes, never a layer the reader's
  browser draws. Saving uploads the copy (JPEG for a JPEG, else PNG), then
  `POST /api/v1/kb/media/{id}/replace` with `article_id`, which changes that article and its
  translations only. The replacement row is the copy's history: `GET /api/v1/kb/media/{copy}` lists the
  original under `earlier_versions`, and undoing the replacement puts it back. The editor refuses
  while the text on screen differs from what is saved, since the replace rewrites the saved text.
  When the replace is refused, the editor moves the copy to the trash (it is in no article) and the
  dialog keeps the drawing and shows the refusal.
- **A one-article replace never changes the help center.** With `article_id`, a published article and
  its published translations keep their text: the new item goes into their unpublished changes, made
  from the published text when there are none (`replaceInOneKBArticle`), so the call needs
  `kb_articles:update`, not `kb_articles:publish`. A draft article is rewritten as a new version. The
  article is locked before its text is read, so a save that drops the item meanwhile is seen; when
  nothing is rewritten the call answers 409 `media_not_in_article` and records no replacement. Such a
  replacement lists the published article with version 0, so its undo needs no publish either
  (`KBReplacementTouchesPublished` counts only versions above 0).
- **A figure's lines are set apart.** An HTML block runs to the next blank line, so a `<figure>` or
  `<figcaption>` tag with the image or caption right under it would show that Markdown as text. The
  normalizer (`kbformat/figure-apart.go`) puts a blank line before and after each such tag of a
  top-level block; code and figures inside a list item or a quote are left as they are. Titles of the
  image and of its link (`![a](src "t")`, `[…](href "t")`) round-trip through the visual editor
  (`linkTitle` on the figure node).
- **A replacement not undone keeps its earlier version.** `kbMediaKeptByReplacement` (a row of
  `kb_media_replacements` with this `from_media_id`, `undone_at IS NULL` and not `in_place`) keeps the
  item from every purge: `PurgeKBMediaNow` deletes nothing and answers 409 `media_kept` with the
  articles and the replacement in `kept_by`; `kbMediaPurgeable` skips it, so the trash shows no
  `purge_at`. It may still go to the trash. The guard lifts once the replacement is undone. An in-place
  file replacement keeps no earlier bytes, so it keeps nothing.

## Files

- **A file is a media library row of kind `file`.** It shares the library's history, trash, purge,
  Undo and storage count, and its bytes live in the same content-addressed storage. `kb_article_attachments`
  (RLS, `workspace_id` in every key) lists files on an article (`translation_id` null) or a translation
  by `position`. A draft carries its own `attachment_ids`; publishing copies them to the live rows, so a
  published article's readers never see a list that is not published. A translation's list changes at
  once, as its text does on a save, and a published one needs `kb_articles:publish`.
- **The type comes from the name and the bytes together** (`media.InspectFile`): the extension picks
  the type, and the file must start with that type's magic bytes (PDF, RTF, OLE2 for doc/xls/ppt, ZIP
  for OOXML, OpenDocument and zip, 7z, gzip); CSV and TXT must be UTF-8 with no NUL. The content type
  comes from the table, never from the client, so an `.exe` renamed `.pdf` is refused before it is stored.
- **Public only while listed.** `/files/{id}/{slug}` looks the file up by id; the slug is for people.
  The query joins a published, public article or translation that lists it, so a draft, an unpublished
  article, a removed entry or another workspace's id answers 404. Answers carry
  `Content-Disposition: attachment` with `filename*=UTF-8''`, `nosniff` and
  `default-src 'none'; sandbox`, and revalidate (`no-cache`, ETag = SHA-256), so a replaced file is
  served new on the next request.
- **Text for search, not for export.** The first 200 KiB of a PDF (`github.com/ledongthuc/pdf`), TXT or
  CSV goes into `kb_media.text` and its `text_search`; the help center search and AI retrieval match it
  only through a listing article. The export leaves the text out; a restore reads the files again.
- **Replace keeps the id.** A file is replaced in place by a file no article lists: the two rows trade
  their bytes, so every list and address of the first keeps its id and the second holds the earlier
  version in the trash for Undo. Images and videos are still replaced article by article.

## Templates

Migration `20261120141816-kb-article-templates`; code `store/kb-templates.go`, `store/kb-template-history.go`,
`httpapi/kb-template-handlers.go`, MCP `catalog-kb-templates.go`, web `features/kb/templates-page.tsx` and
`article-template.tsx`. Issue #678.

- **A template is copied, never linked.** Creating an article copies the body once; the article keeps
  no reference to the template, so editing or trashing a template can never change an article
  (`TestAnArticleCreatedFromATemplateCopiesItsBodyOnce`). `POST /api/v1/kb/articles` with both
  `body_md` and `template_id` is a 400; with neither it uses the collection's `default_template_id`.
- **History and trash copy saved replies.** Versions are append-only for the app role (the migration
  revokes UPDATE and DELETE on `kb_template_versions`), the trash lasts 30 days, writes are optimistic
  with `updated_at`, and the 200-template limit counts the trash. Nothing deletes a template for good
  before then, so the limit refusal says to edit one instead.
- **Trashing a template clears the collections that start from it** in the same transaction
  (`default_template_id` set to null), so a collection never names a template in the trash. A trashed
  template named by `template_id` or `default_template_id` is a 409 `template_in_trash`; a `GET` of one
  is a 404 outside `/trash`.
- **Starters are seeded once per workspace.** `kb_seed_starter_templates` runs in the migration for
  every workspace and at workspace creation, and sets `workspaces.kb_starter_templates_at` first: a
  workspace that has the timestamp never gets starters again, so a starter the team deleted never
  comes back (`TestANewWorkspaceStartsWithThreeStarterTemplatesAndADeletedOneNeverComesBack`).
- **Never public.** No help center reader joins `kb_templates` (`TestNoPublicSurfaceShowsATemplate`).
  An image a template names is public only through a published article. `kbMediaNamedByTemplate`
  matches the media id in the text of a template outside the trash, so such an item counts as in use
  and is never purged as unused.
- **No Freshdesk import.** Freshdesk's API does not expose its article templates, so the importer
  brings none and the user docs say so.
- **Insert template is Markdown only.** It inserts at the CodeMirror cursor; it is hidden in
  **Preview**.

## Review and approval

Migration `20261120184320-kb-article-reviews`; code `store/kb-reviews.go`, `kb-review-gate.go`,
`kb-review-decide.go`, `kb-review-edits.go`, `kb-review-translations.go`, `kb-review-mail.go`,
`httpapi/kb-review-*handlers.go`, `kbreviewmail/`, MCP `catalog-kb-reviews.go`, web
`features/kb/review-*.tsx`, `reviews-page.tsx`, `approval-state.ts`. Issue #674.

- **The gate runs after the write, in its transaction.** `guardKBPublish` compares the text as it
  was and as it is: text that becomes live with a new hash needs an approved review of exactly that
  hash where any collection before or after the change requires approval. Every article and
  translation write calls it (`changeKBArticleLocked`, `CreateKBArticle`, `afterKBTranslationWrite`),
  so bulk publish, MCP and a status `PATCH` cannot skip it. Live text that keeps its hash passes:
  undeleting a published article brings back what was live
  (`TestUndeletingPublishedTextNeedsNoNewApprovalAndTrashedTextCannotChange`).
- **The hash is the whole reviewed text.** `KBContent.Hash` is the SHA-256 of title, slug, body, SEO
  title and description and the sorted collection ids. A move to another collection is a new text.
- **State is derived.** In review and Approved are not columns of the article: an open review, or an
  approved one whose hash is still the pending text. `approves_pending` caches the second for the
  Approved list and `settleKBReviewAfterEdit` keeps it right after every write.
- **Four-eyes is by person, not by token.** A decision compares the requester's agent id, so neither
  their session nor any of their tokens decides; the decider must be listed in `kb_review_reviewers`;
  `kbTextAuthors` refuses a reviewer who wrote the text since it was last published
  (`TestATokensDraftIsApprovedOnlyByAnotherTeammate`, `TestAReviewerMustBeAnotherTeammateWhoMayPublish`).
- **Who wrote a text is a table, not the history.** An autosave records no version, so
  `kb_text_writers` (one row per article, locale and person; locale NULL is the article's own text)
  is the only source `kbTextAuthors` reads. `noteKBTextWriter` runs in the writers every text write
  passes through (`addKBArticleVersion`, `recordKBArticleDraft`, the autosave branch of
  `changeKBArticleLocked`, `recordKBTranslationVersion`): a create, edit, restore, draft, import or
  media replace adds its person; a version with status `published` that is not a draft or a review
  event empties the list, so the next change starts from nobody. A review event adds no one. An import
  has no person and adds no one. The migration filled it from the versions with the rule they had
  (`TestKBTextWritersStartFromTheVersions`, `TestATeammateWhoOnlyAutosavedTheTextNeverReviewsIt`,
  `TestAnAutosavingWriterReviewsTheNextChangeAfterAPublish`). The workspace export leaves it out:
  it says who may review, not what was written.
- **An edit supersedes, then reopens without the editor.** The new review copies the sender and note,
  points at the old one (`supersedes_review_id`) and drops the reviewer who edited, so a second
  person still approves. With nobody left it stays superseded
  (`TestAnEditInReviewReopensTheReviewWithoutTheReviewerWhoEdited`).
- **A published translation is reviewed through its draft row.** A save writes the row and supersedes
  an open review; `…/translations/{locale}/draft/publish` needs an approved review of the row's hash
  where approval is required, else `ErrKBTranslationApprovalRequired`.
- **Mail goes through the outbox columns.** `kb_review_reviewers.email` and `kb_reviews.author_email`
  move `queued` → `sending` → `sent` (or `rejected`, or `off` for a deactivated person or a closed
  review); the `kbreviewmail` sender claims each row right before it sends, so two servers never send
  the same one, and a claim left `sending` goes again after a while
  (`TestEachReviewerIsEmailedOnceAndTheAuthorHearsTheDecision`, `TestAFailedSendGoesAgainLater`).
- **Not gated on purpose.** A media replacement already needs `kb_articles:publish` and changes every
  article that shows the item; a Freshdesk import writes the source's status. Neither opens a review.

## Visibility

Migration `kb-visibility`; code in `server/internal/store/kb-visibility*.go`. User page:
[knowledge-base.md](../user/knowledge-base.md#visibility).

- **One public definition.** `kb_articles.visibility` and `kb_collections.visibility` hold `everyone`,
  `help_chat` or `agents`. What a customer may read is decided only by the `security_invoker` views:
  `kb_article_visibility` (the effective rank), `kb_public_articles` (help center), `kb_help_chat_articles`
  (help chat) and `kb_published_articles` (team-side AI), with the `*_translations` views beside them.
  Forced row-level security still applies, since the views run as the caller.
- **Most restrictive wins, and it fails closed.** The effective rank is the greatest of the article's
  own, its home's and every live extra collection's, each with its live parents. A home that is not
  live counts as `agents`, and `kb_visibility_rank` ranks any unknown value as `agents`
  (`domain.KBVisibilityRank` agrees), so a mistake hides an article rather than showing it.
- **Raising is a publish.** Any write that may show a published article to more people (its own
  value, a collection's value or parent, publishing a draft that moves it, restoring a collection from
  the trash) takes a `kbVisibilitySnapshot` first and calls `guardKBVisibilityRaise` after the write,
  in the same transaction. An article whose rank fell, in a collection that needs approval, without an
  approved review of its published text, rolls the write back: 409 `approval_required` with the ids.
  The handlers ask for `kb_articles:publish` before a raise (`KBArticleVisibilityRaises`,
  `KBCollectionVisibilityRaises`); lowering needs only `kb_articles:update`.
- **A restricted page is a missing page.** The help center, its search, the sitemap, `llms.txt`, the
  `.md` copy and legacy addresses read `kb_public_articles`, so a restricted article is 404 (a legacy
  address goes to search) like an article that does not exist. Help chat quotes a `help_chat` article
  as "Our help notes" with no title, collection or address. AI drafts and triage read every published
  article, restricted ones included; a draft is given no article address, so it cannot link one.

### Traps

- **Never read `status = 'published'` for a customer.** `TestOnlyTeamSideFilesReadThePublishedStatusDirectly`
  fails when a file outside the team-side list (`teamSideKBFiles` in `kb_visibility_gate_test.go`)
  decides on its own that an article is published. Read through the views instead. Add a file to the
  list only when it serves the team's own screens or writes.
- **A new write that reshapes the tree needs the guard.** A path that changes a parent, a collection
  list or the trash state of a collection and skips `guardKBVisibilityRaise` can publish unreviewed text.

## Bulk actions

Migration `20261121202017-kb-bulk-operations`; code `httpapi/kb-bulk-handlers.go`, `kb-bulk-actions.go`,
`kb-bulk-undo-handlers.go`, `store/kb-bulk.go`, MCP `colliedesk_kb_articles_bulk` and
`colliedesk_kb_articles_bulk_undo`, web `features/kb/kb-bulk-*.ts(x)`, `use-kb-bulk.ts`. Issue #679.

- **Each article goes through the call for it alone.** `kbBulkOne` reads the article and runs the
  same store write (`UpdateKBArticle`, `publishOneKBArticle`, `RequestKBReview`, `DecideKBReview`,
  `DeleteKBArticle`) the single route runs, so the review gate, four-eyes, the visibility raise check
  and the trash rule cannot be skipped by a batch. The route is `byHandler` in `routeAccess`: it needs
  `kb_articles:update`, and the handler asks for publish or delete per action and per article.
- **A filter is the list's filter.** `filter` is parsed by `kbArticleFilter`, the parser of
  `GET /api/v1/kb/articles`, so the bulk action and the list never disagree on what matches. More than
  500 matches is refused before anything changes; the web app sends the filter, never the ids of the
  pages it loaded.
- **One undo record, one history entry per article.** `kb_bulk_operations` holds the batch and
  `kb_bulk_operation_articles` what undo puts back, with the article's `updated_at` just after the
  action (`after_token`). Undo leaves an article whose token moved since and lists it `not_undone`.
- **Not everything can be undone.** An approval, a publish of a draft over published text, an unpublish
  that made the draft the text, and an author change from nobody record no entry: the answer says
  `undoable: false`, and the article's history keeps the earlier version.

### Traps

- **The undo claim commits before the articles are put back.** `ClaimKBBulkUndo` marks the batch
  undone first, so two undos at once never both run. A crash after the claim leaves the batch marked
  undone with some articles not put back; the history of each article still shows both changes.
- **A move or a tag change of a published article goes to its draft.** When the batch made that draft,
  undo discards it (`discard_draft`) instead of writing the old values into it.
- **Undo of a trash restores the article at the end of its collection.** The restore follows
  `undelete`, which does not keep the old position.
- **COPY is refused on a table with row-level security.** The entries go in as `unnest` arrays.

## Traps

- **Default privileges grant UPDATE and DELETE on every new table.** The schema's default privileges
  give the app role all four rights, so `GRANT SELECT, INSERT` alone does not make a table append-only.
  The migration revokes all rights on `kb_article_versions` first; `TestKnowledgeBaseVersionsAreAppendOnlyForTheAppRole`
  pins it. A new table uses `limit_app_role` ([workspace isolation](workspace-isolation.md#securing-a-new-table)).
- **"Unused" is more than "no article".** The help center's logo and favicon (`help_centers.logo_media_id`,
  `favicon_media_id`) are library items no article links. `kbMediaNamedByHelpCenter` is part of the
  purge rule (`kbMediaPurgeable`), the `unused` and `in_use` filters, trash-unused and the admin's
  purge; a new query that decides an item is unused must add it, or it deletes a logo. The foreign
  keys clear the theme's column (`ON DELETE SET NULL (col)`) if a row goes anyway. `usage.help_center`
  and the trash list's `purge_at` read the same columns, so the library never says "not used" or
  shows a purge date for an item the purge keeps.
- **The theme's `kept` mark follows the theme.** A save marks the named images kept and unmarks an
  image it no longer names in either field, even one a person had kept before (there is no record of
  who kept it). A trashed image is accepted only in the field that already names it, so a theme read
  and sent back whole still saves; the save locks the `help_centers` row before the media rows, an
  order no other path reverses.
- **A delete trigger also runs inside a workspace's cascade.** Deleting a workspace removes its
  `kb_article_attachments` rows after the workspace row is gone, so `ai_retrieval_attachment_changed`
  queues an article only while its workspace is visible: the insert would otherwise fail the foreign
  key to `workspaces` (retention, `app.privileged`) or row-level security (a superuser, whose cascade
  runs as the table owner). A new trigger that writes another table on DELETE needs the same check;
  `TestAWorkspaceWhoseArticleListsAFileIsDeleted` pins this one.
- **A generated column cannot be copied in.** `restore-workspace.sh` skips columns with
  `attgenerated` set; the database computes `kb_articles.search` and `kb_article_translations.search` again.
- **Write the article row lock first.** Every path that writes a translation locks the article's row
  (`lockKBArticleRow`) before the translation's, as the import, the link rewrite and the media
  replacement do; taking them the other way round deadlocks against an article save.
- **A test that inserts a translation by SQL needs a slug** (NOT NULL since the translations migration)
  and `status 'published'` if AI retrieval should read it.
- **Two files cannot trade bytes in two plain updates.** `kb_media` has a unique index on
  `(workspace_id, sha256)`, checked row by row, so writing a's hash into b while a still holds it
  fails. `swapKBFileBytes` first gives a a placeholder hash made from its own id, then writes b, then a.
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
- **A library search matches a lowercased copy.** `kb_media.search` is `lower(name || '\n' || alt)`,
  stored on write, and a search is `search LIKE '%' || lower(q) || '%'`: `ILIKE` on name and alt
  folded case per character of every row (a search reads the workspace's 10,000 rows), 12 ms locally
  and 25 ms on CI against the 25 ms budget; the copy takes it to 7–8 ms locally. The alt text cannot go
  into the library index instead: with the name, 500 characters of 4 bytes pass the 2,704 bytes a
  btree entry holds (measured: 2,880).
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
  or import. Runs of 200 characters separated by spaces cost the same per character
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
