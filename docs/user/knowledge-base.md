# Knowledge base

The knowledge base holds your team's help articles, written in Markdown and grouped in collections.
Every agent can write, edit, publish and delete articles. A published article appears on the public
help center while an admin has it turned on; a draft is seen only by agents. Every change is kept as
a version you can restore, and a deleted article or collection waits in a trash for 30 days. AI
agents can do the same (see [For developers and AI agents](#for-developers-and-ai-agents)).

Open it from **Knowledge base** in the sidebar (on a phone, in the bar at the top). Moving from
Freshdesk? `collie import freshdesk` brings your Freshdesk knowledge base over with its collections,
dates and images; see [Import from Freshdesk](import-freshdesk.md#knowledge-base).

## Collections

A collection groups articles, like a Freshdesk category, folder and subfolder: collections go three
levels deep. Every article has a home collection, so a new knowledge base starts by asking for
one, for example "Getting started". An article can also be listed in more collections; see
[Several collections](#several-collections).

- **Add**: select **New collection**, type a **Name** (and a description if you like), choose where it
  goes under **Inside** (asked once there is a collection to put it in), then **Create collection**. To add one inside the collection you are looking
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

### Order

Collections and the articles in a collection are listed by name (or title) until you order them by
hand. The order you set is the one the help center, the collection's article list and the reply
box's **Articles** list use.

- **An article**: select its collection, with no search and **All** as the status. Each row has
  **Move up** and **Move down** (the arrows at the end of the row). Drafts have a place too; readers
  see only the published articles, in the same order.
- **A collection**: select it, then **Move up** or **Move down** next to **Delete**. It moves among
  the collections beside it (in the same collection, or at the top).

The buttons are reached with `Tab` like any other, and a screen reader hears where the row went
("Moved Billing to place 2 of 5."). Once a collection is ordered by hand, an article that is new,
moved into it, brought in by an import or taken out of the trash goes last. In the same way a
collection that is new, imported or taken out of the trash goes after the others once they are
ordered; until then it falls in by name. If someone added, moved or deleted an article there a moment
before, Colliedesk takes their change into account and moves yours again; if that fails too, the page
says so and nothing changes. If someone else moved or deleted the very row you moved, the page says
so and shows the list as it is now. The order is not part of an article's history, so restoring a
version does not move an article.

### Several collections

One answer often belongs under two topics, for example "Get a VAT invoice" under both **Billing** and
**Bookings**. Keep one article and list it in both, instead of two copies that drift apart.

1. Open the article. In **Placement**, **Collections** shows its home collection, marked **Home**.
2. Choose a collection in **Also list in…**. It appears as a row under the home.
3. Select **Save**.

The help center now lists the article in each of these collections. Every link opens the same
address, `/a/<number>-<address name>`. Adding or removing a collection never changes that address.

- **The home** gives the article's breadcrumb on the help center and its place when you order a
  collection by hand. In the other collections the article comes after that collection's own
  articles, by title. Choosing another home moves the article; if it was listed there already, it
  is no longer an extra row.
- **Remove** a collection with **×** on its row, then **Save**. Only that listing goes; the article,
  its address and its other collections stay.
- **Search, sitemap.xml, llms.txt and AI answers** return the article once, at its address.
- **The articles list** of a collection in Colliedesk shows the articles whose home it is. The
  number next to a collection counts those too.
- **Delete a collection** that an article is only listed in: the article stays in its home and
  leaves that list. Restore the collection from the trash and the article is listed there again.
  A collection that is still the home of an article cannot be deleted, as before.
- **Import**: the Freshdesk import brings each article into one collection, as Freshdesk has it.

## Find an article

Type in **Search articles**. The search finds whole words and the start of words in titles and text,
and any part of a title, so `pass` finds "password". It does not find a part in the middle of a word
in the text (`word` does not find "password"), and in text written without spaces between words
(Chinese, Japanese, Thai) it finds only the start of the text or a title. **All**, **Published** and
**Drafts** filter by status. The search, the status and the collection are in the address, so a
link opens the same list. On a phone the collections are a list at the top of the articles.

Each row says whether readers can see the article: a published article is public only while the help
center is on. While it is off, a banner at the top says so, with **Turn on in Settings**, and each
published article says "not public"; a draft says "agents only". With the help center on, the arrow at
the end of a published article's row opens its public page.

## Write an article

1. Select **New article** (inside a collection, it starts there).
2. Type a **Title** and the **Text** in Markdown: `**bold**`, `_italic_`, `#` headings, `-` and `1.`
   lists, `- [ ]` checklists, `[links](https://example.com)`, tables, `> quotes` and code. The Markdown
   is colored as you type, and what you type is exactly what is stored. Raw HTML in the text is not
   shown, except a [YouTube or Vimeo video](#videos).
   - The toolbar sets the block (**Text**, **Heading 2**, **Heading 3**; the title is the article's
     Heading 1, so the text starts at Heading 2, and a `#` line written before still shows as **Heading 1**)
     and adds bold, italic, a link, numbered and bulleted lists, a quote and a table; **Insert image, video
     or callout** adds an [image](#images), a [video](#videos) or a [callout](#callouts); **Link to article** links
     [another article](#link-another-article-from-the-editor); **⋯** (More formatting) holds
     strikethrough, code, a checklist, a [code block](#code-blocks), a divider and redo. On a phone, and in an editor
     narrower than 720 pixels, the toolbar keeps **Insert**, bold,
     italic and link, and the rest, **Link to article** included, is under **⋯**. With the keyboard, Tab reaches the toolbar and the
     arrow keys move along it.
   - Shortcuts: `Ctrl+B` bold, `Ctrl+I` italic, `Ctrl+E` code, `Ctrl+K` link, `Ctrl+Z` undo,
     `Ctrl+F` find and replace (`Cmd` on a Mac).
3. Choose the **Collection** (under **Placement** on the right; on a phone, under **Details**).
4. Select **Save draft** to keep it for agents only, or **Save and publish**.

Every action on an article is in the bar at the top: **Save**, **Publish** for a draft, and **⋯** with
**View on help center**, **Copy link**, **Version history**, **Publish** or **Unpublish**, and
**Delete article…**. The column on the right only shows the article's **Status** (with **Turn on
help center** while it is off), [who can see it](#visibility), the images that still need alt text, and
its **Placement** (collection and address name). On a phone the bar is at the bottom of the screen: the
status, **Details** (status, who can see it, alt text, collections, address), **⋯** and **Save**.

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

### Autosave

An article that is already saved once saves itself while you write: 3 seconds after you stop typing,
and at once when you switch to another window or tab, or close it. The status next to **Save** says
**Saving…**, then **Saved 12:04** (in your time zone).

- Autosave never changes the help center. On a draft article it updates the draft. On a published
  article it adds to its [unpublished changes](#unpublished-changes).
- Autosave adds no version to the [history](#history). **Save** (`Ctrl+S`) adds one, as before.
- Autosave never publishes and never counts as an approval. While the text is in review, an autosave
  replaces the review as **Save** does (see [Review before publishing](#review-before-publishing)).
- Without a connection the status says **Offline, kept on this device**. The text is kept in this
  browser, for you and this article only, and saved when the connection comes back.
- If the browser still holds text newer than the saved article when you open it, the editor asks
  **Restore unsaved text from 12:03?**. Select **Restore** to put it back, or **Discard**.
- A new article saves for the first time with **Save draft** or **Save and publish**. A
  [translation](#translations) saves with **Save** only.

If a teammate (or an AI agent) saved the article after you opened it, your save or autosave is
refused and nothing is overwritten. The editor shows both texts line by line: struck through is only
in theirs, highlighted is only in yours. Choose **Save mine over theirs** (theirs stays in the
history) or **Discard mine and show theirs**. A translation does the same.

### Full screen

Select **Full screen** in the toolbar to write with the whole window. The toolbar stays. Press `Esc`
or select **Leave full screen** to go back. `Esc` first closes an open menu or dialog.

### Paste from Word, Google Docs or a web page

Copied text from Word, Google Docs, Notion or a web page goes in as Markdown. Headings, bold, italic,
lists, links, tables and code stay. Fonts, sizes, colors and underline are left out. Players,
frames and scripts are left out too.

- The copy's images go to the [media library](#media-library) first, like a pasted file. An image at
  a web address is downloaded by Colliedesk and stored. While they upload, a box at the cursor says
  so; **Cancel** there drops the paste.
- An image that cannot be stored is left out, and the editor says why. The rest of the paste goes in.
- After the paste, a bar offers **Keep formatting · Paste as plain text**. One click writes the paste
  the other way. The bar goes when you type on.
- In a code block, and from a code editor such as VS Code, a paste stays plain text.

### Callouts

A callout sets a short passage apart: a note, a tip, something important, a warning or a caution.
**Insert image, video or callout** adds one (**Note callout**, **Tip callout**, **Important callout**,
**Warning callout**, **Caution callout**), with its words selected so you can type over them. It is
written in GitHub's alert syntax, a quote whose first line is only the type:

```markdown
> [!WARNING]
> Deleting a workspace cannot be undone.
```

The types are `[!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]` and `[!CAUTION]`, in capitals or not.
The preview and the help center show each type with its own icon, colour and label; the label is in the
page's language, and screen readers read it before the words. Any other first line, or a type
followed by more words on the same line, stays a plain quote. A callout counts only at the top level
of the text, not inside a list or another quote.

### Code blocks

**⋯ → Code block** asks for the code's language (`go`, `python`, `shell`, `json` and so on) and writes it
after the opening fence:

````markdown
```go
fmt.Println("hello")
```
````

The help center colours a code block that names a language it knows, in the light and in the dark
theme. The colours are worked out on the server and are part of the page's style, so the page still
runs no script. A block with no language or a language it does not know stays plain text. An article
colours its code blocks in order up to 10,000 characters of code in all: a block that would go past
that stays plain, and so do the blocks after very slow code. The editor's preview shows code blocks plain. On the help center, one click selects
all the code in a block, ready to copy.

### On this page

An article with 3 or more Heading 2 and Heading 3 headings lists them under **On this page**, each
linking to its heading (the first 50, in a very long article). In a wide window the list is beside the text; on a phone it is above it. It is
part of the page, so it needs no script. To turn it off for an article, clear **Show On this page**
under **On this page** on the article page (on a phone, under **Details**). The switch is the
article's, so it applies to every language, and it is in the [export](export.md). On a published
article, changing it is one of the [unpublished changes](#unpublished-changes): the help center keeps
the list as it is until you publish them. Restoring an older version leaves it as it is.

### Link another article from the editor

To point from one article to another, select **Link to article** in the toolbar (under **⋯** in a narrow
editor) and choose the article. It is the same list as **Articles** in the reply box: the published
articles in the help center's [order](#order), with a search, `Enter` or a click to pick and `Esc` to close.
The article you are editing is not in it, nor is an article that is not for [everyone](#visibility),
since it has no public page. The list is empty while the help center is off.

- With text selected, the selection becomes the link text: `to reset it` becomes
  `[to reset it](/a/7-reset-your-password)`.
- With nothing selected, the article's title is the link text, where the cursor is. Signs such as `*`
  or `_` in a title show as written.

The address is relative to the help center (`/a/7-reset-your-password`, with no host). The help center
adds its own address in front when it shows the page, so the link works on the workspace address, under
a path, and on a [custom domain](#custom-domain) alike. `Ctrl+Z` takes the link back out.

### Images

Paste an image into the text, drag it in, or select **Insert image, video or callout → Upload from this
device**. Colliedesk uploads it and writes an image line, `![name](…)`, on a line of its own after the
paragraph, list or table you are in;
while it uploads, a box says so and nothing is added to the text. A video pasted, dropped or chosen
the same way goes up in parts, the box shows how much has gone, and it goes in as a
`<video controls preload="metadata" src="…"></video>` line; leaving the page cancels a
video still uploading. **Insert image, video or callout → From the media library** opens the library in a
dialog: search it, pick one or more images and videos (they go in in the order you picked them, each a
block of its own), and check each one's alt text (a video's title). An image without alt text in the
library asks for it, and **Insert** waits until it has one; what you type is saved to the library
too, so the next article gets it. In the grid, Tab stops once, the arrow keys move, Space picks and
Enter inserts. On a phone the library fills the screen and asks for the alt text on a second step
(**Next: alt text**). A file that is not an image or a video, or one over its limit, is refused with the
reason before anything is sent. PNG, JPEG, GIF and WebP up to 5 MB
each are accepted, at most 8,192 pixels on a side and 40 megapixels. Images count toward your plan's
storage. The same image uploaded twice is stored once. Saving waits until every image and video has
finished uploading.

Location, camera details, dates and comments that a phone or editor leaves in an image are removed
before it is stored, without changing the picture: a photo keeps the way it should be turned. Images
an import brings in keep their bytes as the other helpdesk served them.

Every uploaded image goes into the workspace's media library, with a name, alt text and a small
preview. An image no article ever saved is deleted 30 days after it was uploaded, unless it is kept;
naming it or writing its alt text keeps it.

**Alt text.** Screen readers read an image's alt text out; an image without it says nothing to them.
The column on the right of the editor lists the images of the article that have none, each with
**Add alt text**, which writes it in the text (and in the library when the library has none).
**Publish** asks first when an image still has no alt text: add it, or **Publish anyway**.

### Media library

**Knowledge → Media** shows every image, video and [file](#attach-files) of the workspace, newest first, with a search by
name or alt text, a choice of images, videos or files, of what is in use, not used or missing alt text
(on a phone, under **Filter**), and a sort: newest first, name, or largest first (what frees the most
storage). A link with `sort=oldest` still lists oldest first. A card shows only what
needs your attention: **No alt** on an image without alt text, **Not used**, and a pin on a kept item.
An image the help center shows says **Used by the help center (logo)** (or favicon) and is in use.
**Upload** takes several files at once, or drop them anywhere on the page; an upload made here is
kept. The uploads gather in a tray in the corner of the library, one line with how many went in, are
already in the library or failed, and a bar of the three; open it for each file's progress. A video
shows its speed and the time left, and can be paused, resumed (also after a lost connection: it goes
on from the bytes the server already has) or cancelled. A file that failed opens the tray by itself
and says why (a wrong type, a file over the limit, a video the server refuses), with **Dismiss**, and
**Try again** when another try can work. The same file uploaded twice says **Already in the
library**. Leaving the page cancels the videos it was still sending. A video uploaded from this
browser gets a poster drawn from its first second; change it under **File** in its panel.

A workspace has at most three video uploads in progress. **Uploads in progress**, in the tray, lists
those this page is not sending (from another tab, or a page that closed), with who started each and when its
parts expire (24 hours after the start): **Resume…** goes on when you choose the same file again, and
**Cancel** frees its place; only the person who started an upload, or an admin, cancels it. A
video resumed here is cancelled too if you leave the page before it finishes, whoever started it.

Choose an item to see it in the panel on the right (on a phone, a sheet over the grid). Under its
preview, **Embed** copies what to put in an article, **Download**, **Replace…** and the trash. Three
tabs hold the rest: **Details** (rename it, write its alt text or a video's title for screen readers,
keep it), **Used in** (the articles that show it, with links) and **File** (type, size, dimensions,
length). **Save** and **Discard** stay at the foot of the panel. A video's **Poster**, under **File**,
is the frame the player shows when you press **Use this frame** (pause it where you want), or an image
you upload.

A line above the library says how many items no live article shows, how much space they take and
how many of them are kept; **Review not used** opens **Not used**. Its count is what its list holds,
kept items included, and it says which go to the trash on their own (an item nobody kept and no
article, deleted article or older version names, 30 days after its last upload) and why the others
stay (kept: uploaded on the Media page, or marked "Keep even when no article uses it"). **All**,
**Not kept** and **Kept** filter the list. Tick items (Shift picks a range) and **Move N to trash**,
or with nothing ticked **Move all N to trash**: on **All** that takes the kept items too, on **Not
kept** only the others. A confirmation names how many, how large, which of them are kept and which
a deleted article still shows; restoring an item brings it back as it was, kept or not, and Undo
puts the whole batch back. **Trash** lists what is there, each item with the day the purge deletes
it; the help center's logo or favicon has no such day, since it stays until the brand names another
image. Restore items, or, as an admin, **Delete forever** after a second confirmation; the answer
says how many were deleted and how many stayed because the help center shows them.

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

An article can show a YouTube or Vimeo video. Select **Insert image, video or callout → YouTube or Vimeo
link**, paste the video's link (a watch page, a `youtu.be` short link, a Vimeo page, or a player
address), and press Enter: the player goes in a block of its own after the one the cursor is in. A
YouTube link's start time (`?t=1m5s`) is kept. A link the help center cannot show is refused, with
the reason; Escape closes the box.

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
- A video of your media library shows with the `<video>` line the editor writes for it (see
  [Images](#images)).
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
library](#media-library) or from the editor (above). **Preview** shows a video with its poster, as
the help center does, and leaves out (and names) one the library does not have.

### Attach files

An article can offer files to download under its text: a warranty form, a price list, a manual. The
**Attachments** strip under the editor takes them: drop files on it, paste them, or **Choose files**.
Each goes up with a bar of its progress and shows its type and size. Move a file up or down to set
the order readers see, or remove it from the list; the file stays in the [media library](#media-library).
On a published article the list is part of its [unpublished changes](#unpublished-changes) until
**Publish changes**; a translation has a list of its own, which changes at once. Where
[approval is required](#review-before-publishing), the list is part of what a reviewer approves.

- Accepted: PDF; Word, Excel and PowerPoint (`doc`, `docx`, `xls`, `xlsx`, `ppt`, `pptx`);
  OpenDocument (`odt`, `ods`, `odp`); CSV, TXT and RTF; ZIP, 7z and `tar.gz`. The type is judged from
  the file's first bytes and its name, so a program renamed `.pdf` is refused. Programs, scripts,
  HTML and SVG are refused, and the message lists the accepted types.
- 25 MiB per file and 100 MiB for the files of one article (or translation), at most 50 files.
  A file over a limit, or of a refused type, says why in the strip before anything is sent.
- Files count toward your plan's storage, and the same file uploaded twice is stored once.
- The text of a PDF, TXT or CSV file (its first 200 KiB) is read for search: the help center's search
  and AI answers find an article by a word that is only in its file. Other types are found by name.

On the help center the files are listed under the article as **Downloads**, each with its name, type
and size. A file downloads from `/files/<id>/<name>` on the help center's address, always as a
download (never opened in the browser), and only while a published article or translation lists it;
otherwise the address says "This page does not exist". The article's Markdown copy and `llms.txt`
list the files too.

To change a file everywhere it is listed, open it in the media library and **Replace…** it with the
new version from your computer. It keeps its address, so every link to it (in an article, an email,
a bookmark) downloads the new version; Undo puts the earlier one back. A file a published article
lists needs the permission to publish to be replaced, and a file an article lists cannot go to the
trash: remove it from the article first.

### Publish, unpublish, edit

An article shows **Draft** or **Published**. **Publish** puts a draft on the help center and
**Unpublish** (in **⋯**) takes it back off; both are versions, so neither is final. **Save** keeps an
article's status: on a published article it saves [unpublished changes](#unpublished-changes), and the
help center keeps the published text. **⋯ → Delete article…** asks first, then moves the article to the
[trash](#trash).

Each article has a number that never changes. Its help center address ends in
`/a/<number>-<address name>`, and the article page shows it under **Address name**. You can change the
address name; links with the old one still lead to the article because the number does not change.

### Search title and meta description

Under **Search engines** on the article page, **Search title** (up to 120 characters) is the title
Google and other search engines show, and **Meta description** (up to 320) the text under it. Left
empty, the article's title (followed by the help center's title) and the first 160 characters of its
text are used. Line breaks and repeated spaces become one space. Both are part of the article's
history and of the [export](export.md). On a published article they are part of its
[unpublished changes](#unpublished-changes) until you publish them.

A save against a newer copy is refused and shows both texts to choose from (see [Autosave](#autosave)).

Leaving the page with unsaved changes asks first.

### Tags

**Tags** on the article page label an article for the team: up to 20 per article, each 1 to 40
characters. A tag is kept in lower case without spaces around it, so `Billing` and ` billing ` are the
same tag. Tags belong to the article, so its [translations](#translations) share them, and they are not
the tags of conversations. A tag change is part of the article's history, its
[review](#review-before-publishing) and the [export](export.md). On a published article it waits in the
[unpublished changes](#unpublished-changes) until you publish them.

The help center search and the help chat find an article by a tag word as they do by a title word.
Readers see the tags on the article page only when **Show tags on articles** is on in the
[help center settings](#help-center-settings).

### Author

**Author** on the article page names the team member the article is credited to: the person who
created it, until someone changes it. Changing it needs `kb_articles:update`, applies at once (also on
a published article) and adds an **Author changed** version to the [history](#history) with the author
before and after. The author must be an active member of the team. A member who left still shows as the
author, with **(left)** after the name.

### Unpublished changes

Editing a published article never changes its help center page by surprise. **Save** keeps your
changes apart from the published article, and the article shows **Published · Unpublished changes**.
The help center, its search, `llms.txt`, the sitemap, the `.md` copy and the help chat keep the
published text until someone publishes the changes. An image that only the unpublished changes show
stays off the help center until then.

1. Edit the article and select **Save**. The bar **Unpublished changes** says who saved them.
2. Select **Preview** to read the text as it will look.
3. Select **Compare with published** to see each changed line: what is removed and what is added.
4. Select **Publish changes** to put them on the help center. It needs `kb_articles:publish`.
5. Or select **Discard draft**. The published article and its history stay as they are.

An article has one set of unpublished changes; every save adds to it, and each save is a version in
the [history](#history). In the article list, published articles with unpublished changes say so, and
the status filter **Unpublished changes** keeps only those.

- **Unpublish** an article with unpublished changes: the changes become the text of the draft article.
- **Delete** it: the unpublished changes go to the trash with it and come back when you restore it.
- A [media replacement](#media-library) also replaces the item in unpublished changes, and its undo puts
  it back there.
- A [translation](#translations) of a published article still goes live when you save it
  ([#694](https://github.com/colliedesk/colliedesk/issues/694)); where publishing
  [needs approval](#review-before-publishing), that save is refused.

## Review before publishing

A team can ask that a teammate approves each new or changed article before it goes on the help center.
Open **Knowledge base → Settings** and turn on **Require approval before publishing**. Only a role
that may publish articles (`kb_articles:publish`) changes it. Each collection can override it:
**Edit** a collection and set **Approval before publishing** to **As the help center**, **Required**
or **Not required**. An article in several collections needs approval when any of them requires it.

Where approval is required, **Publish** and **Publish changes** stay off until a teammate approves the
exact text, and the article says why: **Publishing needs a teammate's approval: send it for review.**

1. Save the article (or its [unpublished changes](#unpublished-changes)).
2. Select **Send for review**. Choose one or more **Reviewers** (up to 10) and add a **Note to the
   reviewers** if you like. Only teammates who may publish are offered, never you.
3. Each reviewer gets an email within a minute and finds the article under **Reviews**. The article
   says **Waiting for approval from** them.
4. A reviewer approves it, or asks for changes with one line you read on the article.
5. Once approved, the article says **Approved by** the reviewer: select **Publish** (or **Publish
   changes**).

You get an email when your review is decided. Steps for the author: save, send for review, publish (3);
for the reviewer: open **Reviews**, open the article, approve (3).

### Reviews

**Reviews** at the top of the knowledge base shows how many texts wait for your decision. It has three
views:

- **Waiting for you**: the texts you were asked to review. Tick several and select **Approve N**.
- **In review**: every text waiting for a decision, with who sent it and who was asked.
- **Approved**: approved texts not published yet. Tick several and select **Publish N**.

Open one to see **Live now** (the published text, or **Not published yet**) beside **Proposed**, and
**Changes**, each removed and added line, then **Files**, the files it lists with the ones added and
removed. **Approve** approves it; **Request changes** sends it back
with **What to change**. A text an AI agent wrote shows **Written by an AI agent through** its token.

### Who may approve

- A reviewer holds `kb_articles:publish` and is chosen when the text is sent.
- Nobody approves their own text: not the person who sent it, nor anyone who wrote part of it or changed
  its files since it was last published.
- A text an AI agent sent through a token is never approved by that token or by another token of the
  person who made it. A token acts for its maker, so it approves only for a reviewer, and the history
  names the token.
- An approval holds for the exact text: title, address name, text, collections, search title, meta
  description and the [files](#attach-files) it lists, in their order (a translation: its own files). Any later
  edit, a change of the file list too, needs a new approval: the article says **Your edits need a new approval:
  save them and send them for review.**
- An edit while a text is in review replaces the review (**Replaced by an edit**) with a new one for the
  same reviewers, except a reviewer who made the edit. Reviewers are emailed again (**Review again**).
  An [autosave](#autosave) is an edit too: it replaces the review, and an approval of the earlier text
  stops counting.

**Withdraw** takes a review back: the sender, or anyone who may publish, can withdraw it. Only one review
of an article (or of one translation) is open at a time.

Every way to publish checks the approval: **Publish**, **Publish changes**, **Publish N**, a new
article created published, a translation set to published, and every API or MCP call. A refused call
answers 409 `approval_required` and says what to send. Three changes keep working as before:
restoring a published article from the trash brings back the text that was live; a
[media replacement](#media-library) changes the published articles that show the item (it needs
`kb_articles:publish`); and an [import](import-freshdesk.md#knowledge-base) brings articles with the
status they had in Freshdesk.

### Review a translation

A [translation](#translations) needs approval where its article does, and is reviewed on its own. It has
no unpublished changes, so to change a published translation: set it back to draft, edit it, send it for
review, and publish it once approved. Saving a published translation is refused and says so.

### Reviews in the history

The article's [history](#history) lists each request, approval, request for changes and withdrawal,
with who made it and through which token. The publish that follows is a version of its own. The
[export](export.md) includes every review and who was asked.

## Templates

A template is the text a new article starts with, for example the headings of a how-to. Select
**Templates** at the top of the knowledge base to see them. A new workspace has three: **How-to**,
**Troubleshooting** and **FAQ**. Edit or delete them like your own; a starter you delete never comes back.

To start an article from a template:

1. Select **New article**. While the workspace has templates, it opens a menu: **Blank**, then the
   templates, and **Manage templates**. In a collection with a
   [default template](#default-template-of-a-collection), that one comes first, marked
   **Default for this collection**.
2. Select a template. The editor opens with its text and an empty **Title**.
3. Type the title and save.

The article gets a copy of the text. Editing or deleting the template later never changes an article
made from it.

**Insert template**, next to **Write**, **Side by side** and **Preview**, adds a template's text where
the cursor is, in an article you are already writing. It is not offered in **Preview**.

### Manage templates

- **New template** asks for a **Name** (unique, ignoring case), **When to use it** (the line the
  **New article** menu shows) and the **Text (Markdown)**.
- **Edit** changes them. Every save is a version: **History** lists each one with who made it and
  through which AI agent token, and **Restore version N** makes an older one current again, as a new
  version.
- **Delete** moves a template to **Template trash** for 30 days; **Restore** there brings it back with
  its history. A template in the trash cannot start an article: an article opened from it meanwhile
  starts blank and says so.

### Default template of a collection

**Edit** a collection and choose its **Default template**: every new article in that collection starts
from it, and **Blank** in the menu still starts one empty. Deleting the template sets the collection
back to **None (blank)**. The **Default template** field shows only while the workspace has templates.

### What stays private

Templates never appear on the help center, in its search or to its visitors. An image or video that
only a template names stays private until an article that uses it is published; the
[media library](#media-library) counts it as in use, so it is not listed as unused. The
[export](export.md) includes every template with its history.

A workspace holds at most 200 templates, counting the ones in the trash. A name holds 200 characters,
**When to use it** 300 and the text 100,000.

Moving from Freshdesk: Freshdesk does not offer its article templates through its API, so
`collie import freshdesk` brings none. Add the ones you need under **Templates**.

## History

**⋯ → Version history** on an article lists every version, newest first: who made it (and through which AI
agent token, or an agent request approved before requests were removed), when, and what changed (title, address name, text, status,
collection), and each [review](#review-before-publishing) request and decision. **Restore version N** makes that version's title, address name and text current again,
as a new version; the status and collection stay as they are, so a restore never publishes or moves
an article by surprise. On a published article the restored version becomes its
[unpublished changes](#unpublished-changes); the help center keeps the published text until you publish
them. Save or undo your own changes first.

## Votes from readers

**Readers' votes** on an article (on the right; on a phone, under the text) shows what readers of the
public page answered to **Was this article helpful?**:

- The count the page shows, such as "13 out of 17 found this helpful". Votes imported from
  Freshdesk count toward it, and the box says how many of them there are.
- Each reader's vote, newest change first: **Yes** or **No**, the day, and what they said was missing.
  Readers are anonymous. The first five are shown;
  **Show all** lists the rest.
- **Earlier counts**: the count before each reset, with who reset it.

One reader has one vote per article: voting again changes it. One visitor address adds at most 10
new readers to an article a day; past that the page still thanks the reader but the vote is not
counted. For this check each vote keeps a keyed hash of the address, never the address itself. Emails and numbers of 6 digits or more
in what a reader writes are replaced with "[email removed]" and "[number removed]" before anything is
stored, and the line is cut to 500 characters.

**Reset votes** (with the right to edit articles, `kb_articles:update`) starts the count again from 0,
for instance after you rewrite the article. It asks first. The current votes and their lines stop
counting and leave the list; nothing is deleted. **Earlier counts** keeps the count the page showed,
and the [export](export.md) keeps every vote and reset. After a reset the Freshdesk votes no longer
count. A reset cannot be undone.

## Keep an article away from AI

AI features (reply drafts and triage suggestions, once an admin turns them on) find the published
articles that fit a conversation by themselves, in their own language and in each published
[translation](#translations), and quote them; nobody has to sort the knowledge base for AI. A draft
(of an article or of a translation), anything in the trash and an internal note of a conversation
are never read.

To keep one published article away from AI, open it and select **Exclude from AI** under **Used by
AI** (on the right; on a phone, under the text). From then on drafts and suggestions skip it in every language; people and the help center
still see it. The article says since when and by whom, and its history has the change. **Let AI use
it again** undoes it. Neither changes the text, so an edit someone has open still saves.
To keep an article for your team but out of help chat and the help center, make it
[Agents only](#visibility) instead.

## Visibility

**Who can see this** sets who reads an article. It is in the column on the right of the editor (on a
phone, under **Details**), and in a collection's **Edit** form.

| Choice | Who sees it |
|---|---|
| **Everyone** (the default) | The help center, help chat and your team. |
| **Help chat only** | Help chat may quote it, without a link, as "Our help notes". The help center never shows it. |
| **Agents only** | Only your team: reply suggestions and AI drafts. |

A collection narrows every article in it and in the collections inside it. An article is as restricted
as the most restrictive of itself and every collection it is in, their parents included. When a
collection keeps an article more restricted than its own choice, the editor says so, for example
"Agents only because of Internal". Moving an article into such a collection, or a collection under one,
restricts it at once.

A help center page that is not for everyone answers as if it did not exist (404). The help center's
search, `sitemap.xml` and `llms.txt` leave it out, and so does an address it had before. An AI draft may
use a restricted article, but never puts its address in the reply.

Raising who can see a published article (for example from **Agents only** to **Everyone**) needs the right
to publish (`kb_articles:publish`), whichever way it happens: the article's own choice, its collection's
choice, moving the article, or moving the collection. Where publishing needs [approval](#review-before-publishing),
each such article also needs an approved review of its current text. Otherwise nothing changes, and
Colliedesk names the articles: "2 articles would become public without an approved review: …". Making
an article more restricted needs only the right to edit (`kb_articles:update`).

The knowledge base list marks each article that is not for everyone (**Help chat only**, **Agents
only**), and **Who can see** above the list keeps only the articles with that choice.

## Translations

A translation is the same article in another language, not a new article: it has the article's
number, collection and place, and its own title, text, address name, search title and description,
status (draft or published) and [history](#history). Customers see a translation only while it is
published, its article is published, and its language is shown.

### Languages

**Knowledge base → Languages** lists the help center's languages. The primary language is the one
the articles are written in (set it with the help center's **Language** in
[Help center settings](#help-center-settings)). Add another language with its code (`fr`, `de`,
`pt-BR`); it starts **hidden**, so you can translate first and show it when it is ready. Tick
**Shown to customers** to show it, untick it to hide it again; nothing is deleted either way. The
arrows set the order of the language menu. A language that still holds translations or collection
names, in the trash too, cannot be removed: hide it, or delete what is written in it first.

Each language says how many articles are published, drafts, **not translated** and **outdated** in
it. Select a count to list those articles; each opens its translation.

### Translate an article

Open an article: **Translations** in its sidebar lists every language with the translation's status,
**Outdated** when the article changed since, or **Not translated**. Select a language to open the
translation beside the original (side by side on a wide screen, one above the other on a phone).
A new translation starts empty; **Start from the original text** copies the article's text to
translate over it. **Save translation** keeps it as a draft; **Publish** puts it on the help center
once the language is shown. Steps: open the article, select the language, write, Publish (4).

### Outdated translations

When the article's own title or text changes (an edit, a version restored, a newer import), its
translations are marked **Outdated**; changing the article's collection, status or images does not.
Open the translation, bring it up to date, tick **Mark as up to date with the original** and save,
or select the same button without changing the text. **Mark as outdated** sets it back. Both are
versions of the translation, so its history says who did it.

### Collection names

On a collection, **Names in other languages** sets its name and description in each language. A
language without one shows the primary name. **Remove** takes it away; a name an import brought in
comes back with the next run of that import.

### History and trash

**Version history** and **Delete translation** are on the translation's page. A deleted translation
goes to the [trash](#trash) for 30 days with its history; the article stays. While it is there the
article cannot get a new translation in that language: restore it instead.

### Imported translations

A [Freshdesk import](import-freshdesk.md#knowledge-base) brings every translation with its status,
search title and description, the categories' and folders' names in each language, and the
languages: shown when the Freshdesk portal showed them, hidden otherwise. An import never changes a
language you already set, nor a translation someone edited in Colliedesk.

## Trash

Select **Trash** at the top of the articles to see deleted articles and collections. Each shows who deleted it and how many days
are left; after 30 days it is deleted for good, with its history. **Restore** brings it back as it
was: a published article is published again. Restore a collection before the articles that were in
it. Deleted translations are listed there too. Articles in the trash count toward the 2,000 articles a workspace can hold.

## Help center settings

Open **Knowledge base → Settings**. It needs only
`help_center:read` (Agent and Admin hold it), not the articles' permission: a role without
`kb_articles:read` still finds the **Settings** tab beside **Media**, and Knowledge base in the sidebar opens it when the
role reads neither articles nor media. Everyone who opens it sees whether the help center is on. An admin can turn
it on or off with **Publish the help center** and set its **Title** (empty uses the workspace name).
Turning it off takes every page offline at once; turning it on again publishes the articles marked
published. **Show tags on articles** shows each article's published [tags](#tags) on its page; search
finds articles by tag either way.

### Brand

An admin sets the help center's brand in **Knowledge base → Settings → Brand**. Every page
shows it on the next visit (see [what visitors see](#what-visitors-see)). Agents who are not admins
see the saved brand and its preview, without the controls. A brand has:

- **Logo** and **favicon**: images of the [media library](#media-library), outside its trash, with
  the logo's text for screen readers (empty uses the title). An image the help center uses is kept
  while it uses it: the library shows **Used by the help center**, it never counts as **Not used**
  and is never deleted on its own. It can still go to the trash; it is then not shown, restoring it
  brings it back, and a brand read and saved again whole keeps it. **Delete forever** leaves it in
  the trash (the answer is `in_use`) until the brand names another image. Once the brand names
  another image, the old one is no longer kept: **Remove unused** can take it, and the purge deletes
  it 30 days after its last upload unless you keep it.
- **Website link** (an `http` or `https` address) and **contact**: an `https` address or `mailto:`
  followed by one plain email address (`mailto:help@example.com`; no name, no `?subject=` or `?bcc=`,
  no `%` encoding), and a phone number (digits, spaces and `+ - ( )`).
- **Colors**: primary, header, header text, background, text, links and footer, each `#rrggbb`. Any
  color set makes the help center light only.
- **Fonts** for text and headings, from fonts already on the reader's device, so nothing is
  downloaded: **Sans** (`system-sans`, the default text font) or **Serif** (`classic-serif`, the default
  heading font). Five other fonts cannot be chosen in the app yet (see
  [For developers and AI agents](#for-developers-and-ai-agents)). A brand saved with one of them keeps
  it, and the form shows it until you choose another.
- **Custom CSS** (`custom_css`), for details the fields above do not cover, such as
  `.masthead{border-bottom:2px solid #0b5cad}`. See [custom CSS](#custom-css).

Every color pair must be readable (WCAG AA): 4.5:1 for text, links, header text, footer text and
the button text on the primary color (white or black, whichever reads better), and 3:1 for the
primary color against the background. A brand that fails is not saved; the answer names each color
with its ratio and the nearest color that passes, for example "Text on the background is 2.85:1; it
needs 4.5:1. #767676 passes."

In Settings:

- **Logo** and **Browser tab icon**: **Upload** adds an image to the media library and uses it;
  **Choose from library** picks one already there; **Remove** goes back to none.
- **Colors**: type `#rrggbb` or use the color picker. An empty color uses the default, which shows
  greyed in the field. Under each color is its contrast; one that would be refused says why and
  which color passes. **Reset** clears one color; **Reset to default look** clears the whole brand
  in the form (nothing changes until you save).
- **Your website**, **Contact link** and **Phone number**: while one is not yet a complete address
  or number, the preview leaves it out and its field says what shape it needs.
- **Advanced** holds **Custom CSS**. It is folded until the brand has some.
- **Save brand** saves it. A refused save shows each message under its field and moves the focus to
  the first one; editing a field clears its message.

**Preview** shows the home page or an article with the brand being edited, at desktop or phone
width, about 300 ms after you stop typing, before anything is saved. It is the real page built from
your collections and published articles (never drafts), also while the help center is off; with
nothing published, the article page shows a sample text, and a video in the article shows as a box
that says it plays on the live help center. Links in the preview do not open, and the preview runs
no script; custom CSS cannot change that. Each person may ask for 600 previews a minute.

#### Custom CSS

Custom CSS is added after the brand's colors and fonts on every public page and in the preview, so
its rules win over them. Nothing may load from elsewhere: `url()`, `@import` and `@font-face` are
refused (so are image functions such as `image-set()`), and a refused save names the line and column. The
color contrast checks of the brand do not cover custom CSS, so look at the preview before you save. It is
saved and served exactly as written, at most 32,768 bytes of UTF-8 (a
letter outside ASCII takes 2 to 4 bytes; the field counts them as you type). An empty field (or only
spaces) removes it. Pages keep the same security policy: the CSS comes from the help
center's own address, in the brand's style sheet, never inline in a page. Saving the theme needs
both `help_center:update` and `kb_articles:publish`, for a person and an agent token alike; without
either it is refused and the public pages stay unchanged.

**The contrast checks do not cover custom CSS.** A color or a hidden element it sets can make a page
hard to read. Check the preview before you save, and clear the field to go back.

The help center loads nothing from other sites, so CSS that could load anything, run a script in an
old browser or reach past its own rules is refused. Nothing is saved, and the message says the line
and column, for example "line 3, column 14: url() is not allowed". Settings opens **Advanced** and puts
the cursor there. While the text is refused, the preview keeps the last custom CSS it could show, and
the field says why. These are refused:

- `url()`, and the image functions `image()`, `image-set()`, `cross-fade()`, `element()`, `src()` and
  `paint()`, with or without a vendor prefix such as `-webkit-`.
- `expression()`, and the properties `behavior` and `-moz-binding` (a class such as `.behavior` is fine).
- Every at-rule except `@media`, `@supports`, `@keyframes`, `@layer` and `@container`. So `@import`,
  `@font-face`, `@namespace`, `@charset` and `@property` are refused.
- A backslash outside quotes (an escape such as `u\72l(` spells `url(`).
- `</` and `<!--` anywhere, a bracket that does not close or closes nothing, a string or comment that
  never ends, a NUL or other control character (a tab is fine), and a byte order mark.
- `url(`, `@import`, `image-set(`, `image(`, `cross-fade(`, `element(`, `src(`, `paint(`,
  `expression(`, `javascript:` or `vbscript:` anywhere, also inside quotes or comments, once escapes
  are read.

Images and fonts of your own are not possible this way. Use the logo, the favicon and the fonts above.

## Public help center

The help center is a public website made from your published articles. Customers read it without
signing in, on Colliedesk Cloud and on your own server alike. It is off until an admin turns it on
in **Knowledge base → Settings** (see [Help center settings](#help-center-settings)).

### Its address

**Knowledge base → Settings** shows the address as a link, even while the help center is off.

- **Colliedesk Cloud:** `/help` on your workspace's own address, `https://<workspace address name>.colliedesk.com/help`.
  The rest of that address is your team's desk ([your workspace's address](workspace.md#the-workspace-address)).
  The help center's older addresses without `/help` move there (301).
- **Your own server:** `<server address>/help/<workspace address name>`, for example
  `https://desk.example.com/help/acme`. The server can give each workspace its own host name instead;
  see [workspace host names](../self-host/running-behind-proxy.md#workspace-host-names).
- **Your own domain**, such as `https://help.example.com`, once you set a [custom domain](#custom-domain).
  The other two addresses then move there (301), with the rest of the address kept.

An article's address is the help center address followed by `/a/<number>-<address name>`, for example
`/a/12-reset-your-password`. A link with an old or mistyped address name still leads to the article:
the number finds it, and the page moves to the current address. A collection's address is the path
of address names, `/c/billing/cards`; changing one is not redirected. Addresses of a Freshdesk help center you imported
(`/support/solutions/articles/<id>-...`) lead to the imported pages on every one of these addresses;
see [Old help center links](import-freshdesk.md#old-help-center-links). **Changing the workspace
address name** (Settings → Channels → Email) changes every help center address, and old links stop working.

### In other languages

Each language you show (see [Languages](#languages)) has the same pages below its code:
`<help center address>/fr/`, `/fr/a/12-<French address name>`, `/fr/c/billing` and `/fr/search`. The
primary language keeps the addresses it always had; `/<primary code>/...` leads to them. A language's
pages show only the articles published in it (with their collections' names in that language), and
its search searches only its translations. An article not published in that language yet leads to
the article in the primary language.

- **Language menu.** The header of every page lists every language you show, in your order. Each
  opens this page in that language, or that language's home where the page is not translated yet.
  A language with nothing published yet has a home that says so and links the primary language;
  search engines are told only about the pages that exist.
- **Browser language.** A visitor who opens the help center's home page with a browser set to a
  language you show (or a close one: `fr-CA` for `fr`) moves to that language's home. Other pages,
  links and search engines are never moved; choosing the primary language in the menu keeps it.
- **Words of the help center** (Search, Home, Updated, Previous…) are in the page's language for
  English, French, German, Spanish, Italian, Portuguese (also of Brazil), Dutch, Japanese, Simplified
  Chinese, Korean, Vietnamese, Polish, Swedish and Russian, and in English for any other; so is the
  "Ask AI" link to the help chat. The chat window's own words are still in English. Editing them is
  not possible yet.
- **Old Freshdesk links in a language** (`/fr/support/solutions/articles/<id>`) lead to the
  translation once it is published in that language, and to the article otherwise.
- The [AI chat](ai-chat.md) on a page of a language answers from that language's published
  translations and links their pages.

### Custom domain

The help center can answer on an address of your own domain, such as `help.example.com`. Open
**Knowledge base → Settings → Custom domain**. Everyone with `help_center:read` sees the domain and its state; an admin sets,
checks and removes it.

1. Type the host name in **Host name** and press **Set domain**. The card then shows the DNS records
   to add, each with a **Copy name** and a **Copy value** button. The state is *waiting for its DNS
   records*.
2. At your DNS provider, add the records the card shows:

   | Record | Name | Value | What it does |
   |---|---|---|---|
   | `CNAME` | `help.example.com` | the target the card shows, such as `hc-5f0c2a9e41b7d36c8a0e2f19.colliedesk.com` on Colliedesk Cloud | Sends visitors to the help center and proves the domain is yours |
   | `TXT` | `_help-center-verify.help.example.com` | the 32-character token the card shows | Proves the domain is yours |

   The `CNAME` target and the token belong to this domain setting: they are made when you set the
   name, never change while it is set, and are not used again after you remove it. Changing the
   workspace address name does not change them. Point the `CNAME` at the target the card shows, not at
   your help center's own address (`acme.colliedesk.com`): that one does not prove the domain is yours.

   On Colliedesk Cloud the `CNAME` alone is enough. Add the `TXT` record as well when your DNS
   provider cannot point this name with a `CNAME`, or flattens it into `A` records (for example a
   proxied record on Cloudflare). On a server without help center host names, the first record points
   at the server's own address and only sends visitors there; the `TXT` record is then required.
3. Press **Check now**. When DNS shows a record, the state becomes *the help center is served on
   help.example.com*. Otherwise the card says what it found under **Last check**. DNS changes can take
   a few minutes. You can also just wait: the first visitor after the change makes the server check
   again, and the domain goes live at that moment.

The certificate (`https://`) is issued automatically the first time someone opens the address.

**Does the address really open?** When a check finds the records and the help center serves on the
domain, the server then opens the address once, as a visitor would. That first visit also gets the
certificate issued, so your first real visitor does not wait. The line under the state shows the result:

- *HTTPS works: https://help.example.com opens this help center.* Everything is in place.
- *… has no valid certificate yet …*: the certificate is not issued yet. Press **Check now** again in a
  minute. If it still fails on a server you run yourself, its reverse proxy cannot get a certificate
  for the name.
- *… answers, but not with this help center …*: another server answers on the name, for example an
  old DNS record or a proxy such as a CDN in front of it. Point the name at Colliedesk only.
- *… did not answer in time* or *… could not be reached*: check that the name points at Colliedesk
  and press **Check now** again.

The daily check opens the address too and updates the line. A server without TLS opens it over
`http://`, and the line does not mention HTTPS. During the trial the address is not opened, since the
help center does not serve there yet.

#### Moving from Freshdesk

**Verify by TXT first, then change the CNAME.** The card says this under **Moving from Freshdesk?**;
its ⓘ gives the short version and links here. Add only the `TXT` record and
press **Check now**. The domain is then verified while your visitors still reach Freshdesk. Change the
`CNAME` afterwards, to the target the card shows. The domain is already verified, so the first visit
after the switch gets its certificate at once and does not wait for a DNS check. If you change the
`CNAME` first, the first visitor makes the server check DNS and ask for the certificate, which takes
longer. The old `*.freshdesk.com` addresses are covered in
[Import from Freshdesk](import-freshdesk.md#old-freshdesk-com-addresses).

#### Plans, limits and removal

**Colliedesk Cloud:** a custom domain comes with every paid plan, one per workspace. During the trial
you can set and verify it, to prepare the move, but the help center serves on it only once the
workspace is paid: trial pages are hidden from search engines, and serving them on your domain would
cost it its place in search results. Until then the card says *verified*, the help center stays on
its own address, and no certificate is issued for the domain. Within a minute of paying, the domain
serves the help center and the other addresses move there. A workspace whose trial ended unpaid
serves on no address. A server you run yourself has no plans: every workspace serves on its domain.

While the domain is active, the help center answers only help center pages on it: the app, the API
and MCP stay on the Colliedesk address. Every address Colliedesk gives out uses the domain: the link
in Settings, the address of each article in the reply box and in the API, and the page's canonical
link. The server checks the records again every day. If they are gone, the card warns under **Last
check** and says by when they must be back: the help center keeps serving on the domain for 7 days
without them. After 7 days the domain goes back to *waiting for its DNS records*, the card says why,
and the help center's other addresses serve it again. Put the records back and press **Check now**
to serve on the domain again.

- **Another workspace's help center serves on this host name:** you can still set it. It waits until
  your records prove it and the other workspace's no longer do; **Check now** then moves the domain to
  your help center at once. While the other workspace's records still prove it (for example its `TXT`
  record is still there), **Check now** says the name is taken. In the same way, if another workspace
  proves your domain while your records are gone, the domain moves there, and your card says so.
- **Check now** can run 10 times an hour per workspace (people and AI agents together); the server
  asks DNS each time, and opens the address only after DNS proves the domain is yours.
- **Refused names:** an IP address, a single word such as `help`, more than 253 characters, the
  Colliedesk address itself, and names below it. A name with accents, such as `hilfe.bücher.de`, is
  stored in its ASCII form (`hilfe.xn--bcher-kva.de`), which is the form DNS uses.
- **Remove domain** asks first. The help center then stops answering on the domain with the next
  page, and its other addresses serve it again. Setting the same name again needs a new check, with a
  new `CNAME` target and `TXT` token.
- **Who sees the token:** every agent, and every AI agent token including `read`. The token is not a
  password: once you add the `TXT` record, anyone can read it in public DNS. It only proves the domain
  for this domain setting, and it never leaves Colliedesk in an export.

### What visitors see

- Only published articles outside the trash that are for [everyone](#visibility). Drafts, the trash,
  articles for help chat or agents only, and collections with nothing of these in or below them never
  appear: not in a page, a count, a search or an address.
- The home page lists the top-level collections, each with its first articles and the collections
  inside it. A collection page lists those collections, each with its first articles, and its own
  articles in their [order](#order), 50 a page. An article page lists the other articles of its
  collection beside the text, and its headings under [On this page](#on-this-page).
- **Search** works like the search in the editor: whole words and the start of words in titles and
  text, and any part of a title. It shows the best 25 matches.
- Search also finds words in the text of an article's [files](#attach-files) (PDF, TXT, CSV).
- Files listed under an article are offered for download below its text, while a published article lists them.
- Images you added to an article are shown while a published article uses them. An image from
  another site is shown as a link to it: the help center loads nothing from other sites.
- Raw HTML in the text is not shown, except [YouTube and Vimeo videos](#videos), and links work only
  for `http`, `https`, `mailto` and `tel`. A video's site sees the reader's address when the video
  loads, as with any embedded video.
- Every article ends with **Was this article helpful?** and **Yes** / **No**, with the count so far
  ("12 out of 15 found this helpful", votes imported from Freshdesk included). After **No** the reader
  can say **What was missing?** in one line. Both are plain forms that work without script. The words
  are in the article's language (English for a language without them). See [Readers' votes](#votes-from-readers).

Every page carries structured data for search engines (schema.org JSON-LD, which runs nothing): a
breadcrumb trail, and on an article page an `Article` with its title, description and dates. An article
written as a list of questions also carries a `FAQPage`: it has at least two headings of its highest
level, every one of them ends with `?`, and each is followed by its answer before the next one. Text
before the first question is an introduction and is left out.

Pages have no scripts, no cookies and no tracking, with two exceptions: a reader who votes gets one
cookie of the help center's own (`cd_reader`, kept a year, never read by script), so a second vote
changes the first; with the [help chat](help-chat.md) on, article pages and the chat page load the
chat's one script from the help center itself, and the chat keeps one cookie once a customer asks.
Pages follow the visitor's light or dark setting, and
fit a phone. The page labels (Search, Home and so on) are in English; the help center's language
marks your articles' language for browsers and search engines.

With a [brand](#brand):

- The header shows the **logo** in place of the title, at most 40 pixels tall, with the logo's text
  for screen readers. The **favicon** is the browser tab's icon. A logo or favicon in the media
  library's trash is not shown; restoring it brings it back.
- The **website link** is in the header, named by its host (`acme.example`).
- The footer shows the **contact**: the email address of a `mailto:` contact, **Contact us** for an
  `https` one, and the **phone number** as a link that calls it.
- The **colors** and **fonts** replace the default ones. Any color set makes the pages light only,
  whatever the visitor's setting. Links, the hover color and the keyboard focus outline in the
  header and footer switch to the header text (or footer text) color where the link or primary
  color would not read on them.
- Fonts come from the visitor's device, so the page loads nothing more and looks a little different
  on each system.
- **Custom CSS** applies after the colors and fonts. Custom CSS alone keeps the visitor's light or
  dark setting.

### Search engines and AI assistants

Each help center is its own site for search engines, on the address it serves (its host, its custom
domain, or `/help/<slug>` on a server without a help center domain). Everything below names only that
address, never the app's address or another workspace's:

- Every page has a canonical link, `og:url` and an `hreflang` link for the help center's language
  (and `x-default`); a page that exists in other languages links each of them (`hreflang`), and
  `x-default` is the primary language's. Every other address of a page, such as a trailing slash, `?page=1`, a tracking
  query (`?utm_source=…`), an old article slug or the slug host after a custom domain is active,
  answers **301** to the canonical address, so search engines never see the same page twice.
- `robots.txt` (on a host of its own) allows crawlers and names the sitemap.
- `sitemap.xml` lists the home page, every collection that shows something and every published
  article with the date it last changed; with other languages shown, also each of their pages, every
  page with its versions in the other languages.
- `llms.txt` lists the published articles by collection, for AI assistants, each with a link to its
  Markdown and its files.
- Every article's address followed by `.md` (`/a/12-reset-your-password.md`) is the article as
  Markdown, with its images and its files on the help center's address.

During a Colliedesk Cloud **trial**, `robots.txt` asks every crawler to stay away, and `sitemap.xml`
and `llms.txt` list nothing. A paid plan lifts this at once.

#### Search Console and Bing Webmaster Tools

You can verify the help center's address in Google Search Console and Bing Webmaster Tools yourself.
Both tools offer a verification by HTML tag. Copy only the `content` value of the tag:

- Google: the tag is `<meta name="google-site-verification" content="…">`.
- Bing: the tag is `<meta name="msvalidate.01" content="…">`.

Open **Knowledge base → Settings → Search Console**, paste each value into its field and press **Save**.
The help center then puts the tag in the head of every page of this workspace, on its own address
(its host or its custom domain). The app and every other workspace's help center never carry it.
Then press **Verify** in Search Console or Bing.

- Paste the value, not the whole tag. A value that is not 16 to 100 letters, digits, `-` or `_`
  is refused and nothing is saved.
- To remove a token, clear its field and save. A token you do not touch stays as it is.
- Saving changes the public help center, so it needs `help_center:update` and
  `kb_articles:publish`, for a person and an AI agent token alike; without either it is refused.
- The tokens are in the [export](export.md) (`help-center.ndjson`), so a move away keeps them.

### Limits

- One visitor address can load 600 pages, run 30 searches and vote 20 times a minute; past that the
  page asks to wait a minute.
- During a Colliedesk Cloud **trial**, pages ask search engines not to index them. A paid plan lifts
  this.
- A **locked** workspace (trial over, nothing paid) has no help center: its addresses say "This page
  does not exist", like a help center that is off. Paying brings it back at once.
- Browsers may keep a page but check with the server before showing it again, so an article you
  unpublish disappears on the next visit.

## Link an article in a reply

Point a customer to an article from the reply box of a conversation:

1. Select **Articles** under the message box (or move to it with `Tab` and press `Enter`).
2. Before you type, the published articles are listed in the help center's [order](#order),
   collection by collection. Type to search them. The search is the one of the knowledge base page: whole
   words and the start of words in titles and text, and any part of a title. Accents count, so
   `mat` does not find `mật`.
3. Press the arrow keys to move, then `Enter` (or click) to insert. `Esc` closes the list and puts
   you back in the box.

The article goes in where the cursor is, as its title on one line and its
[address](#its-address) on the next, each on a line of its own. Mail goes out as plain text, so
the customer sees exactly these two lines, and their mail app makes the address a link. You can
edit both before you send. `Ctrl+Z` (`Cmd+Z`) takes the link back out.

An article that is not for [everyone](#visibility) has no public page to link. The list marks it
**Help chat only** or **Agents only**, and choosing it inserts its published text instead of a link.

The list shows only published articles, and only while the help center is on: while it is off no
article has a public page, so the list says so instead. An admin gets **Open help center
settings**, which opens Knowledge base → Settings in a new tab, so your draft stays; an agent asks an admin. The list
asks again each time it opens, so an article published or a help center turned on a moment ago
shows up.

An address you sent keeps working when the article's address name changes, but not when the
**workspace address name** changes (see [Its address](#its-address)), when the article is
unpublished or deleted, or while the help center is off.

## Limits

- 2,000 articles per workspace, counting the trash, and 1,000 collections.
- 200 [templates](#templates) per workspace, counting the trash; 100,000 characters per template text.
- 100,000 characters per article text, 300 per title; never cut.
- Images: PNG, JPEG, GIF and WebP, 5 MB each, at most 8,192 pixels on a side and 40 megapixels.
- 10,000 images and videos in the media library per workspace, counting its trash; videos up to
  200 MiB and 8,192 pixels on a side, three uploads in progress at a time.
- Files: 25 MiB each and 100 MiB per article or translation, at most 50 under one; the text read for
  search stops at 200 KiB.
- Collections go three levels deep.
- Search does not find a part in the middle of a word, nor words in text written without spaces.
- 40 languages besides the primary one; one translation per article and language.

## For developers and AI agents

An AI agent connected through [MCP](mcp.md), or the API with an agent token, can do what a person does
in the knowledge base. Each action needs its permission and happens at once; the version names the
token. Without the permission the call is refused and names what is missing
([Roles and permissions](roles-and-permissions.md)).

| To | Needs |
|---|---|
| Read, search, history, trash, templates | `kb_articles:read`, `kb_media:read`, `help_center:read` |
| Write a draft, save or discard unpublished changes, order, restore a version | `kb_articles:create`, `kb_articles:update` |
| Add, edit, delete and restore templates | `kb_articles:update` |
| Delete and restore from the trash | `kb_articles:delete` |
| Anything that changes the public help center (publish, publish changes, unpublish or trash a published article, a published collection) | `kb_articles:publish` as well |
| Send a text for review, withdraw your own review | `kb_articles:update` |
| Approve, request changes, withdraw anyone's review, set whether publishing needs approval | `kb_articles:publish` |
| Upload, name, replace, trash images, videos and files | `kb_media:create`, `kb_media:update`, `kb_media:delete` |
| Set the files listed under an article or translation | `kb_articles:update` (and `kb_articles:publish` for a published translation) |
| Turn the help center on or off, add or order its languages | `help_center:update` |
| Show or hide a language | `help_center:update` and `kb_articles:publish` |
| Set its theme, search console tokens and custom domain | `help_center:update` and `kb_articles:publish` |

Articles and collections:

- Search and read articles with `colliedesk_kb_articles_search` and `colliedesk_kb_article_get`. Agents
  also write, publish, delete and restore articles, manage collections, and upload and read images.
- Order: `colliedesk_kb_order_set` or `PUT /api/v1/kb/order` puts collections and articles in
  [order](#order).
- Several collections: `collection_ids` (the home first) on `colliedesk_kb_article_create` and
  `colliedesk_kb_article_update` lists an article in several collections. Every article answer carries
  `collection_ids`.
- Search title and meta description: `seo_title` and `seo_description` on `POST` and
  `PATCH /api/v1/kb/articles`, and on `colliedesk_kb_article_create` and `colliedesk_kb_article_update`.
- [Who can see it](#visibility): `visibility` (`everyone`, `help_chat` or `agents`) on
  `colliedesk_kb_article_create`, `colliedesk_kb_article_update`, `colliedesk_kb_collection_create` and
  `colliedesk_kb_collection_update`, and on `POST` and `PATCH` of `/api/v1/kb/articles` and
  `/api/v1/kb/collections`. Every answer carries `visibility` and `effective_visibility` (the most
  restrictive of the article and its collections). `visibility=` on `colliedesk_kb_articles_search` and
  `GET /api/v1/kb/articles` keeps the articles with that effective visibility. A raise that would publish
  unreviewed text is refused with `approval_required` and the article ids.
- Keep an article away from AI: `colliedesk_kb_article_ai_exclude` and `colliedesk_kb_article_ai_include`,
  with `kb_articles:update`.
- Unpublished changes: `colliedesk_kb_article_update` (or `PATCH /api/v1/kb/articles/{id}`) on a
  published article saves them, with `kb_articles:update`. `colliedesk_kb_article_get` answers the
  published article with its `draft`; `has_draft` marks it in every list, and `has_draft=true` on
  `GET /api/v1/kb/articles` keeps only those. `GET /api/v1/kb/articles/{id}?version=draft` answers the
  unpublished changes as one article. `colliedesk_kb_article_draft_publish`
  (`POST /api/v1/kb/articles/{id}/draft/publish`, with `kb_articles:publish`) puts them live and
  `colliedesk_kb_article_draft_discard` (`DELETE /api/v1/kb/articles/{id}/draft`) drops them. Both take
  the article's `updated_at` as you read it: saving the unpublished changes moves it, so changes saved
  after you read them are never published or dropped unseen.
- Autosave: `PATCH /api/v1/kb/articles/{id}` with `autosave: true` takes only `title` and `body_md` and
  adds no version. On a draft article it changes the article; on a published one it adds to the
  unpublished changes. Every `PATCH` sends the `updated_at` it started from; a newer copy on the server
  refuses it with 409 `changed_elsewhere`. An agent saves with `colliedesk_kb_article_update` as before,
  which adds a version.
- [Readers' votes](#votes-from-readers): `colliedesk_kb_article_votes_get` or
  `GET /api/v1/kb/articles/{id}/votes` reads the count, each vote with its line, and the earlier counts
  (`kb_articles:read`). `colliedesk_kb_article_votes_reset` or `POST /api/v1/kb/articles/{id}/votes/reset`
  resets them (`kb_articles:update`); a repeat of the same press, or a reset with nothing counted, stores
  no second reset. The reset names the token that made it.
- Link an article in a reply: every article an agent reads or finds carries its public address in `url`
  (also in the API). An agent links an article the same way a person does: the title and the address on
  two lines, from a search with `status` published while the help center is on.

[Review before publishing](#review-before-publishing):

- `colliedesk_kb_article_send_for_review` (`POST /api/v1/kb/articles/{id}/reviews`, or
  `/translations/{locale}/reviews` for a translation) sends the text to `reviewer_ids` with a `note`; it can
  save the title, text and search fields with the article's `updated_at` in the same call. Reviewers come
  from `colliedesk_agents_list` (`GET /api/v1/agents`): those who may publish.
- `colliedesk_kb_reviews_list` (`GET /api/v1/kb/reviews`, `state=open&reviewer=me` for what waits for
  you; or per article and translation) and `colliedesk_kb_review_get` (`GET /api/v1/kb/reviews/{id}`, with
  the live text and the line diff) read them with `kb_articles:read`.
- `colliedesk_kb_review_approve` (`POST /api/v1/kb/reviews/{id}/approve`, or `POST /api/v1/kb/reviews/approve`
  for several) and `colliedesk_kb_review_request_changes` (`POST /api/v1/kb/reviews/{id}/request-changes`,
  a `note` is required) need `kb_articles:publish`. `colliedesk_kb_review_withdraw`
  (`POST /api/v1/kb/reviews/{id}/withdraw`) needs `kb_articles:update`.
- `colliedesk_kb_articles_publish` (`POST /api/v1/kb/articles/publish`) publishes up to 50 articles and
  answers each one on its own. `colliedesk_kb_articles_search` takes `state` `in_review` or `approved`.
- `require_approval` on `colliedesk_help_center_update` (`PUT /api/v1/help-center`, true or false) and on
  `colliedesk_kb_collection_update` (`inherit`, `on` or `off`) needs `kb_articles:publish`.
- A review id of another workspace is 404.

[Templates](#templates):

- `colliedesk_kb_templates_list` (`GET /api/v1/kb/templates`; `trash` true, or
  `GET /api/v1/kb/templates/trash`, for the trash) and `colliedesk_kb_template_get` read them with
  `kb_articles:read`. `colliedesk_kb_template_create`, `_update`, `_delete`, `_undelete`, `_history` and
  `_version_restore` (`POST`, `PATCH` and `DELETE /api/v1/kb/templates/{id}`, `/{id}/restore`,
  `/{id}/versions`, `/{id}/versions/{version}/restore`) need `kb_articles:update`.
- `template_id` on `colliedesk_kb_article_create` (or `POST /api/v1/kb/articles`) starts an article
  from a template instead of `body` (`body_md` in the API); with neither, it starts from its collection's default template.
  A template in the trash is refused with 409 `template_in_trash`.
- `default_template_id` on `colliedesk_kb_collection_create` and `colliedesk_kb_collection_update` (and
  their routes) sets a collection's default template; null clears it.
- Writing an article from a template takes two calls: list the templates, create the article.

Images and videos:

- An image in the text is `![name](/api/v1/kb/images/…)`. A video of the media library is
  `<video controls preload="metadata" src="/api/v1/kb/media/<id>"></video>`.
- An image at a public web address: `colliedesk_kb_image_import` (`POST /api/v1/kb/images/import`, with
  `kb_media:create`) downloads it into the media library and answers its link. Private, loopback and
  internal addresses are refused with 403 `address_refused`.
- Media library: `colliedesk_kb_media_list` and `colliedesk_kb_media_get` show which images no article
  uses and which have no alt text. `colliedesk_kb_media_update` names, describes or keeps them.
  `colliedesk_kb_media_trash` and `colliedesk_kb_media_undelete` move them to the trash or back, and
  `colliedesk_kb_media_replace` replaces one in every article, with the `kb_media:*` permissions above.
  Deleting forever (`colliedesk_kb_media_purge`) needs `kb_media:purge`.
- Files: `colliedesk_kb_file_upload` stores a file of up to 5 MiB; a larger one (up to 25 MiB) goes up
  in parts with `colliedesk_kb_video_upload_start` and `kind` `file` (`POST /api/v1/kb/media-uploads`
  with `kind=file`). `colliedesk_kb_article_attachments_set` (`PUT /api/v1/kb/articles/{id}/attachments`,
  or `…/translations/{locale}/attachments` with `locale`) sets the list in order; `[]` empties it.
  `colliedesk_kb_media_list` with `kind` `file` lists files with the articles that list them, and
  `colliedesk_kb_media_replace` replaces one in place with a file no article lists yet.
- Videos: an agent uploads a video in parts (`colliedesk_kb_video_upload_start`, `_part`, `_complete`)
  and sets its poster (`colliedesk_kb_video_poster_set`) with `kb_media:create` and `kb_media:update`.

Translations:

- An agent translates with the same permissions as articles. `colliedesk_help_center_languages_get`
  names the languages and what is missing or outdated in each. `colliedesk_kb_translations_list` lists
  those articles. `colliedesk_kb_translation_get` reads a translation and its original in one call.
- `colliedesk_kb_translation_create` and `colliedesk_kb_translation_update` write one (`outdated` false
  marks it up to date in the same call). `_delete`, `_undelete`, `_history`, `_version_restore` and
  `colliedesk_kb_collection_translation_set` / `_delete` do the rest.
- `colliedesk_help_center_languages_set` adds, orders, shows and hides languages (`help_center:update`,
  and `kb_articles:publish` to show or hide one).
- Translating one missing article takes three calls: list, read the article, create.

Help center:

- Its address: `GET /api/v1/help-center` and `colliedesk_help_center_get` give it in `url`.
- Brand: `PUT /api/v1/help-center/theme` or `colliedesk_help_center_theme_update` saves it;
  `colliedesk_help_center_theme_get` reads it. A field left out of a save goes back to its default, so
  send the whole brand. Every save changes `content_version`.
- Fonts: besides `system-sans` and `classic-serif`, the API takes `humanist-sans`, `geometric-sans`,
  `transitional-serif`, `rounded-sans` and `mono`.
- Contrast check: `POST /api/v1/help-center/theme/check` (`colliedesk_help_center_theme_check`) runs the
  brand's checks without saving.
- Preview: `POST /api/v1/help-center/theme/preview` gives the preview page as one HTML document.
  `colliedesk_help_center_theme_check` with `preview` (`home` or `article`) gives it to an AI agent.
- Custom CSS: a brand save must send `custom_css` (null or `""` for none). A save without it is refused
  with 422 and changes nothing, so a client that does not know the field cannot remove your CSS by
  accident.
- Search Console: `GET` and `PUT /api/v1/help-center/search-console`; AI agents use
  `colliedesk_help_center_search_console_get` and `colliedesk_help_center_search_console_update`.
- Custom domain: `colliedesk_help_center_domain_get` reads it. A token with `help_center:update` and
  `kb_articles:publish` also sets, checks and removes it (`colliedesk_help_center_domain_set`, `_check`,
  `_remove`); without either it is refused.
