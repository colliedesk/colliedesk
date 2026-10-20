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
   lists, `- [ ]` checklists, `[links](https://example.com)`, tables, `> quotes` and code. The Markdown
   is colored as you type, and what you type is exactly what is stored. Raw HTML in the text is not
   shown, except a [YouTube or Vimeo video](#videos).
   - The toolbar sets the block (**Text**, **Heading 1** to **3**) and adds bold, italic,
     strikethrough, code, a link, bulleted and numbered lists, a checklist, a quote, a code block, a
     table, a divider, an image and a [video](#videos). With the keyboard, Tab reaches the toolbar and the arrow keys move
     along it.
   - Shortcuts: `Ctrl+B` bold, `Ctrl+I` italic, `Ctrl+E` code, `Ctrl+K` link, `Ctrl+Z` undo,
     `Ctrl+F` find and replace (`Cmd` on a Mac).
3. Choose the **Collection**.
4. Select **Save draft** to keep it for agents only, or **Save and publish**.

**Write** shows the Markdown, **Preview** shows how the article reads, and **Side by side** (in a wide
window) shows both. A wide window opens an article side by side, unless it is longer than 20,000
characters: then it opens in **Write**, which stays fast at any length. Raw HTML in the text never runs:
the preview and the help center show only [YouTube and Vimeo players](#videos) and leave the rest out.

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
each are accepted, at most 8,192 pixels on a side and 40 megapixels. Images count toward your plan's
storage. The same image uploaded twice is stored once. Saving waits until every image has finished
uploading.

Location, camera details, dates and comments that a phone or editor leaves in an image are removed
before it is stored, without changing the picture: a photo keeps the way it should be turned. Images
an import brings in keep their bytes as the other helpdesk served them.

Every uploaded image goes into the workspace's media library, with a name, alt text and a small
preview. An image no article ever saved is deleted 30 days after it was uploaded, unless it is kept;
naming it or writing its alt text keeps it.

### Media library

**Knowledge → Media** shows every image and video of the workspace, newest first, with a search by
name or alt text, a choice of images or videos, of what is in use, not used or missing alt text,
and a sort (newest, oldest, name, largest). **Upload** takes several files at once, or drop them
anywhere on the page; an upload made here is kept. Each file shows its progress; a video shows its
speed and the time left, and can be paused, resumed (also after a lost connection: it goes on from
the bytes the server already has) or cancelled. The same file uploaded twice says **Already in the
library**. Leaving the page cancels the videos it was still sending.

A workspace has at most three video uploads in progress. **Uploads in progress** lists those this
page is not sending (from another tab, or a page that closed), with who started each and when its
parts expire (24 hours after the start): **Resume…** goes on when you choose the same file again, and
**Cancel** frees its place; only the person who started an upload, or an admin, cancels it.

Choose an item to see it in a panel: its type, size, length and where it is used, with a link to
each article. There you rename it, write its alt text (a video's title for screen readers), keep it,
copy what to put in an article (**Copy embed**), download it, **Replace…** it, or move it to the
trash. A video's **Poster** is the frame the player shows when you press **Use this frame** (pause
it where you want), or an image you upload.

A line above the library says how many items no article uses and how much space they take;
**Review** opens **Not used**. There, tick items (Shift picks a range) and **Move selected to trash**,
or **Move all unused to trash**. A confirmation says how many and how large; kept items are skipped
unless you include them, and Undo puts the whole batch back. **Trash** lists what is there; restore
items, or, as an admin, **Delete forever** after a second confirmation.

Items no article uses can go to the library's **trash**: one at a time, a chosen set, or all unused
items at once (kept ones only when you say so). An image a published or draft article still shows
cannot go to the trash; change the article first. Saving an article that names an item in the trash
takes it out again, and one Undo brings back everything a "move to trash" moved. Items stay 30 days
in the trash and still count toward your storage until then; an admin can **delete them forever**
earlier to free the space, which cannot be undone (articles in the trash and older versions that
showed one then say "Media no longer exists").

**Replacing** an item puts another image (or video, for a video) in its place in every article that
shows it, as a new version of each article; published ones change on the help center at once. Undo
the replacement to put the first one back. An article saved with an image that no longer exists is
saved all the same, and the answer names the missing image.

Only uploaded images are shown. An image linked from another site (`![x](https://…)`) is not shown in
the preview or on the help center; the preview says so. Download it and paste it in instead.

### Videos

An article can show a YouTube or Vimeo video. Select **Insert video** in the toolbar, paste the
video's link (a watch page, a `youtu.be` short link, a Vimeo page, or a player address), and press
Enter: the player goes in a block of its own after the one the cursor is in. A YouTube link's start
time (`?t=1m5s`) is kept. A link the help center cannot show is refused, with the reason; Escape
closes the box.

You can also write the embed HTML yourself, in a paragraph of its own with a blank line before and
after it, for example the code from YouTube's **Share → Embed**:

```html
<iframe src="https://www.youtube.com/embed/jNQXAC9IVRw?start=65" title="Me at the zoo" allowfullscreen></iframe>
```

- The address must be `https://www.youtube.com/embed/<id>`, `https://www.youtube-nocookie.com/embed/<id>`
  or `https://player.vimeo.com/video/<id>`. YouTube videos play from `youtube-nocookie.com`, YouTube's
  privacy-enhanced mode. Of the address only the video id, YouTube's `start=`
  (seconds) and Vimeo's `h=` (the key of an unlisted video) are kept. A YouTube playlist or live
  stream (`embed/videoseries`, `embed/live_stream`) is not shown: embed one video of it.
- `title`, `width`, `height`, `allow` and `allowfullscreen` are kept; a player without `allow` gets
  `encrypted-media; picture-in-picture`. The video fills the width of the
  page at 16:9 whatever size you give, and loads only when a reader scrolls to it.
- A video of your media library shows as `<video src="/api/v1/kb/media/<id>"></video>` (below).
  `muted`, `loop`, `playsinline`, `title`, `width`, `height` and `preload` (`metadata` or `none`) are
  kept; it always has controls and never starts by itself (`autoplay` is left out). Its poster and
  size come from the library, so a `poster` you write is left out.
- Any other HTML is not shown, text included: an iframe from another site, a `<video>` of another
  site, an `<audio>` file, a script, a `style`, a form. Words on the same lines as the HTML count as part of it and go
  too, so start your text after a blank line. Inside a paragraph only the tags are left out and the
  words between them stay, except the content of a `<script>`, `<style>` or similar element. **Preview** names what it left out on a line above the article; the saved text
  keeps the HTML as you wrote it.
- A video file of your own goes into the media library (below), not as `<video>` HTML of another site.

**Video files.** The media library also holds videos you upload: MP4 (H.264 or AV1 video with AAC
sound, including a QuickTime `.mov` screen recording) or WebM (VP8, VP9 or AV1 with Opus or Vorbis),
up to 200 MiB (about ten minutes of a 1080p screen recording; a self-hosted server may set another
limit). Nothing is converted, so the video must be one every browser plays: a video recorded in H.265
(HEVC), the default of iPhones, is refused with how to export it as H.264 (on an iPhone: **Settings →
Camera → Formats → Most Compatible**). A video goes up in parts, so a lost connection resumes where it
stopped; it counts toward your plan's storage. Location, titles, recording dates and tracks of data a
camera or phone writes (GPS, subtitles) are removed from the file without changing a frame; the
name of the app that wrote a WebM stays. A video can be at most 8,192 pixels on a side. A video can have a
poster, the still picture shown before it plays. The help center plays a video a published article
shows from its own address, with seeking, and shows its poster; a video in the trash, or that no
published article shows, is not public. Browsers and proxies may keep a copy of a video once
it was public, as with images: unpublishing or moving it to the trash stops new readers, not copies
already made, so keep a video that must stay private out of published articles. Upload videos in the [media
library](#media-library); the editor's button to insert one comes next.

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
- Raw HTML in the text is not shown, except [YouTube and Vimeo videos](#videos), and links work only
  for `http`, `https`, `mailto` and `tel`. A video's site sees the reader's address when the video
  loads, as with any embedded video.

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
restore them, manage collections, upload and read images, and read the help center settings. In
the media library (`colliedesk_kb_media_list`, `colliedesk_kb_media_get`) an agent sees which images
no article uses and which have no alt text, and can name, describe or keep them
(`colliedesk_kb_media_update`), move them to the trash or back (`colliedesk_kb_media_trash`,
`colliedesk_kb_media_undelete`) and replace one in every article (`colliedesk_kb_media_replace`), with
the same tiers as writing an article. A request from a lower tier always names the images it would
move. Deleting forever (`colliedesk_kb_media_purge`) is an admin's. An agent with a `send` token uploads
a video in parts (`colliedesk_kb_video_upload_start`, `_part`, `_complete`) and sets its poster
(`colliedesk_kb_video_poster_set`); a lower tier cannot upload files.

| Token tier | Read, search, history, trash | Write, publish, delete, restore, upload images | Turn the help center on or off |
|---|---|---|---|
| `admin` | Yes | Yes, at once | Yes |
| `send` | Yes | Yes, at once. The version names the token | Proposed for an admin to approve |
| `draft`, `read` | Yes | Proposed: an [agent request](agent-requests.md) a person approves; an image upload is refused | Proposed for an admin to approve |

## Limits

- 2,000 articles per workspace, counting the trash, and 1,000 collections.
- 100,000 characters per article text, 300 per title; never cut.
- Images: PNG, JPEG, GIF and WebP, 5 MB each, at most 8,192 pixels on a side and 40 megapixels.
- 10,000 images and videos in the media library per workspace, counting its trash; videos up to
  200 MiB and 8,192 pixels on a side, three uploads in progress at a time.
- Collections go three levels deep.
- Search does not find a part in the middle of a word, nor words in text written without spaces.
- Translations an import brings in are kept and exported, but not shown or edited yet.
