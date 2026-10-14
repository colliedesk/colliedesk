# Import from Freshdesk

`colliedesk import freshdesk` copies a Freshdesk account into a CollieDesk workspace through the
Freshdesk API. It ends with a reconcile report that compares what Freshdesk holds with what
CollieDesk now holds. If anything is missing, the report says **FAILED** and lists the Freshdesk
ids. Run the same command again to retry: finished items are never copied twice.

## What is imported

| Freshdesk | CollieDesk |
|---|---|
| Contacts | Contacts (matched by email when the contact already exists) |
| Companies | The contact's **organization** |
| Agents | Matched by email to your CollieDesk agents; otherwise the author name is kept |
| Tickets, including archived tickets | Conversations with the original subject, status, priority, assignee, tags and timestamps |
| Ticket description, public replies, incoming replies, private notes | Timeline messages (incoming, reply, private note) with the original author and time |
| Attachments | Files stored by CollieDesk, each with a SHA-256 checksum |
| Time entries | Time entries (agent, minutes, note, billable, time) |

Status mapping: Open → Open, Pending → Pending, Resolved → Resolved, Closed → Closed. Custom
statuses become Pending and are listed in the report. Priority: Low, Medium → Normal, High, Urgent.

The report lists what is **not** imported, for example custom fields and knowledge base articles.

## Before you start

1. Create the CollieDesk workspace (see [Install](install.md)).
2. Invite your team first. Replies by an agent link to that agent only when a CollieDesk agent
   with the same email exists.
3. Get a Freshdesk API key from an admin account with access to all tickets:
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
- `--report-dir DIR`: where to write the report. Default: the current directory.

The import respects Freshdesk's rate limit. It waits when Freshdesk says the limit is used up,
so a large account can take hours. You can stop it at any time with Ctrl+C.

## Resume or retry

Run the same command again. Items that were imported are skipped, an interrupted ticket continues
where it stopped, and failed items are tried again. Nothing is duplicated.

## Read the report

Each run writes two files: `freshdesk-import-<workspace>-<time>.md` for people and `.json` for
tools.

- **Status**: `OK` means every count matches. `FAILED` means something is missing; the
  **Failures** table lists each item with its Freshdesk id and ticket id. `DRY_RUN` is a preview.
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
