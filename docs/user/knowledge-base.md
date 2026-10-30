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
     and adds bold, italic, a link, numbered and bulleted lists, a quote and a table; **Insert image or
     video** adds an [image](#images) or a [video](#videos); **⋯** (More formatting) holds
     strikethrough, code, a checklist, a code block, a divider and redo. On a phone the toolbar keeps **Insert**, bold,
     italic and link, and the rest is under **⋯**. With the keyboard, Tab reaches the toolbar and the
     arrow keys move along it.
   - Shortcuts: `Ctrl+B` bold, `Ctrl+I` italic, `Ctrl+E` code, `Ctrl+K` link, `Ctrl+Z` undo,
     `Ctrl+F` find and replace (`Cmd` on a Mac).
3. Choose the **Collection** (under **Placement** on the right; on a phone, under **Details**).
4. Select **Save draft** to keep it for agents only, or **Save and publish**.

Every action on an article is in the bar at the top: **Save**, **Publish** for a draft, and **⋯** with
**View on help center**, **Copy link**, **Version history**, **Publish** or **Unpublish**, and
**Delete article…**. The column on the right only shows the article's **Visibility** (with **Turn on
help center** while it is off), the images that still need alt text, and its **Placement**
(collection and address name). On a phone the bar is at the bottom of the screen: the status,
**Details** (visibility, alt text, collection, address), **⋯** and **Save**.

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

Paste an image into the text, drag it in, or select **Insert image or video → Upload from this
device**. Colliedesk uploads it and writes
`![name](/api/v1/kb/images/…)` on a line of its own after the paragraph, list or table you are in;
while it uploads, a box says so and nothing is added to the text. A video pasted, dropped or chosen
the same way goes up in parts, the box shows how much has gone, and it goes in as
`<video controls preload="metadata" src="/api/v1/kb/media/…"></video>`; leaving the page cancels a
video still uploading. **Insert image or video → From the media library** opens the library in a
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

**Knowledge → Media** shows every image and video of the workspace, newest first, with a search by
name or alt text, a choice of images or videos, of what is in use, not used or missing alt text
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

An article can show a YouTube or Vimeo video. Select **Insert image or video → YouTube or Vimeo
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
library](#media-library) or from the editor (above). **Preview** shows a video with its poster, as
the help center does, and leaves out (and names) one the library does not have.

### Publish, unpublish, edit

An article shows **Draft** or **Published**. **Publish** puts a draft on the help center and
**Unpublish** (in **⋯**) takes it back off; both are versions, so neither is final. **Save** keeps an
article's status. **⋯ → Delete article…** asks first, then moves the article to the [trash](#trash).

Each article has a number that never changes. Its help center address ends in
`/a/<number>-<address name>`, and the article page shows it under **Address name**. You can change the
address name; links with the old one still lead to the article because the number does not change.

### Search title and meta description

Under **Search engines** on the article page, **Search title** (up to 120 characters) is the title
Google and other search engines show, and **Meta description** (up to 320) the text under it. Left
empty, the article's title (followed by the help center's title) and the first 160 characters of its
text are used. Line breaks and repeated spaces become one space. Both are part of the article's
history and of the [export](export.md), and are set through the API (`seo_title`,
`seo_description` on `POST` and `PATCH /api/v1/kb/articles`) and by AI agents
(`colliedesk_kb_article_create`, `colliedesk_kb_article_update`). Changing them on a published
article changes the help center, so a token that may edit but not publish files a request a person
approves.

If a teammate (or an AI agent) saved the article after you opened it, your save is refused and
nothing is overwritten. Your text stays on screen and you choose: **Save mine over theirs** (theirs
stays in the history) or **Discard mine and show theirs**.

Leaving the page with unsaved changes asks first.

## History

**⋯ → Version history** on an article lists every version, newest first: who made it (and through which AI
agent token or approved request), when, and what changed (title, address name, text, status,
collection). **Restore version N** makes that version's title, address name and text current again,
as a new version; the status and collection stay as they are, so a restore never publishes or moves
an article by surprise. Save or undo your own changes first.

## Keep an article away from AI

AI features (reply drafts and triage suggestions, once an admin turns them on) find the published
articles that fit a conversation by themselves, in every language an article has, and quote them;
nobody has to sort the knowledge base for AI. A draft, an article in the trash and an internal note
of a conversation are never read.

To keep one published article away from AI, open it and select **Exclude from AI** under **Used by
AI**. From then on drafts and suggestions skip it in every language; people and the help center
still see it. The article says since when and by whom, and its history has the change. **Let AI use
it again** undoes it. Neither changes the text, so an edit someone has open still saves. An agent
token can do the same (`colliedesk_kb_article_ai_exclude`, `colliedesk_kb_article_ai_include`, tier
`send`).

## Trash

