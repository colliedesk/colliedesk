# Knowledge base

The knowledge base holds your team's help articles, written in Markdown and grouped in collections.
Every agent can write, edit, publish and delete articles. A published article appears on the public
help center while an admin has it turned on; a draft is seen only by agents. Every change is kept as
a version you can restore, and a deleted article or collection waits in a trash for 30 days. AI
agents can do the same (see [AI agents](#ai-agents)).

Open it from **Knowledge** in the left rail (on a phone, in the bar at the top). Moving from
Freshdesk? `collie import freshdesk` brings your Freshdesk knowledge base over with its collections,
dates and images; see [Import from Freshdesk](import-freshdesk.md#knowledge-base).

## Collections

A collection groups articles, like a Freshdesk category, folder and subfolder: collections go three
levels deep. Every article belongs to one collection, so a new knowledge base starts by asking for
one, for example "Getting started".

- **Add**: select **New collection**, type a **Name** (and a description if you like), choose where it
  goes under **Inside**, then **Create collection**. To add one inside the collection you are looking
  at, select **Add a collection inside**. A collection on the third level cannot hold another one.
- **Choose**: select a collection on the left to list only the articles directly in it; **All
  articles** lists every one. The number next to a collection counts the articles directly in it, drafts included, not those of the collections inside it.
- **Edit**: select the collection, then **Edit**. You can rename it, change its description, move it
  under another collection (or to the top), and change its **Address name**, the part of its help
  center address. Changing the address name changes the help center addresses of the collection and
  of the collections inside it; old addresses are not redirected, so the page warns you before you
  save.
- **Delete**: only an empty collection can be deleted. Move or delete its articles and collections
  first; otherwise Colliedesk says why and deletes nothing. A deleted collection goes to the trash.

## Find an article

Type in **Search articles**. The search finds whole words and the start of words in titles and text,
and any part of a title, so `pass` finds "password". It does not find a part in the middle of a word
in the text (`word` does not find "password"), and in text written without spaces between words
(Chinese, Japanese, Thai) it finds only the start of the text or a title. **All**, **Draft** and
**Published** filter by status. The search, the status and the collection are in the address, so a
link opens the same list.

## Write an article

1. Select **New article** (inside a collection, it starts there).
2. Type a **Title** and the **Text** in Markdown: `**bold**`, `_italic_`, `#` headings, `-` and `1.`
   lists, `[links](https://example.com)`, tables, `> quotes` and code. **Preview** shows how it reads.
   Raw HTML in the text is never shown.
3. Choose the **Collection**.
4. Select **Save draft** to keep it for agents only, or **Save and publish**.

`Ctrl+S` (`Cmd+S` on a Mac) saves. The text holds at most 100,000 characters and a title 300. A longer
text is never cut: the counter turns red and saving waits until you shorten it, or split it into
two articles.

### Images

Paste an image into the text, or drag it in. Colliedesk uploads it and writes
`![name](/api/v1/kb/images/…)` where the cursor was; while it uploads, the text says so. PNG, JPEG,
GIF and WebP up to 5 MB each are accepted. Images count toward your plan's storage. The same image
uploaded twice is stored once. Saving waits until every image has finished uploading.

Only uploaded images are shown. An image linked from another site (`![x](https://…)`) is not shown in
the preview or on the help center; the preview says so. Download it and paste it in instead.

### Publish, unpublish, edit

An article shows **Draft** or **Published**. **Publish** puts a draft on the help center and
**Unpublish** takes it back off; both are versions, so neither is final. **Save** keeps an article's
status.

Each article has a number that never changes. Its help center address ends in
`/a/<number>-<address name>`, and the article page shows it under **Address name**. You can change the
address name; links with the old one still lead to the article because the number does not change.

If a teammate (or an AI agent) saved the article after you opened it, your save is refused and
nothing is overwritten. Your text stays on screen and you choose: **Save mine over theirs** (theirs
stays in the history) or **Discard mine and show theirs**.

Leaving the page with unsaved changes asks first.

## History

**History** on an article lists every version, newest first: who made it (and through which AI
agent token or approved request), when, and what changed (title, address name, text, status,
collection). **Restore version N** makes that version's title, address name and text current again,
as a new version; the status and collection stay as they are, so a restore never publishes or moves
an article by surprise. Save or undo your own changes first.

## Trash

Select **Trash** to see deleted articles and collections. Each shows who deleted it and how many days
are left; after 30 days it is deleted for good, with its history. **Restore** brings it back as it
was: a published article is published again. Restore a collection before the articles that were in
it. Articles in the trash count toward the 2,000 articles a workspace can hold.

## Help center settings

Open **Settings → Help center**. Every agent sees whether the help center is on. An admin can turn
it on or off with **Publish the help center** and set its **Title** (empty uses the workspace name).
Turning it off takes every page offline at once; turning it on again publishes the articles marked
published.

## Public help center

The help center is a public website made from your published articles. Customers read it without
signing in, on Colliedesk Cloud and on your own server alike. It is off until an admin turns it on
in **Settings → Help center** (see [Help center settings](#help-center-settings)).

### Its address

**Settings → Help center** shows the address as a link, even while the help center is off; the API
(`GET /api/v1/help-center`) and `colliedesk_help_center_get` give it in `url`.

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

## AI agents

An AI agent connected through [MCP](mcp.md), or the API with an agent token, can search and read
articles (`colliedesk_kb_articles_search`, `colliedesk_kb_article_get`), write, publish, delete and
restore them, manage collections, upload and read images, and read the help center settings.

| Token tier | Read, search, history, trash | Write, publish, delete, restore, upload images | Turn the help center on or off |
|---|---|---|---|
| `admin` | Yes | Yes, at once | Yes |
| `send` | Yes | Yes, at once. The version names the token | Proposed for an admin to approve |
| `draft`, `read` | Yes | Proposed: an [agent request](agent-requests.md) a person approves; an image upload is refused | Proposed for an admin to approve |

## Limits

- 2,000 articles per workspace, counting the trash, and 1,000 collections.
- 100,000 characters per article text, 300 per title; never cut.
- Images: PNG, JPEG, GIF and WebP, 5 MB each.
- Collections go three levels deep.
- Search does not find a part in the middle of a word, nor words in text written without spaces.
- Translations an import brings in are kept and exported, but not shown or edited yet.
