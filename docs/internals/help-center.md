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
  is unique only among siblings. Collections and the articles of a collection follow the order set by
  hand (`position`, then name or title; see `knowledge-base.md`); a reorder bumps `content_version`,
  so cached pages and their neighbours list move with it.
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
  properties of `site.css` into `<base>/_/brand-<sha256[:16]>.css`, served after `OpenHelpCenter`,
  `immutable` like `site.css`; any other `brand-*.css` name is the help center's 404, so a cache
  never keeps an old look under a live name. The name keeps 128 bits because an admin writes every
  byte of the custom CSS: with 48 bits, a second sheet with the name of the current one is a few
  seconds of brute force, and caches would serve it for a year after the owner restored the first. It loads after `site.css`, so its `:root` also wins
  over the dark palette: with any color set, it sets every color and `color-scheme: light` (owner
  decision: brand colors are chosen for light; the `<meta name="color-scheme">` says `light` too).
  A theme without colors, fonts or custom CSS has no sheet, so an untouched help center sends what
  it sent before. Only validated `#rrggbb` colors, the fixed font stacks (system fonts, no
  `font-src`) and checked custom CSS (below) reach it; `TestTemplatesCarryNoBrandingScriptOrInlineStyle`
  checks it with every font.
- **Custom CSS is the end of the brand sheet, not a sheet of its own** (`custom-css.go`). Appended
  after the `:root` variables, it overrides them, its bytes are in the sheet's hash (a new text is a
  new immutable name, the old one the help center's 404) and it costs no extra request or route;
  the CSP stays byte for byte. It is stored and served exactly as the admin wrote it (reject, never
  rewrite), so the export round-trips. `CheckCustomCSS` runs in the theme handlers (save, check,
  preview) before anything else sees the text; the column only bounds its size (32 KiB, `octet_length`),
  since the store cannot import the checker. It reads the text with gorilla/css's tokenizer and
  refuses, at a line and column: a URL token or a function that takes an address or an image
  (`url`, `image`, `image-set`, `cross-fade`, `element`, `src`, `paint`, `image-rect`, vendor prefix
  dropped) and `expression()`; the properties `behavior` and `-moz-binding` (the name followed by `:`,
  not after `.` or `:`, so the class `.behavior` stays); a control character other than tab and line
  ends; at-rules other than `@media`,
  `@supports`, `@keyframes`, `@layer`, `@container` (an allowlist, so a new at-rule is refused); a
  backslash outside a string token, so no escape spells a refused name; an unbalanced bracket, an
  unclosed string or comment, `<!--`/`-->`. Two checks do not trust the tokenizer: `</` anywhere
  (the preview inlines the sheet in a `<style>` element), and, after decoding escapes and
  lowercasing, `url(`, `@import`, `image-set(`, `image(`, `cross-fade(`, `element(`, `src(`, `paint(`,
  `expression(`, `javascript:`, `vbscript:` or `</` anywhere, strings and comments included. The
  image functions are in that list because the tokenizer's names are not the browser's: it reads
  `U+0-image(` as a unicode range and a function `-image(`, which no vendor prefix explains. The
  tokenizer follows CSS 2.1 and is stricter than a browser; the one difference people hit, a raw tab
  in a string (an unclosed string to it), is read as a space before tokenizing (same length, so every
  line and column stays). Any other difference refuses rather than lets through;
  `FuzzCheckCustomCSS` holds the property, and the browser's `img-src 'self'` and
  `default-src 'none'` are the second gate. Contrast is not checked: the docs and the field say so.
  A 32 KiB text takes 7–57 ms to check (the regex tokenizer), once per save or preview. The limit
  is 32,768 bytes of UTF-8, not characters: the contract and the MCP schema carry no `maxLength`
  (a JSON schema counts characters) and the settings field counts bytes.
- **A save must name `custom_css`** (`HelpCenterThemeSave`, `customCSSRequired`). The save replaces the
  whole theme, so a body without the field, from a web bundle loaded before it existed or an agent
  request filed before it, would clear the CSS silently. It is 422 `validation_failed` instead, an
  agent request without it is not filed, and one stored earlier fails on approval with the same 422;
  null or `""` clears it. The check and the preview save nothing and keep it optional.
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
  open. Without `HELP_CENTER_DOMAIN` it is always 404. A claim target or custom domain goes through
  `Site.CertifyHost`: a name nobody claims is remembered (30 s, forgotten by a domain write), and a
  name no cache knows is charged to the client's address (`TRUSTED_PROXIES`-aware, the same 10
  unknown-name lookups a minute as `Wrap`) before any query, while names found claimed or served
  are never charged, so random names cannot make the server query without bound.

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

