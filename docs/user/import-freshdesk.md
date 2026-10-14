# Import from Freshdesk

`colliedesk import freshdesk` copies a Freshdesk account into a CollieDesk workspace through the
Freshdesk API. It ends with a reconcile report that compares what Freshdesk holds with what
CollieDesk now holds. If anything is missing, the report says **FAILED** and lists the Freshdesk
ids. If the totals could not be confirmed, it says **UNVERIFIED**. Both end with a non-zero exit
code. Run the same command again to retry: finished items are never copied twice.

## What is imported

| Freshdesk | CollieDesk |
|---|---|
| Contacts | Contacts (matched by email when the contact already exists) |
| Companies | The contact's **organization** |
| Agents | Matched by email to your CollieDesk agents; otherwise the author name is kept |
| Tickets, including archived tickets | Conversations with the original subject, status, priority, assignee, tags and timestamps |
| Ticket description, replies, incoming replies, private and public notes, forwards | Timeline messages with the original author, time, from/to/cc/bcc and Freshdesk kind. A forward to a third party and a public note become notes, never replies to the customer |
| Ticket type, channel, due dates, cc and forwarded-to addresses | Shown in the conversation details |
| Original message HTML | **Show original** on the message (shown in a locked frame that runs no scripts and loads nothing) |
| Attachments | Files stored by CollieDesk, each with a SHA-256 checksum |
| Time entries | Time entries (agent, minutes, note, billable, time), shown under **Time logged** on the timeline |

Status mapping: Open → Open, Pending → Pending, Resolved → Resolved, Closed → Closed. Custom
statuses become Pending and are listed in the report. Priority: Low, Medium → Normal, High, Urgent.

The report lists what is **not** imported, for example custom fields and knowledge base articles.

## Before you start

1. Create the CollieDesk workspace (see [Install](install.md)).
2. Invite your team first. Replies by an agent link to that agent only when a CollieDesk agent
   with the same email exists.
3. Get a Freshdesk API key from an **admin** account with access to all tickets. The import reads
   the account id (View Account), which needs an admin key:
   1. Sign in to Freshdesk.
   2. Select your profile picture, then **Profile settings**.
   3. Copy the key under **Your API Key**.

The key gives read access to the whole account. Keep it in an environment variable. CollieDesk
never accepts it as a command-line argument and never prints it.

## Run the import

1. Put the key in an environment variable:
   ```sh
   read -rs FRESHDESK_API_KEY && export FRESHDESK_API_KEY
   ```
2. Preview the import. A dry run counts contacts, companies, agents and tickets and writes nothing:
   ```sh
   docker compose exec -e FRESHDESK_API_KEY server /app/colliedesk import freshdesk \
     --workspace <slug> --domain <account>.freshdesk.com --dry-run --report-dir /data/import-reports
   ```
3. Run the import:
   ```sh
   docker compose exec -e FRESHDESK_API_KEY server /app/colliedesk import freshdesk \
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
- `--report-dir DIR`: where to write the report. Default: the current directory.

The import respects Freshdesk's rate limit. It waits when Freshdesk says the limit is used up,
so a large account can take hours. You can stop it at any time with Ctrl+C.

## Resume or retry

Run the same command again. Items that were imported are skipped, an interrupted ticket continues
where it stopped, and failed items are tried again. Nothing is duplicated. A file that is lost or
damaged on disk after the import is detected by the next run and downloaded again.

Only one import or undo runs in a workspace at a time. A second one stops at once with
"already running".

## How completeness is checked

Freshdesk's ticket list leaves out archived, trashed and spam tickets. CollieDesk therefore checks
every ticket number from 1 one by one, and keeps checking past the highest number it knows until
200 numbers in a row do not exist (or at least up to `--last-ticket-id`). It also compares its
counts with the totals Freshdesk reports for tickets and contacts. The report says how each total
was verified. If Freshdesk refuses the archive (some plans) or a total cannot be read, the report
is **UNVERIFIED**: check the archive in Freshdesk before you close the account.

## Undo an import

An import into the wrong workspace can be removed:

1. Preview what would be removed:
   ```sh
   docker compose exec server /app/colliedesk import undo --workspace <slug> --dry-run
   ```
2. Remove it:
   ```sh
   docker compose exec server /app/colliedesk import undo --workspace <slug>
   ```

Undo removes the conversations the import created, with their messages, attachments and time
entries, and the contacts and tags it created that nothing else uses. A conversation where your
team wrote a message after the import is kept. Contacts and tags that existed before the import
are kept. Changes without a new message (status, assignee or tags changed in CollieDesk) are
removed together with their conversation. Run the import again afterwards if you want the data back.

## Read the report

Each run writes two files: `freshdesk-import-<workspace>-<time>.md` for people and `.json` for
tools.

- **Status**: `OK` means every count matches and the totals were verified. `FAILED` means
  something is missing; the **Failures** table lists each item with its Freshdesk id and ticket id.
  `UNVERIFIED` means nothing failed but completeness could not be proven (see **How the totals were
  verified**). `DRY_RUN` is a preview.
- **Counts**: contacts, tickets, messages, attachments and time entries in Freshdesk and in
  CollieDesk.
- **Attachment bytes**: total size reported by Freshdesk and total size stored. Every stored file
  is read again and its SHA-256 compared with the value recorded at download.
- **Deleted in Freshdesk during the import**, **In Freshdesk Trash**, **In Freshdesk Spam**:
  tickets that were not imported, by id.
- **Warnings**: data that was imported with a change you should know about, for example a
  contact without an email or a Freshdesk agent who has no CollieDesk account.

Do not close the Freshdesk account until the last report says `OK`.

## Limits

- Images placed inside a message (not attached) stay links to Freshdesk. They stop working when
  the Freshdesk account closes. The report lists the tickets that have them.
- Freshdesk has no documented API for time entries of archived tickets. The report lists those
  tickets; check them in Freshdesk.
- Contacts without an email get an address of the form `freshdesk-contact-<id>@import.invalid`.
- A catch-up import with `--since` updates status, priority, subject and assignee from Freshdesk
  and adds new replies and tags. It does not remove tags.
- The import stops before a download when less than 256 MB would stay free on the attachments
  volume.
