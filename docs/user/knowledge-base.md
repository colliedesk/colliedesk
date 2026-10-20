# Knowledge base

The editor, collections, history and trash are described with the knowledge base editor. This page
covers what customers see.

## Public help center

The help center is a public website made from your published articles. Customers read it without
signing in, on Colliedesk Cloud and on your own server alike. It is off until an admin turns it on
in **Settings → Help center** (or through the API, `PUT /api/v1/help-center`, or the MCP tool
`colliedesk_help_center_update` with an `admin` token).

### Its address

`GET /api/v1/help-center` (and `colliedesk_help_center_get`) gives the address in `url`, even while
the help center is off.

- **Colliedesk Cloud:** the workspace's own host name, `https://<workspace address name>.colliedesk.com`.
- **Your own server:** `<server address>/help/<workspace address name>`, for example
  `https://desk.example.com/help/acme`. The server can give each workspace its own host name instead;
  see [help center host names](../self-host/running-behind-proxy.md#help-center-host-names).

An article's address is the help center address followed by `/a/<number>-<address name>`, for example
`/a/12-reset-your-password`. A link with an old or mistyped address name still leads to the article:
the number finds it, and the page moves to the current address. A collection's address is the path
of address names, `/c/billing/cards`; changing one is not redirected. **Changing the workspace
address name** (Settings → Email) changes every help center address, and old links stop working.

### What visitors see

- Only published articles outside the trash. Drafts, the trash, and collections with nothing
  published in or below them never appear: not in a page, a count, a search or an address.
- The home page lists the top-level collections, each with its first articles and the collections
  inside it. A collection page lists those collections and its articles by title, 50 a page. An
  article page lists the other articles of its collection beside the text.
- **Search** works like the search in the editor: whole words and the start of words in titles and
  text, and any part of a title. It shows the best 25 matches.
- Images you added to an article are shown while a published article uses them. An image from
  another site is shown as a link to it: the help center loads nothing from other sites.
- Raw HTML in the text is never shown, and links work only for `http`, `https`, `mailto` and `tel`.

Pages have no scripts, no cookies and no tracking, follow the visitor's light or dark setting, and
fit a phone. The page labels (Search, Home and so on) are in English; the help center's language
marks your articles' language for browsers and search engines.

### Limits

- One visitor address can load 600 pages and run 30 searches a minute; past that the page asks to
  wait a minute.
- During a Colliedesk Cloud **trial**, pages ask search engines not to index them. A paid plan lifts
  this.
- A **locked** workspace (trial over, nothing paid) has no help center: its addresses say "This page
  does not exist", like a help center that is off. Paying brings it back at once.
- Browsers may keep a page but check with the server before showing it again, so an article you
  unpublish disappears on the next visit.
