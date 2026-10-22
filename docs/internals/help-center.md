# Help center internals

Decisions and traps only. Code: `server/internal/helpcenter` (routing, pages, Markdown),
`server/internal/store/help-center.go` (queries), `server/internal/httpapi/help-center-handlers.go`
(wiring, limits, `tls-ask`). Issue #90; the data model is in `knowledge-base.md`.

## Decisions

- **Rendered on the server, no script.** A single-page app would load the web app's bundle (about
  204 KB) before the first request, which misses the 200 ms budget and indexes poorly. On a
  self-hosted server the help center shares the app's origin, and the session cookie has `Path=/`
  and no `Domain`, so a script running there could act as a signed-in agent. Pages are Go
  `html/template` with goldmark (GitHub Flavored Markdown, like the editor's preview), without
  `html.WithUnsafe`: each piece of raw HTML goes through `kbhtml` instead (see "Video players"
  below), and a transformer drops links whose scheme is not http, https, mailto or tel (after
  decoding references and dropping the control characters browsers ignore). The CSP is
  `default-src 'none'; img-src 'self'; style-src 'self'; frame-src https://www.youtube-nocookie.com
  https://player.vimeo.com; media-src 'self'; script-src 'none'; form-action 'self'; frame-ancestors
  'none'; base-uri 'none'` (`frame-src` comes from the allowlist); table alignment is an `align`
  attribute because a `style` attribute would be refused. Fonts are the system's.
- **Video players, and no other HTML.** `server/internal/kbhtml/allowlist.json` is the only raw HTML
  an article may show; the editor preview (`web/src/features/kb/kb-html.ts`) reads the same file and
  both run `testdata/cases.json`, so they cannot drift. goldmark hands raw HTML over in pieces (an
  HTML block, or each inline tag on its own: `<video …>` and `</video>` are two pieces), so each piece
  is parsed alone (`x/net/html` on the server, `DOMParser` in the preview, both HTML5 parsers):
  - An `<iframe>` is kept only for a YouTube or Vimeo player, and is written again from the video id,
    so no host, path or query of the author's comes through; `youtube.com/embed` becomes
    `youtube-nocookie.com/embed`. An address with a space, control character or backslash, a port,
    a user or another scheme is refused: browsers read those differently from a server parser.
  - The rebuilt tag must pass bluemonday unchanged (a second gate; anything it would alter is left
    out). Then `sandbox="allow-scripts allow-same-origin allow-presentation allow-popups"`,
    `referrerpolicy="strict-origin-when-cross-origin"` and `loading="lazy"` are added; the author
    cannot set them. `allow-scripts allow-same-origin` is safe only because `src` is always the
    player's own origin, never relative and never `srcdoc`.
  - Everything else in a piece is left out, text included, as raw HTML always was, so imported
    articles that still hold HTML read as before; a player nested in another element is still found,
    except inside `dropWithContent` elements (`svg`, `object`, `template`…). Words between inline
    tags are Markdown, not part of a piece, and stay, except between an inline `dropWithContent` tag
    (`<script>`, `<style>`, `<iframe>`…) and its closing tag or the end of the paragraph (the
    `hiddenText` transformer; the preview does it on mdast). A paragraph left with nothing to show
    is removed. `testdata/articles.json` holds whole articles both sides render. Words on the lines of an HTML block (CommonMark
    runs one from a line starting with `<iframe` to the next blank line) belong to the piece, so they
    go too; the preview names them ("text in an HTML block"). The Freshdesk import therefore gives
    each player a paragraph of its own, except in a table cell.
  - Why so strict: an iframe of any site would let an article frame a phishing page under the help
    center's address, leak readers' addresses to sites nobody chose, and break when that site
    goes away; only these two players have a stable, documented embed address (No lock-in: the
    article's Markdown keeps the HTML as written, and the export carries it).
  - `<video>` of the media library (`src="/api/v1/kb/media/<id>"`, nothing else) is shown with
    `controls` always and `preload` `metadata` or `none`; `muted`, `loop` and `playsinline` are the
    author's, `autoplay` never is. An article cannot set a `poster`: `kbhtml.FragmentIn` takes the
    page's library (`HelpCenterArticle` reads the videos its current text links, in the same
    transaction, only when the text names a media path) and writes `<help center>/media/<id>`, the
    library's poster `…/media/<id>/poster/<last 8 hex of its content address>` and, when the author
    gave none, the width and height (scaled to 4,096 on the long side), so a new poster never edits
    an article. A video the library does not know (trash, an image, another workspace) is left out.
    The render cache key holds each video's id, size and poster version, so a new poster is a new
    page for the same text; the goldmark node renderers are shared, so the page's library travels
    in its writer (`pageWriter`) and, for `hiddenText`, in the parser context.
  - The preview shows the same `<video>` from its API address, without a poster yet (PR 5 adds the
    library to the editor).
  - Cost: a 100,000-character article of nothing but raw HTML (about 4,500 pieces, 900 of them
    players) renders in about 12 ms on an M-series Mac (plain text: 1 ms); pages are cached by
    content version.
- **Images from other sites become links.** `img-src 'self'` would show them broken, and on a
  shared origin an image pointing at an API path would fire a request with the reader's cookies.
  Article images (`/api/v1/kb/images/<id>`) are rewritten to `<help center>/images/<id>`.
- **An image is public only while a published article's current text names it.**
  `kb_article_media` links an image to every article whose version ever named it (so restoring a
  version never shows a deleted image); the public route also checks `strpos(body_md, path)` on the
  published, live articles linked to it, and an image in the media library's trash is never served.
  `/media/<id>` is the same rule for a video (`HelpCenterVideo`, path `/api/v1/kb/media/<id>`),
  served with ranges by `attachments.Serve` (`http.ServeContent` on local disk), the app's route
  too; its poster is public under the same rule, at its versioned address only. Every range request
  counts toward the 600 pages a minute per address, and since that counts requests, not bytes, at
  most 4 video responses are sent at once to one address and 64 on the server (503 with
  `Retry-After`; `streams.go`): a player reads one or two ranges at a time. Like an image, a video
  is public once a published text names its path anywhere, a code block or an HTML comment
  included, not only in a `<video>` the page shows. Images are `immutable` for a year: an id always names the
  same bytes. A cache that kept one keeps it after the article is unpublished; the id is a random
  UUID that was public while the article was.
- **Two addresses, one handler.** `APP_ORIGIN/help/<slug>` always works. With `HELP_CENTER_DOMAIN`,
  `<slug>.<domain>` serves the same pages and the path redirects there (301), whether or not the
  workspace exists, so the redirect tells nothing. A slug `email.ValidateHostedSlug` refuses (reserved
  names such as `app`, `www`, `help`) never becomes a host, so the app's own host never turns into a
  help center. The help center wraps everything else in `serve.go`: a request to a help center host
  never reaches the app, the API or `/mcp`. `helpcenter.URL` and `ArticleURL` build every address;
  `GET /api/v1/help-center` returns `url` from them.
- **Unknown, off, locked and malformed are one 404.** Same status, headers and bytes (one copy for
  host names and one for paths, which differ in the style sheet's address), as for the web form
  (`web-form.md`). A locked workspace is a trial that ended unpaid (`locked.go`): it keeps sign-in,
  billing and the export, not a public website, or a trial would host a site forever. Paying opens
  it again at once; self-hosted servers are never locked. As with the web form, timing can still
  tell the cases apart; slugs are not secret (hosted mail addresses carry them).
- **No new privileged path.** The slug is resolved with `WorkspaceForMailSlug`
  (`PrivilegeWorkspaceBySlug`, already reviewed); everything after runs in the workspace's scope.
  `OpenHelpCenter` checks on, then lock, in one scoped transaction.
- **ETag = slug, `content_version`, trial, title, locale, workspace name, base path, build version,
  a hash of the templates and what the page shows of the brand** (brand sheet name, the logo and
  favicon shown with the logo's size, logo text, links). The brand part counts what is shown, not
  the saved theme: moving the logo to the library's trash changes no `content_version`, but the
  page loses its logo. It is computed only after the workspace is found open, so a locked
  or turned-off help center never answers 304. `content_version` grows with every knowledge base
  write (`bumpKB`), so unpublishing an older article invalidates every page, which `max(updated_at)`
  would miss. Pages are `public, max-age=0, must-revalidate`; a 404 and a redirect are `no-store`.
  The style sheet is named by its hash and `immutable`; it lives under `/_/` (below the help center
  and below `/help/`), and `_` is never a slug.
- **Collections are hidden unless something published is in or below them**, so a collection of
  drafts never shows its name. Collection addresses are the slug path (`/c/billing/cards`): a slug
  is unique only among siblings. Articles in a collection are ordered by title; there is no manual
  order yet.
- **Search** reuses the knowledge base search (`kbQuery` and the title `ILIKE`), published only,
  best 25, and reads the first 600 characters of only those 25 for the excerpt; no `ts_headline`.
  The query is cut at 200 characters.
- **Limits per client address** (the web form's limiter and `TRUSTED_PROXIES` handling): 600 pages
  and 30 searches a minute across all workspaces. A search reads every article vector of the
  workspace (no index under row-level security).
- **Trial pages ask not to be indexed** (`<meta name="robots">`, `X-Robots-Tag`, and `robots.txt`
  on a help center host). There is no sitemap.
- **The brand is a second style sheet, not inline style** (`theme.go`). `style-src 'self'` refuses
  inline style, and the CSP stays byte for byte as it is, so `brandCSS` writes the theme as custom
  properties of `site.css` into `<base>/_/brand-<sha256[:6]>.css`, served after `OpenHelpCenter`,
  `immutable` like `site.css`; any other `brand-*.css` name is the help center's 404, so a cache
  never keeps an old look under a live name. It loads after `site.css`, so its `:root` also wins
  over the dark palette: with any color set, it sets every color and `color-scheme: light` (owner
  decision: brand colors are chosen for light; the `<meta name="color-scheme">` says `light` too).
  A theme without colors or fonts has no sheet, so an untouched help center sends what it sent
  before. Only validated `#rrggbb` colors and the fixed font stacks (system fonts, no `font-src`)
  reach it; `TestTemplatesCarryNoBrandingScriptOrInlineStyle` checks it with every font.
- **Every color pair the pages show reads (WCAG AA), not only the six the save checks.**
  `domain.ThemeBrandPalette` derives surface, muted, rule and the accent tints from the saved
  colors and falls back (surface to the background, muted to the text, tints to the surface) when
  text or links would not read on them, so a derived color never refuses a save. The focus ring
  (`--ring`) is drawn on the page, the cards and the tints: it is the primary color where that
  reaches 3:1 on all of them, else the link color (4.5:1 on each by the fallbacks above); a primary
  that passes the save on a dark page can still fade on its lighter cards. Header and footer each
  get their own link hover and focus ring color: the link (or primary) color where it reads on the
  band, 4.5:1 (3:1 for the ring), else the band's ink, which the save already checks. The skip
  link, focused, sits on the header band and takes its ring; the header's search button draws its
  ring inside itself in its own ink, since a ring around it would cross both the field and the band.
  A button under the pointer (`--accent-hover`) moves the primary 12% away from its ink, so the
  button text only gains contrast; a brightness filter took text near 4.5:1 below it. Proved by
  `TestBrandPaletteKeepsEveryShownPairReadable` (with the default look's hover by
  `TestDefaultLookKeepsButtonTextReadableOnHover`) and `web/e2e/help-center-branding.spec.ts`:
  axe on every page with the default look in light and dark, with the brand in light (it is light
  only; the test proves that under dark), the rings and hover color of the header, and no sideways
  scroll at 320 px with a long website host (the link ends in an ellipsis, its `title` holds it).
- **Logo and favicon are public only while the theme names them** (`brand-media.go`):
  `<base>/_/logo/<id>` and `<base>/_/icon/<id>` serve the id the page's theme names in that field,
  an image of the workspace's library outside its trash (`OpenHelpCenter` joins `kb_media`, and
  `HelpCenterBrandImage` checks again that the help center names it), with the article images'
  headers (`immutable`, `default-src 'none'; sandbox`, `nosniff`). The logo's `width` and
  `height` come from `kb_media`, so the header does not move as it loads.
- **No product name in the pages.** The templates and the style sheet never name Colliedesk (#125);
  `TestTemplatesCarryNoBrandingScriptOrInlineStyle` and the HTTP test check it.
- **`tls-ask`** (`GET /api/v1/public/help-center/tls-ask?domain=`) answers Caddy's on-demand TLS
  question: 200 only for exactly one valid label below `HELP_CENTER_DOMAIN` whose help center is
  open. Without `HELP_CENTER_DOMAIN` it is always 404. It is not rate limited: its only client is
  the proxy, which asks once per name it has no certificate for.

- **An article renders once per text, behind a guard (`render.go`).** goldmark cannot be stopped and
  has quadratic paths (Traps), so a public page must not parse Markdown on every visit nor let one
  workspace's texts take the CPU. The page and its meta description are kept in memory, least
  recently used out first within 64 MiB, under a key of the renderer version, workspace, article
  number, address and text (so never served to another workspace or for an older text; restart and
  each replica render again once). At most a quarter of the processors (at least two) render at
  once, one per workspace (its other first visits wait for it), and visitors of one text share its
  render. A visitor waits at most 1 s, then gets the text escaped as written, under a line saying
  to reload, with `no-store` and no ETag. A render that had started finishes in the background and
  its page serves the next visit; one still waiting for a slot does not start, and the next visit
  tries again (no queue of renders builds up). Left over: many
  workspaces with slow texts can hold every slot for as long as one render takes (about 0.7 s now),
  delaying others' first visits to the 1 s fallback. Chosen over rendering on write, which still
  needs the same guard for older articles and would add a migration and a render to every write path.

## Traps

- **goldmark has quadratic paths, and every visit renders.** Measured on 100,000 characters: one line
  of `>` (or `1. `, `+ `) markers 2.7 s; one line of `[a](` or `![](` (links that never close) 3.6 s;
  25,000 headings of one name 24 s (goldmark looks for a free id suffix from 1 each time); `*a`, `_a`,
  `~a` 0.7 s (Linkify starts an e-mail search at every `*`, `_`, `~`, space and `(`, and it read the
  rest of the line). `tame` escapes, per line outside fenced code, the 17th nested marker and every
  link start past 16 whose `(` does not close before the next one; `headingIDs` gives goldmark's ids
  (`a`, `a-1`, …) remembering the next free suffix; `linkify` asks goldmark's Linkify only where a
  URL prefix or an `@` within 65 characters follows, with a local part of at most 64. Every known
  text now renders cold in under 30 ms locally; the render guard above bounds what is not known. `TestHelpCenterHostileArticlesLatencyBudget` holds them.

- **A build without a version still changes its ETag.** Development builds have an empty version;
  the template hash in the ETag keeps an edited page from being revalidated as the old one.
- **The labels are English.** `locale` sets `<html lang>`; translating the page chrome is not done.
- **The limiter is per process**, like the web form's. It counts per client address, so a scraper
  slows only itself; behind a proxy that address comes from `TRUSTED_PROXIES`, without it every
  visitor shares the proxy's address and one budget.
- **The server does not compress.** The proxy in front does (Caddy `encode zstd gzip`); pages are
  small (about 4 KB, the style sheet 13 KB, 3.6 KB compressed).
- **Search text is cleaned first.** Postgres refuses NUL and invalid UTF-8 in text, which would turn
  a crafted query into a 500; both are dropped before the query runs.
- **Image sizes are unknown** (not stored), so an article image can shift the text below it as it
  loads. Search and result pages carry `noindex`; other pages a canonical link and, for articles and
  collections, a description.
