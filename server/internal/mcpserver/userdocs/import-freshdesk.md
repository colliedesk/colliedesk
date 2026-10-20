# Import from Freshdesk

`collie import freshdesk` copies a Freshdesk account into a Colliedesk workspace through the
Freshdesk API. It ends with a reconcile report that compares what Freshdesk holds with what
Colliedesk now holds. If anything is missing, the report says **FAILED** and lists the Freshdesk
ids. If the totals could not be confirmed, it says **UNVERIFIED**. Both end with a non-zero exit
code. Run the same command again to retry: finished items are never copied twice.

## What is imported

| Freshdesk | Colliedesk |
|---|---|
| Contacts | Contacts (matched by email when the contact already exists) |
| Companies | The contact's **organization** |
| Agents | Matched by email to your Colliedesk agents; otherwise the author name is kept |
| Tickets, including archived tickets | Conversations with the original subject, status, priority, assignee, tags and timestamps |
| Ticket description, replies, incoming replies, private and public notes, forwards | Timeline messages with the original author, time, from/to/cc/bcc and Freshdesk kind. A forward to a third party and a public note become notes, never replies to the customer |
| Automatic replies Freshdesk flagged (an out-of-office, a messenger or bot acknowledgement) | Shown as one line, "<sender> sent an automated response", with a chevron that opens the message. Tickets imported before this was added keep those replies as ordinary messages; import them again to fold them |
| Ticket type, channel, due dates, cc and forwarded-to addresses | Shown in the conversation details |
| Original message HTML | Shown formatted on the message in a locked frame that runs no scripts; **Show plain text** switches to the text. Remote images load through the Colliedesk image proxy |
| Attachments | Files stored by Colliedesk, each with a SHA-256 checksum |
| Images inside a message that Freshdesk hosts | Stored by Colliedesk with a SHA-256 checksum, embedded in the formatted message, and also listed as files in the plain-text view. See "Images inside messages" below |
| Time entries | Time entries (agent, minutes, note, billable, time), shown under **Time logged** on the timeline |
| Canned responses | [Saved replies](saved-replies.md), titled `<folder name> / <title>` so the folder is still visible. See below |
| Knowledge base (solutions): categories, folders, articles and their translations | Knowledge base collections and articles with the original dates and author. See "Knowledge base" below |

Status mapping: Open → Open, Pending → Pending, Resolved → Resolved, Closed → Closed. Custom
statuses become Pending and are listed in the report. Priority: Low → Low, Medium → Normal, High → High, Urgent → Urgent. An unknown priority becomes
Normal and is listed in the report.

The report lists what is **not** imported, for example custom fields.

### Canned responses

Each canned response becomes a shared saved reply. The text is the plain-text version Freshdesk
provides; when there is none, the HTML version is reduced to text. Placeholders are changed where
Colliedesk has the same meaning:

| Freshdesk | Colliedesk |
|---|---|
| `{{ticket.requester.name}}`, `{{ticket.from_name}}` | `{{customer.name}}` |
| `{{ticket.agent.name}}` | `{{agent.name}}` |

Every other placeholder, for example `{{ticket.url}}`, stays in the text as written and is listed in
the report under **Freshdesk placeholders left as written**. Edit those saved replies before you use
them.

The report also lists, by Freshdesk id, what does not come over:

- **Files attached to a canned response.** Saved replies hold text only. Download the files from
  Freshdesk if you still need them.
- **A canned response with no text.** There is nothing to import.
- **A personal folder.** Saved replies are shared by the whole workspace, so responses in a personal
  folder are **not imported** by default. The report lists their Freshdesk ids under **Canned
  responses in personal folders, not imported**. To share them, run the import again with
  `--include-personal-canned-responses`.
- **An image in the HTML** of a canned response. Saved replies hold text only, so the image is dropped
  and the report warns (`saved_reply_images_dropped`). Links are kept as `text (address)`.

A canned response whose title is already taken by another saved reply is imported as
`<title> (Freshdesk <id>)`. An existing reply is never changed. If the workspace would pass 500 saved
replies, or a text is longer than 20 000 characters, that item is listed under **Failures** and the
status is `FAILED`. If Freshdesk fails to list the folders (not a refusal, for example server errors),
the tickets still import; the report shows a failure for the canned responses and the status is
`FAILED` until you run the import again. Running the import again never adds a canned response twice and never overwrites
a saved reply your team edited or deleted. If your Freshdesk plan has no canned responses (Freshdesk
refuses them), the report says so and this does not change the status.