Select **Trash** at the top of the articles to see deleted articles and collections. Each shows who deleted it and how many days
are left; after 30 days it is deleted for good, with its history. **Restore** brings it back as it
was: a published article is published again. Restore a collection before the articles that were in
it. Articles in the trash count toward the 2,000 articles a workspace can hold.

## Help center settings

Open **Settings → Help center**. Every agent sees whether the help center is on. An admin can turn
it on or off with **Publish the help center** and set its **Title** (empty uses the workspace name).
Turning it off takes every page offline at once; turning it on again publishes the articles marked
published.

### Brand

An admin sets the help center's brand in **Settings → Help center → Brand**, through the API
(`PUT /api/v1/help-center/theme`) or an AI agent (`colliedesk_help_center_theme_update`). Every page
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
  heading font). The API also takes `humanist-sans`, `geometric-sans`, `transitional-serif`,
  `rounded-sans` and `mono`; a font saved that way stays and shows in the form until you choose another.
- **Custom CSS** (`custom_css`), for details the fields above do not cover, such as
  `.masthead{border-bottom:2px solid #0b5cad}`. See [custom CSS](#custom-css).

Every color pair must be readable (WCAG AA): 4.5:1 for text, links, header text, footer text and
the button text on the primary color (white or black, whichever reads better), and 3:1 for the
primary color against the background. A brand that fails is not saved; the answer names each color
with its ratio and the nearest color that passes, for example "Text on the background is 2.85:1; it
needs 4.5:1. #767676 passes." `POST /api/v1/help-center/theme/check` (`colliedesk_help_center_theme_check`)
runs the same checks without saving. A field left out of a save goes back to its default, so send
the whole brand. Every save changes `content_version`.

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
no script; custom CSS cannot change that. `POST /api/v1/help-center/theme/preview` gives the same page as one HTML
document, and `colliedesk_help_center_theme_check` with `preview` (`home` or `article`) gives it to an
AI agent. Each person may ask for 600 previews a minute.

#### Custom CSS

Custom CSS is added after the brand's colors and fonts on every public page and in the preview, so
its rules win over them. It is saved and served exactly as written, at most 32,768 bytes of UTF-8 (a
letter outside ASCII takes 2 to 4 bytes; the field counts them as you type). An empty field (or only
spaces) removes it. A save through the API must send `custom_css` (null or `""` for none): a save
without it is refused with 422 and changes nothing, so a page or an agent that does not know the field
cannot remove your CSS by accident. Pages keep the same security policy: the CSS comes from the help
center's own address, in the brand's style sheet, never inline in a page. A custom agent token needs
both `help_center:update` and `kb_articles:publish` to save the theme; without either it files a
request for a person to approve, and the public pages stay unchanged.

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
in **Settings → Help center** (see [Help center settings](#help-center-settings)).

### Its address

**Settings → Help center** shows the address as a link, even while the help center is off; the API
(`GET /api/v1/help-center`) and `colliedesk_help_center_get` give it in `url`.

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
address name** (Settings → Email) changes every help center address, and old links stop working.

### Custom domain

The help center can answer on an address of your own domain, such as `help.example.com`. Open
**Settings → Help center → Custom domain**. Every agent sees the domain and its state; an admin sets,
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

**Moving from Freshdesk: verify by TXT first, then change the CNAME.** Add only the `TXT` record and
press **Check now**. The domain is then verified while your visitors still reach Freshdesk. Change the
`CNAME` afterwards, to the target the card shows. The domain is already verified, so the first visit
after the switch gets its certificate at once and does not wait for a DNS check. If you change the
`CNAME` first, the first visitor makes the server check DNS and ask for the certificate, which takes
longer. The old `*.freshdesk.com` addresses are covered in
[Import from Freshdesk](import-freshdesk.md#old-freshdesk-com-addresses).

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
  asks DNS each time.
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

- Only published articles outside the trash. Drafts, the trash, and collections with nothing
  published in or below them never appear: not in a page, a count, a search or an address.
- The home page lists the top-level collections, each with its first articles and the collections
  inside it. A collection page lists those collections, each with its first articles, and its own
  articles in their [order](#order), 50 a page. An article page lists the other articles of its
  collection beside the text.
- **Search** works like the search in the editor: whole words and the start of words in titles and
  text, and any part of a title. It shows the best 25 matches.
- Images you added to an article are shown while a published article uses them. An image from
  another site is shown as a link to it: the help center loads nothing from other sites.
- Raw HTML in the text is not shown, except [YouTube and Vimeo videos](#videos), and links work only
  for `http`, `https`, `mailto` and `tel`. A video's site sees the reader's address when the video
  loads, as with any embedded video.

Every page carries structured data for search engines (schema.org JSON-LD, which runs nothing): a
breadcrumb trail, and on an article page an `Article` with its title, description and dates. An article
written as a list of questions also carries a `FAQPage`: it has at least two headings of its highest
level, every one of them ends with `?`, and each is followed by its answer before the next one. Text
before the first question is an introduction and is left out.

Pages have no scripts, no cookies and no tracking (with the [help chat](help-chat.md) on, article pages
and the chat page load the chat's one script from the help center itself, and the chat keeps one cookie
once a customer asks), follow the visitor's light or dark setting, and
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
  (and `x-default`). Every other address of a page, such as a trailing slash, `?page=1`, a tracking
  query (`?utm_source=…`), an old article slug or the slug host after a custom domain is active,
  answers **301** to the canonical address, so search engines never see the same page twice.
- `robots.txt` (on a host of its own) allows crawlers and names the sitemap.
- `sitemap.xml` lists the home page, every collection that shows something and every published
  article with the date it last changed.
- `llms.txt` lists the published articles by collection, for AI assistants, each with a link to its
  Markdown.
- Every article's address followed by `.md` (`/a/12-reset-your-password.md`) is the article as
  Markdown, with its images on the help center's address.

During a Colliedesk Cloud **trial**, `robots.txt` asks every crawler to stay away, and `sitemap.xml`
and `llms.txt` list nothing. A paid plan lifts this at once.

#### Search Console and Bing Webmaster Tools

You can verify the help center's address in Google Search Console and Bing Webmaster Tools yourself.
Both tools offer a verification by HTML tag. Copy only the `content` value of the tag:

- Google: the tag is `<meta name="google-site-verification" content="…">`.
- Bing: the tag is `<meta name="msvalidate.01" content="…">`.

Open **Settings → Help center → Search Console**, paste each value into its field and press **Save**.
The help center then puts the tag in the head of every page of this workspace, on its own address
(its host or its custom domain). The app and every other workspace's help center never carry it.
Then press **Verify** in Search Console or Bing.

- Paste the value, not the whole tag. A value that is not 16 to 100 letters, digits, `-` or `_`
  is refused and nothing is saved.
- To remove a token, clear its field and save. A token you do not touch stays as it is.
- Saving changes the public help center, so it needs the same permission as publishing: an admin
  saves it at once, and an AI agent token needs `help_center:update` and `kb_articles:publish`
  (without them, it files a request a person approves). The API is `GET` and `PUT
  /api/v1/help-center/search-console`; AI agents use `colliedesk_help_center_search_console_get` and
  `colliedesk_help_center_search_console_update`.
- The tokens are in the [export](export.md) (`help-center.ndjson`), so a move away keeps them.

### Limits

- One visitor address can load 600 pages and run 30 searches a minute; past that the page asks to
  wait a minute.
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

The list shows only published articles, and only while the help center is on: while it is off no
article has a public page, so the list says so instead. An admin gets **Open help center
settings**, which opens Settings in a new tab, so your draft stays; an agent asks an admin. The list
asks again each time it opens, so an article published or a help center turned on a moment ago
shows up.

An address you sent keeps working when the article's address name changes, but not when the
**workspace address name** changes (see [Its address](#its-address)), when the article is
unpublished or deleted, or while the help center is off.

## AI agents

An AI agent connected through [MCP](mcp.md), or the API with an agent token, can search and read
articles (`colliedesk_kb_articles_search`, `colliedesk_kb_article_get`), write, publish, delete and
restore them, manage collections, put collections and articles in [order](#order)
(`colliedesk_kb_order_set`, `PUT /api/v1/kb/order`), upload and read images, and read the help center settings and
brand (`colliedesk_help_center_theme_get`, and `colliedesk_help_center_theme_check` to try one) and
its custom domain (`colliedesk_help_center_domain_get`). An admin token also sets, checks and removes
the custom domain (`colliedesk_help_center_domain_set`, `_check`, `_remove`). A custom token needs
both `help_center:update` and `kb_articles:publish` for these changes; without either it files an
[agent request](agent-requests.md) for a person to approve.
Every article an agent reads or finds carries its public address in `url` (also in the API), so an
agent links an article in a reply the same way a person does: the title and the address on two
lines, from a search with `status` published while the help center is on. In
the media library (`colliedesk_kb_media_list`, `colliedesk_kb_media_get`) an agent sees which images
no article uses and which have no alt text, and can name, describe or keep them
(`colliedesk_kb_media_update`), move them to the trash or back (`colliedesk_kb_media_trash`,
`colliedesk_kb_media_undelete`) and replace one in every article (`colliedesk_kb_media_replace`), with
the same tiers as writing an article. A request from a lower tier always names the images it would
move. Deleting forever (`colliedesk_kb_media_purge`) is an admin's. An agent with a `send` token uploads
a video in parts (`colliedesk_kb_video_upload_start`, `_part`, `_complete`) and sets its poster
(`colliedesk_kb_video_poster_set`); a lower tier cannot upload files.

| Token tier | Read, search, history, trash | Write, publish, delete, restore, order, upload images | Turn the help center on or off, set its brand and custom domain |
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
