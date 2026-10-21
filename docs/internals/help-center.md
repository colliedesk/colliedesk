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
  `html.WithUnsafe`: raw HTML is left out, and a transformer drops links whose scheme is not http,
  https, mailto or tel (after decoding references and dropping the control characters browsers
  ignore). The CSP is `default-src 'none'; img-src 'self'; style-src 'self'; script-src 'none';
  form-action 'self'; frame-ancestors 'none'; base-uri 'none'`; table alignment is an `align`
  attribute because a `style` attribute would be refused. Fonts are the system's.
- **Images from other sites become links.** `img-src 'self'` would show them broken, and on a
  shared origin an image pointing at an API path would fire a request with the reader's cookies.
  Article images (`/api/v1/kb/images/<id>`) are rewritten to `<help center>/images/<id>`.
- **An image is public only while a published article's current text names it.**
  `kb_article_images` links an image to every article whose version ever named it (so restoring a
  version never shows a deleted image); the public route also checks `strpos(body_md, path)` on the
  published, live articles linked to it. Images are `immutable` for a year: an id always names the
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
- **ETag = slug, `content_version`, trial, title, locale, workspace name, base path, build version
  and a hash of the templates.** It is computed only after the workspace is found open, so a locked
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
- **No product name in the pages.** The templates and the style sheet never name Colliedesk (#125);
  `TestTemplatesCarryNoBrandingScriptOrInlineStyle` and the HTTP test check it.
- **`tls-ask`** (`GET /api/v1/public/help-center/tls-ask?domain=`) answers Caddy's on-demand TLS
  question: 200 only for exactly one valid label below `HELP_CENTER_DOMAIN` whose help center is
  open. Without `HELP_CENTER_DOMAIN` it is always 404. It is not rate limited: its only client is
  the proxy, which asks once per name it has no certificate for.

## Traps

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
