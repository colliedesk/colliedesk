# Knowledge base

The knowledge base holds your team's help articles, written in Markdown and grouped in collections.
Every agent can write, edit, publish and delete articles. A published article appears on the public
help center while an admin has it turned on; a draft is seen only by agents. Every change is kept as
a version you can restore, and a deleted article or collection waits in a trash for 30 days. AI
agents can do the same (see [AI agents](#ai-agents)).

Open it from **Knowledge** in the left rail (on a phone, in the bar at the top).

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
   lists, `- [ ]` checklists, `[links](https://example.com)`, tables, `> quotes` and code. The Markdown
   is colored as you type, and what you type is exactly what is stored.
   - The toolbar sets the block (**Text**, **Heading 1** to **3**) and adds bold, italic,
     strikethrough, code, a link, bulleted and numbered lists, a checklist, a quote, a code block, a
     table, a divider and an image. With the keyboard, Tab reaches the toolbar and the arrow keys move
     along it.
   - Shortcuts: `Ctrl+B` bold, `Ctrl+I` italic, `Ctrl+E` code, `Ctrl+K` link, `Ctrl+Z` undo,
     `Ctrl+F` find and replace (`Cmd` on a Mac).
3. Choose the **Collection**.
4. Select **Save draft** to keep it for agents only, or **Save and publish**.

**Write** shows the Markdown, **Preview** shows how the article reads, and **Side by side** (in a wide
window) shows both. A wide window opens an article side by side, unless it is longer than 20,000
characters: then it opens in **Write**, which stays fast at any length. Raw HTML in the text never runs:
the preview shows it as text, and the help center leaves it out.

Enter starts a plain new line (type `- ` or `> ` again to go on with a list or quote), and Tab moves on
to the next control instead of indenting, so the keyboard never gets stuck in the text; indent with
spaces.

`Ctrl+S` (`Cmd+S` on a Mac) saves. The text holds at most 100,000 characters and a title 300. A longer
text is never cut: the counter turns red and saving waits until you shorten it, or split it into
two articles.

### Images

Paste an image into the text, drag it in, or select **Insert image**. Colliedesk uploads it and writes
`![name](/api/v1/kb/images/…)` on a line of its own after the paragraph, list or table you are in;
while it uploads, a box says so and nothing is added to the text. PNG, JPEG, GIF and WebP up to 5 MB
each are accepted. Images count toward your plan's storage. The same image uploaded twice is stored
once. Saving waits until every image has finished uploading.

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

<!-- The public help center (addresses, search, what visitors see, caching) is documented with the
pull request that serves it (issue #90). -->

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