- **The brand preview is the real page, rendered by the server and shown without scripts.**
  `POST /api/v1/help-center/theme/preview` runs `Site.Preview`: the same templates (`homeOf`,
  `articleOf`) as the public pages, from the workspace's live collections and published articles
  (lowest article number, or a fixed sample), with the candidate theme; nothing is stored, so there
  is no draft table or token to clean up.
  - *One transaction.* `Store.HelpCenterPreview` reads the site, the logo and favicon check (the
    rule of a save, `themeMediaProblem`), the collections and the page's articles (home: the first
    of each top collection; article: the text, its videos and its neighbours) in one workspace
    transaction whose statements go out as one pgx batch. Before, a preview was five to seven
    transactions in a row; the session's own read stays apart.
  - *Style.* The document links `site.css` by its hashed, immutable address under `/help/_/` (the
    browser keeps it between previews; it is not sent again in every answer) and carries only the
    brand sheet inline, last in the head so it overrides the sheet's defaults (a theme not saved has
    no hashed brand sheet). The custom CSS ends that block, alone in its `<style>` element, so text
    that leaves a selector or an at-rule open (`x`, `@media print`, accepted since its brackets
    balance) runs into the end of the element instead of into the next rule. The home answer went from 22 KB to 6 KB. `vite.config.ts` proxies `/help/` so the sheet loads in development too.
  - *Policy.* Its own meta policy comes first: `default-src 'none'; img-src <origin>/api/v1/kb/images/
    <origin>/api/v1/kb/media/; style-src <origin>/help/_/ 'unsafe-inline'; script-src 'none';
    form-action 'none'; base-uri 'none'` (`previewPolicy`, origin = `APP_ORIGIN`). The frame is
    same-origin and sends the cookie, so it names paths, as the email frame does: a CSS `url()` cannot
    reach any other route. A session's writes must come from `APP_ORIGIN` (CSRF check), so the web
    app compares the policy with its own `location.origin` and refuses a document without exactly
    this one. The templates keep no inline style; `'unsafe-inline'` covers the brand block only.
  - *Links and media.* Links and media point below `/api/v1/kb` (images at `/api/v1/kb/images/<id>`,
    the logo at `/api/v1/kb/media/<id>`), which serve the session's own workspace whether the help
    center is on or off. Videos and players would be blocked boxes under this policy, and loosening
    it for them is not worth it, so each becomes "Video shown on the live help center"
    (`playerElement`: kbhtml writes both as an empty element with escaped attributes).
  - *Inert links.* The first `<style>` element, right after the policy, sets `pointer-events: none
    !important` on links, buttons, inputs, labels and summaries, everything inside them and their
    `::before`/`::after` boxes, inside an anonymous `@layer`. For `!important` the first-declared layer
    wins over every later layer and unlayered rule whatever their specificity or order, and no sheet
    can add to a layer without a name, so custom CSS cannot hand a link its pointer back (specificity,
    `@layer x`, `a *`, a `::before` box over the frame, `all: unset`). A rule placed after the custom
    CSS was beaten all those ways; the e2e test clicks each one in a frame without the app's listener.
    The web app keeps the frame `inert` until its document has loaded and its capture listener (which
    also holds a link opened from the keyboard) is attached, again on every new preview.
  - *Frame.* The web app shows it in `<iframe sandbox="allow-same-origin" srcdoc>`, never with
    `allow-scripts`; same origin lets the images load with the session and lets the page stop link
    clicks inside the frame. The e2e test runs the preview with CSP enforced (only the axe tests on
    the public pages bypass it).
  - *Requests.* Previews count against the help center page limiter under a per-person key (600 a
    minute). The form asks for nothing until the saved theme is loaded, waits 300 ms after the last
    change, keeps one request in flight, drops an answer for a theme that is no longer the latest and
    sends nothing after it is gone. A link or phone number not yet of a shape the server accepts, like
    a color being typed, goes to the preview as empty and its field says the shape; Save sends what
    is written. An edit clears the refusal of the fields it changes.