### Knowledge base

The whole knowledge base is imported on every run, whatever `--since` says:

| Freshdesk | Colliedesk |
|---|---|
| Category | Top-level collection |
| Folder, subfolder | Collection inside it, then a collection inside that one |
| A folder nested deeper | A third-level collection named after its folders, for example `Advanced / Deep`, listed under the warning `kb_folder_flattened` |
| Article | Article in Markdown with its title, original created and updated dates, and the author when a Colliedesk agent has the same email |
| Article images hosted by Freshdesk | Stored by Colliedesk; the article points at the stored copy |
| Translations | Kept with the article and in the [export](export.md); not shown in Colliedesk yet |

An article is **published** only when it is published in Freshdesk, every folder above it is
visible to **All Users**, and its category is in your help center's portal. Everything else is
imported as a **draft**, so an internal article never becomes public by mistake. The report lists the
drafts by reason; publish the ones that should be public. If your Freshdesk account has several
portals, choose the one whose articles stay published with `--help-center-portal <id>` (the portal ids
are in the report under `knowledge_base.portals`); without it, all articles of those categories are
drafts.

Markdown cannot hold everything HTML can. Embedded video, iframes, forms and scripts are left out of
the article, and a table cell that spans several columns is repeated in each. The report names the
articles under the warning `kb_article_markup_dropped`. The original HTML stays with the article and
is in the export.

An image Freshdesk no longer has stays a link and is listed. An image that fails to download for
another reason makes the status `FAILED`; run the import again.

The import refuses, before it writes anything, a knowledge base that does not fit: more than 2,000
articles in the workspace (articles in the trash count) or 1,000 collections, or an article longer than
100,000 characters once converted. A text is never cut. `--dry-run` lists what does not fit.

Running the import again updates an article that changed in Freshdesk, unless your team changed it in
Colliedesk since: then your version stays and the report lists the article under
`kb_article_changed_in_both`. `import undo` removes the imported articles and collections, but keeps an
article your team edited, restored or deleted since, and the collections that hold it or your own
articles. If Freshdesk has no knowledge base, the report says so. If Freshdesk refuses it (the API key
may lack the right), the status is `UNVERIFIED`.

## Before you start

1. Create the Colliedesk workspace (on your own server, see
   [self-host](../self-host/README.md)).
2. Invite your team first. Replies by an agent link to that agent only when a Colliedesk agent
   with the same email exists.
3. Get a Freshdesk API key from an **admin** account with access to all tickets. The import reads
   the account id (View Account), which needs an admin key:
   1. Sign in to Freshdesk.
   2. Select your profile picture, then **Profile settings**.
   3. Copy the key under **Your API Key**.

The key gives read access to the whole account. Keep it in an environment variable. Colliedesk
never accepts it as a command-line argument and never prints it.

## Run the import

1. Put the key in an environment variable:
   ```sh
   read -rs FRESHDESK_API_KEY && export FRESHDESK_API_KEY
   ```
2. Preview the import. A dry run counts contacts, companies, agents and tickets and writes nothing:
   ```sh
   docker compose exec -e FRESHDESK_API_KEY server /app/collie import freshdesk \
     --workspace <slug> --domain <account>.freshdesk.com --dry-run --report-dir /data/import-reports
   ```
3. Run the import:
   ```sh
   docker compose exec -e FRESHDESK_API_KEY server /app/collie import freshdesk \
     --workspace <slug> --domain <account>.freshdesk.com --report-dir /data/import-reports
   ```
4. Copy the reports out of the container:
   ```sh
   docker compose cp server:/data/import-reports ./import-reports
   ```

Options:

- `--api-key-env NAME`: read the key from another variable. Default `FRESHDESK_API_KEY`.
- `--since YYYY-MM-DD`: import only tickets updated on or after this UTC date. Use it for a final
  catch-up import just before you switch.
- `--dry-run`: count only.
- `--last-ticket-id N`: the highest ticket number you see in Freshdesk. Recommended: the ticket
  check then covers at least this far (see "How completeness is checked").
- `--help-center-portal ID`: on an account with several portals, the portal whose published
  knowledge base articles stay published (see "Knowledge base").
- `--report-dir DIR`: where to write the report. Default: the current directory.

