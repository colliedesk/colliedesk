# Import from Help Scout

`collie import helpscout` copies one Help Scout inbox into a Colliedesk workspace through the Help
Scout Inbox API 2.0. It works like the [Freshdesk import](import-freshdesk.md): it ends with a
reconcile report that compares what Help Scout holds with what Colliedesk now holds. If anything is
missing, the report says **FAILED** and lists the Help Scout ids. If the totals could not be
confirmed, it says **UNVERIFIED**. Both end with a non-zero exit code. Run the same command again to
retry: finished items are never copied twice.

## What is imported

| Help Scout | Colliedesk |
|---|---|
| Conversations of one inbox | Conversations with the original subject, status, assignee, tags and timestamps. The conversation number is shown in the conversation details |
| Customer threads | Incoming messages |
| Replies | Outgoing messages, with the original author, time and to/cc/bcc |
| Notes | **Private notes.** A note is never shown as sent to the customer |
| Forwards and logged phone calls | Private notes that keep their Help Scout kind |
| Chat threads | Incoming or outgoing messages, by who wrote them |
| Original thread HTML | **Show original** on the message (a locked frame that runs no scripts and loads nothing) |
| Attachments | Files stored by Colliedesk, each with a SHA-256 checksum |
| Images inside a thread that Help Scout hosts | Stored by Colliedesk with a SHA-256 checksum, embedded in **Show original**, and listed as files on the message |
| Customers | Contacts, matched by email when the contact already exists |
| Users | Matched by email to your Colliedesk agents; otherwise the author name is kept |

Status mapping: Active → Open, Pending → Pending, Closed → Closed. Help Scout has no priority, so
every conversation is Normal.