- **Custom domains: ownership by a CNAME to the claim's own target or a TXT token, never by address
  or slug.** Every custom domain resolves to the same proxy, so an `A`/`AAAA` match would let any
  workspace claim any other's domain. A claim (`help_center_domains`, one per workspace) is proven when
  the end of the name's CNAME chain (`net.Resolver.LookupCNAME`) is exactly the claim's target
  `hc-<claim>.<HELP_CENTER_DOMAIN>` (`helpcenter.ClaimHost`), or `_help-center-verify.<name>` holds its
  128-bit token. The claim is 96 random bits made with the claim; setting another name, or removing and
  setting the same one, makes a new claim, so a target is never reused. The workspace's slug host used
  to be the target: a slug can be renamed and taken by another workspace, and a CNAME left at the
  customer's DNS provider then proved the other workspace's claim (`TestACNAMELeftBehindNeverProvesAnotherWorkspacesClaim`).
  `email.HelpCenterClaimLabel` reserves exactly the `hc-<24 hex>` shape, not the `hc-` prefix, so a
  team called "HC Labs" keeps the slug `hc-labs`. A request on a target redirects to the help center's
  current address, and `tls-ask` answers for it. Without `HELP_CENTER_DOMAIN` every workspace shares
  the app's host, so only the token proves anything. The host name is stored lower-case ASCII (Unicode
  goes through `idna.Lookup`), never an IP address, a single label, `APP_ORIGIN`'s host or a name at or
  below `HELP_CENTER_DOMAIN` (`helpcenter.CustomHost`).
- **A check records only the claim it looked up.** Activation and failure match `workspace_id` and
  `claim`, never the host name: a remove and re-add of the same name during the 3 s lookup answers
  `domain_changed` instead of activating the new claim with the old claim's proof.
- **What DNS answered stays in the log.** `check_error` says which record to set ("points somewhere
  else", "has no CNAME record pointing at", "the TXT record ... was not found"); the CNAME chain's end
  and the lookup errors go to the server log (`DomainEvidence.Detail`), never TXT values. An admin may
  type any name, and the server's resolver may answer for names inside its network.
- **Active domains are released.** Pending claims of several workspaces coexist, any number of them;
  the partial unique index on active names allows one active claim per name. A failed check of an
  active domain keeps it serving and sets `failing_since`; the first failed check
  `domain.HelpCenterDomainRelease` (7 days) later puts it back to pending with `pending_reason`
  `proof_lost`. The daily re-check (`Server.RecheckHelpCenterDomains`, from `serve` at start and
  daily) is what usually finds it, so a DNS provider's hiccup does not take a help center off line,
  and a domain that changed hands stops receiving the old addresses' 301s within 7 to 8 days. The
  time since the first failure counts, not the number of failed checks: every restart runs a re-check.
- **Takeover is one transaction.** A claim proven while another workspace is active on the name runs
  `takeOverHelpCenterHost` (`PrivilegeHelpCenterDomainTakeover`): it locks the holder's row, compares
  the holder's claim and token with the same DNS answer inside the database, and either refuses
  (`domain_taken`, the holder still proves it) or puts the holder back to pending (`taken_over`) and
  activates the claim. Two takeovers of one name serialize on the holder's row; one that finds the
  holder gone retries, so a name never has two active claims or none by accident (`TestATakeoverIsAtomic`).
  `PUT` therefore accepts a name that is active elsewhere.
- **One DNS change is enough.** `tls-ask` checks a pending name live (2 s) and activates it, so the
  first visitor after the CNAME change gets a certificate. `helpcenter.LiveChecks` bounds those checks
  to 4 at once and one per name every 30 s (4,096 names remembered; full of recent names it refuses),
  and only names with a pending claim are checked at all, so `tls-ask` cannot make the server flood
  DNS. It looks DNS up once and asks the database which pending claims that answer proves
  (`HelpCenterHostProvenClaims`), so squatters' claims never crowd out the real one; a cap on claims
  read per name would. Every refusal is the same 404. For an active name it records
  `certificate_requested_at` at most once an hour per name (an in-process window plus the same
  condition in the `UPDATE`): the question is public and must not write on every call.
- **The app answers only on its own host.** `Wrap` passes a request to the app, the API and MCP only
  when `Host` is `APP_ORIGIN`'s host, `localhost` or an IP address (the health check and Caddy's
  `ask` use loopback). Every other name is the help center's: a slug host, a claim target or an active
  custom domain serves it, and anything else, a domain just removed included, gets the help center's
  404. A removed domain whose certificate Caddy still holds would otherwise serve the sign-in page,
  and a login there sets a session cookie for that name. Proxies must keep `Host`
  (`docs/self-host/running-behind-proxy.md#the-host-header`).