The import respects Freshdesk's rate limit. It waits when Freshdesk says the limit is used up,
so a large account can take hours. You can stop it at any time with Ctrl+C.

While it runs, the command prints progress lines to stderr, for example
`progress: importing tickets 120/400, elapsed 4m10s`: one line when each step starts, one line at
most every 30 seconds with the counts, and one line for each rate-limit wait. The summary on
stdout is unchanged. The report states how many pauses, HTTP 429 responses and seconds of waiting
the rate limit cost.

## Resume or retry

Run the same command again. Items that were imported are skipped, an interrupted ticket continues
where it stopped, and failed items are tried again. Nothing is duplicated. A file that is lost or
damaged on disk after the import is detected by the next run and downloaded again.

On Colliedesk Cloud, the first import into a workspace that is still in its trial extends the trial
to 30 days in total from when it started.

On a Colliedesk Cloud workspace on a paid plan (Solo, Small, Founding, Team or Team 25), imported attachments
count toward the storage limit of the plan (10, 30, 20, 100 or 250 GiB). Before it writes anything, the import adds up the size of the attachments and the message images it would bring in
(it reads each image once to know its size) and compares it with the room left. If they do not fit, it stops with a message that gives the bytes
needed, the bytes free and the limit, and nothing is imported: no contact, ticket or file. Switch to a larger plan under
**Billing > Manage billing** (Solo to Small, Small or Founding to Team; on Team or Team 25, contact us
for more storage) and run it again, or import a smaller slice with
`--since`.

The check is the only place the limit applies to an import. An import that passes it is admitted and
runs to the end: customer email that arrives meanwhile, or a file added in Freshdesk after the check,
can take usage past the limit, and the import is not stopped for that. The overshoot is whatever
arrives while the import runs. Tickets created in Freshdesk during the check are imported without
being counted beforehand, so on a busy account it can be more than a few files. That is accepted.
There is no partial import because of the storage limit. A file counts each time it is attached, so the same file on two
messages counts twice.

To do this the import has to know the sizes first. Freshdesk's ticket list does not carry them, so
before it writes anything the import lists the tickets and probes the ticket numbers (the same calls as
the real run, and as `--dry-run`), then reads every ticket and its conversations (conversations come
in pages, so that is at least two calls per ticket). The real run repeats all of it. Expect about
twice the API calls and time of an import without the check, once, on a paid workspace only. A
ticket that cannot be read is tried three times (each try retries the usual way); if it still fails,
the import stops before writing anything and names the ticket. Run the same command again.

`--dry-run` on a paid workspace tells you in advance: the report says whether the import
**would be refused**, with the bytes it needs and the bytes free. `import undo` removes the imported
files and gives the space back.

Only one import or undo runs in a workspace at a time. A second one stops at once with
"already running".

## How completeness is checked

Freshdesk's ticket list leaves out archived, trashed and spam tickets. Colliedesk therefore checks
every ticket number from 1 one by one, and keeps checking past the highest number it knows until
200 numbers in a row do not exist (or at least up to `--last-ticket-id`). It also compares its
counts with the totals Freshdesk reports for tickets and contacts. The report says how each total
was verified. If Freshdesk refuses the archive (some plans) or a total cannot be read, the report
is **UNVERIFIED**: check the archive in Freshdesk before you close the account. When the archive
is refused, ticket numbers below the highest existing ticket that Freshdesk does not return are
printed at the end of the command output and listed in the report under **Ticket ids that could
not be resolved**. Each may be a deleted or an archived ticket: look them up in Freshdesk.

## Images inside messages

Some messages show an image in their body (a pasted screenshot, a signature logo) instead of a
file attached to the message. Freshdesk hosts those images, and they disappear with the account.
The import downloads each image that Freshdesk hosts: from your Freshdesk address (for example
`yourcompany.freshdesk.com`), from Freshdesk's attachment servers and from Freshdesk's public image
storage. It stores the file like an attachment and embeds it in the formatted message, so the original
message keeps showing it after you close Freshdesk. The Markdown body of the message is unchanged.
Only PNG, JPEG, GIF and WebP images are copied.

Images from other sites, such as a newsletter banner, are not copied. They stay links, and the
locked frame does not load them. An image whose address names Freshdesk but sits on another host
(for example a custom support domain) is not copied either. The report lists its ticket under the
warning `inline_images_not_rehosted`: save those images in Freshdesk before you close the account.