Not imported, and listed in the report: Spam, deleted and draft conversations; draft threads that
were never sent; line items (Help Scout's record of a status or assignee change); attachments Help
Scout marked as a virus. Also not imported: custom fields, saved replies, workflows, ratings and
customers who have no conversation in the inbox.

## Before you start

1. Create the Colliedesk workspace (on your own server, see
   [self-host](../self-host/README.md)).
2. Invite your team first. Replies by a Help Scout user link to a Colliedesk agent only when an agent
   with the same email exists.
3. Create a Help Scout app. Use an account that can see the inbox:
   1. Sign in to Help Scout.
   2. Select your profile picture, then **Your Profile > My Apps**.
   3. Select **Create My App**. Any name works. Any HTTPS address works as the redirection URL; the
      import does not use it.
   4. Copy the **App ID** and the **App Secret**.

The App Secret gives read access to everything the account can see. Keep it in an environment
variable. Colliedesk never accepts it as a command-line argument and never prints it.

## Run the import

1. Put the App ID and App Secret in environment variables:
   ```sh
   read -r HELPSCOUT_APP_ID && export HELPSCOUT_APP_ID
   read -rs HELPSCOUT_APP_SECRET && export HELPSCOUT_APP_SECRET
   ```
2. Preview the import. A dry run counts conversations, customers and users and writes nothing:
   ```sh
   docker compose exec -e HELPSCOUT_APP_ID -e HELPSCOUT_APP_SECRET server /app/collie import helpscout \
     --workspace <slug> --dry-run --report-dir /data/import-reports
   ```
   If the account has more than one inbox, the command stops and lists them as `<id> (<name>)`.
   Add `--mailbox <id>` to choose one.
3. Run the import:
   ```sh
   docker compose exec -e HELPSCOUT_APP_ID -e HELPSCOUT_APP_SECRET server /app/collie import helpscout \
     --workspace <slug> --mailbox <id> --report-dir /data/import-reports
   ```
4. Copy the reports out of the container:
   ```sh
   docker compose cp server:/data/import-reports ./import-reports
   ```

To import several inboxes, run the import once per inbox. Each inbox is a separate import with its
own report and its own undo.

Options:

- `--mailbox ID`: the Help Scout inbox to import. Needed when the account has more than one.
- `--app-id-env NAME`, `--app-secret-env NAME`: read the credentials from other variables.
  Defaults `HELPSCOUT_APP_ID` and `HELPSCOUT_APP_SECRET`.
- `--since YYYY-MM-DD`: import only conversations modified after this UTC date. Use it for a final
  catch-up import just before you switch.
- `--dry-run`: count only.
- `--report-dir DIR`: where to write the report. Default: the current directory.

The import respects Help Scout's rate limit. When Help Scout answers 429 or says that no requests
are left this minute, the import waits as long as Help Scout asks. A large inbox can take hours.
Help Scout access tokens expire after two days; the import asks for a new one when Help Scout
refuses the old one. You can stop it at any time with Ctrl+C.

While it runs, the command prints progress lines to stderr. The summary on stdout gives the status,
the counts and the paths of the two report files.

## Resume or retry

Run the same command again. Items that were imported are skipped, an interrupted conversation
continues where it stopped, and failed items are tried again. Nothing is duplicated. A conversation
is read again when Help Scout reports a later change or a different number of threads. A file that is
lost or damaged on disk after the import is detected by the next run and downloaded again.

On Colliedesk Cloud, the first import into a workspace that is still in its trial extends the trial
to 30 days in total. On a paid plan, imported files count toward the storage limit of the plan.
Before it writes anything, the import adds up the attachments and images it would bring in. If they
do not fit, it stops, names the bytes needed and free, and imports nothing. This works as described
for the [Freshdesk import](import-freshdesk.md#resume-or-retry).

Only one import or undo runs in a workspace at a time. A second one stops at once with
"already running".

## How completeness is checked

The import reads every page of the inbox's conversation list and compares the number it counted
with the total Help Scout reports on the list (`page.totalElements`). If they differ, the report is
**UNVERIFIED**. Every attachment is checked against the size Help Scout reports for it. A file whose
size differs is a failure and is downloaded again on the next run.

## Images inside threads

The import copies each `<img>` in a thread whose address is on a Help Scout host (`helpscout.net`
or one of its subdomains). It stores the file like an attachment and embeds it in **Show original**,
so the message keeps showing it after you close Help Scout. Only PNG, JPEG, GIF and WebP images are
copied, up to 5 MB each and 5 MB embedded per message. Images from other sites stay links. An image
whose address names Help Scout but is on another host raises the warning
`inline_images_not_rehosted`: save it before you close the account.

An image Help Scout no longer has (HTTP 404 or 410) does not fail the import. The report lists it
under **Images already gone in Help Scout (not recoverable)**. Any other problem fails the image and
the status is `FAILED`. Run the same command again to retry.

## Undo an import

Undo works as for Freshdesk:

```sh
docker compose exec server /app/collie import undo --workspace <slug> --dry-run
docker compose exec server /app/collie import undo --workspace <slug>
```

If the workspace holds more than one import, add `--source helpscout:mailbox:<id>` (the report names
the source). Undo removes the conversations the import created, with their messages, attachments
and images, and the contacts and tags it created that nothing else uses. A conversation where your
team wrote a message after the import is kept; the command lists its Help Scout conversation id.

## Read the report

Each run writes `helpscout-import-<workspace>-<time>.md` for people and `.json` for tools.

- **Status**: `OK`, `FAILED`, `UNVERIFIED` or `DRY_RUN`, with the same meaning as for Freshdesk.
- **Counts**: conversations, contacts, messages, attachments, inline images, and tags on
  conversations, in Help Scout and in Colliedesk.
- **Failures**: each failed item with its Help Scout id and conversation id.
- **Spam**, **deleted** and **draft** conversations, **draft threads** and **virus** attachments:
  listed by id, not imported.
- **Warnings**: for example a user without a Colliedesk agent, a customer without an email, or a
  conversation assigned to a Help Scout team (imported unassigned).

Do not close the Help Scout account until the last report says `OK`.

## Limits

- One inbox per run.
- A conversation assigned to a Help Scout team is imported unassigned.
- A customer without an email gets an address of the form
  `helpscout-customer-<id>@import.invalid`.
- A catch-up import updates subject, status and assignee from Help Scout and adds new threads and
  tags. It does not remove tags.
- Images inside threads are copied only from `helpscout.net` hosts. This and the other mappings
  were built from the Help Scout API documentation; they have not yet been checked against a live
  Help Scout account.