- **Host lookups are cached, bounded and charged.** `Wrap` looks a host up only when it is a valid
  name of two labels or more that is not `APP_ORIGIN`'s host, not at or below `HELP_CENTER_DOMAIN` and
  not an IP address. The lookup (`store.HelpCenterHost`, `PrivilegeHelpCenterHost`, read-only) returns
  only a workspace id. Known and unknown names have caches of their own, 30 s and 4,096 entries each,
  so random names only evict random names. A name neither cache holds is charged to the client's
  address first (`Config.AllowLookup`, 10 a minute; the busy page beyond); a known name whose entry
  expired is looked up again without the charge. Concurrent misses are merged (`singleflight`). A
  domain write forgets only the names it changes (`Site.ForgetDomain`: the old and new host, the claim
  target, the workspace's address), and a check that changes no status forgets nothing; a generation
  counter per cache keeps a lookup that started before a forget from storing its answer. The cache
  only names a candidate: the page's own query (`OpenHelpCenterWorkspace`) reads the active domain, and
  a host that no longer matches gets the 404, so a removal is served nowhere from the next request on.
- **The TXT token is readable by every tier.** `GET /help-center/domain` and its MCP tool are
  read-tier and show the token. It is public in DNS once the record exists and proves only its own
  claim, so hiding it below admin would protect nothing; it still never leaves in an export or a log.
- **Old addresses move with a 301 built from the stored name.** The slug host and `/help/<slug>`
  redirect (no-store) to the custom domain with the escaped path and query, while the help center is
  open; the target never uses a host name or path prefix the request names. `tls-ask` keeps answering
  for the slug host so the redirect keeps its certificate.
- **One address builder.** `helpcenter.SiteURL(origin, domain, slug, custom)` gives the help center's
  address everywhere: the API's `url` fields (through `Site.Address`, cached per workspace like the
  hosts), article `url`s, canonical links and the ETag, which holds the address so a page cached
  under the old one is stale.

- **Old Freshdesk addresses are one handler on every address** (`legacy.go`). A path the help
  center does not know that `ParseLegacyPath` reads as `[/<lang>]/support/...` (language
  `[a-z]{2,3}(-[A-Za-z0-9]{2,8})?`) is answered after `OpenHelpCenter`, so the slug host, `/help/<slug>`
  and an active custom domain all serve it (the path address first moves to the help center's address
  with the old path kept), behind the page limiter. Home, `/support/home` and `/support/solutions` are
  301 home; a category, folder or article id is looked up by `HelpCenterLegacyTarget`: one scoped query
  that probes `import_items_source_id_idx` `(workspace_id, entity, source_id)` (the primary key leads
  with the source, which the old address does not carry), `source LIKE 'freshdesk:%'`, `status='done'`,
  joined to the article (published, outside the trash) or collection (outside the trash). An article is
  301 to `ArticlePath`; a collection 301 to its slug path only while something is published in or
  below it (the collection tree, as the pages decide). Anything else is 302 to `/search?q=<words of the
  old slug>`, or home without words: 302 so a later publish takes over, and never a 404, since these
  addresses live in emails and search engines. `/support/search?term=` is 302 to the search. Every
  target is `SiteURL` plus our own path, never a host or path the request names; the redirect is
  `no-store`. `/support/...` on `APP_ORIGIN` outside `/help/` is the app's and looks nothing up.
  Workspace B never reaches A's pages: the query is scoped and filters `workspace_id`, and Freshdesk
  ids repeat across accounts (`TestOldFreshdeskAddressesStayInTheirWorkspace`). Two accounts imported
  into one workspace with the same id: the newest ledger row wins.

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

- **A wildcard CNAME breaks the CNAME check.** The check compares where the name's chain ends; if
  `*.<HELP_CENTER_DOMAIN>` is itself a CNAME, every chain ends at its target and no claim matches the
  slug host. Keep the wildcard as `A`/`AAAA` records, or the owner uses the TXT record. Comparing the
  two chains' ends instead would let any workspace claim a name CNAME'd to another's slug host.
- **A flattened or proxied CNAME looks like an `A` record.** Cloudflare's proxied records and apex
  CNAME flattening answer with addresses, so the chain ends at the name itself; the TXT record is the
  way there.
- **Cloud needs the catch-all site in its proxy.** The `https://` on-demand site of
  `docs/self-host/running-behind-proxy.md` lives in the Cloud operator's infrastructure repository,
  not here; without it a verified domain gets no certificate on Cloud.
- **Other processes may lag 30 s.** A domain activated through one process gets the help center's
  404 on another until its unknown-name entry expires, and API `url` fields there keep the previous
  address as long. A removal is never served late (the page's query decides).
- **`TRUSTED_PROXIES` matters for host lookups.** Behind an untrusted proxy every visitor has the
  proxy's address and shares its 10 unknown-name lookups a minute; known names are unaffected.