Each message embeds at most 5 MiB of images. A further image is still stored as a file on the
message, but the formatted message keeps its Freshdesk link, and the report lists the ticket under the
warning `inline_image_not_embedded`.

An image counts as one item in the report (**Inline images**). Two things can stop an image:

- **Freshdesk no longer has it** (the file was deleted: HTTP 404 or 410). Nothing can bring it
  back, so this does not fail the import. The report lists it under **Images already gone in
  Freshdesk (not recoverable)** with the ticket id and the message, and the message keeps its
  original link, which no longer works. A run does not ask for it again while the ticket is
  unchanged. If the ticket changes in Freshdesk, the next run checks the image once more.
- **Anything else**: a temporary error, an expired link, a file that is not a PNG, JPEG, GIF or
  WebP image (an SVG is refused), a redirect to another host, or an image over 5 MB. The status is
  `FAILED`. The **Failures** table lists the image with its ticket id and the reason, and the
  message keeps its original Freshdesk link. Run the same command again: a temporary problem is
  retried. If it still fails, open the ticket in Freshdesk and save the image yourself before you
  close the account.

## Undo an import

An import into the wrong workspace can be removed:

1. Preview what would be removed:
   ```sh
   docker compose exec server /app/collie import undo --workspace <slug> --dry-run
   ```
2. Remove it:
   ```sh
   docker compose exec server /app/collie import undo --workspace <slug>
   ```

Undo removes the conversations the import created, with their messages, attachments, inline
images and time entries, the saved replies it created that nobody edited since, and the contacts and tags it created
that nothing else uses. A conversation where your
team wrote a message after the import is kept, and so is a saved reply your team edited (the command
lists its Freshdesk id). Contacts and tags that existed before the import
are kept. Changes without a new message (status, assignee or tags changed in Colliedesk) are
removed together with their conversation. Run the import again afterwards if you want the data back.

## Read the report

Each run writes two files: `freshdesk-import-<workspace>-<time>.md` for people and `.json` for
tools.

- **Status**: `OK` means every count matches and the totals were verified. `FAILED` means
  something is missing; the **Failures** table lists each item with its Freshdesk id and ticket id.
  `UNVERIFIED` means nothing failed but completeness could not be proven (see **How the totals were
  verified**). `DRY_RUN` is a preview.
- **Counts**: contacts, tickets, messages, attachments, inline images, time entries and saved replies in
  Freshdesk and in Colliedesk. The saved reply total is checked against the sum Freshdesk reports for its folders.
- **Attachment bytes**: total size reported by Freshdesk and total size stored. Every stored file
  is read again and its SHA-256 compared with the value recorded at download. **Inline image
  bytes** are checked the same way. Freshdesk reports no size for them, so only the stored total is given.
- **Ticket ids that could not be resolved (archive unavailable)**: ticket numbers Freshdesk does
  not return while the archive is refused. Not counted as absent. Check them in Freshdesk.
- **Deleted in Freshdesk during the import**, **In Freshdesk Trash**, **In Freshdesk Spam**:
  tickets that were not imported, by id.
- **Images already gone in Freshdesk (not recoverable)**: inline images that Freshdesk answers
  404 or 410 for, by ticket id and message. They do not change the status. Nothing can be
  recovered; the report is the record that they were already missing.
- **Warnings**: data that was imported with a change you should know about, for example a
  contact without an email or a Freshdesk agent who has no Colliedesk account.

Do not close the Freshdesk account until the last report says `OK`.

## Limits

- Images inside a message are copied only when Freshdesk hosts them, only as PNG, JPEG, GIF or WebP,
  up to 5 MB each and 5 MB embedded per message. Images from other sites stay links.
- Freshdesk has no documented API for time entries of archived tickets. The report lists those
  tickets; check them in Freshdesk.
- Contacts without an email get an address of the form `freshdesk-contact-<id>@import.invalid`.
- A catch-up import with `--since` updates status, priority, subject and assignee from Freshdesk
  and adds new replies and tags. It does not remove tags.
- The import stops before a download when less than 256 MB would stay free on the attachments
  volume.

If a ticket's Freshdesk responder matches a deactivated Colliedesk agent, the conversation is imported unassigned and the report lists it under the warning `assignee_deactivated`. Reactivate the agent and re-run to assign it.
