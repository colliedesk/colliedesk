# Export all your data

Download everything in your workspace as one ZIP file. Use it as a backup, to move to another
helpdesk, or to keep a copy when a cloud plan ends. The format is open and documented here.

## Who can export

Admins only, because the file holds every contact. Agents get "403 Forbidden".

The export also works when the workspace is locked (a cloud trial or subscription ended) and on
self-hosted installs.

## How to export

In the app, open **Settings → Export all data** and select **Export all data**. On a locked cloud
workspace the same button is on the locked page. The page shows how much has arrived while the file
is built; keep it open until the file is saved. If the server is busy with other exports you are
told how long to wait. If another export of your workspace is still running you are told to wait
for it. The browser keeps the file in memory until it is complete, so for a very large workspace use the `curl` command below. A file that stops early is never saved: you get an error and can try again.

Without the app, send an authenticated `GET /api/v1/export`. With an API token:

```sh
curl -fSL -H "Authorization: Bearer $COLLIEDESK_TOKEN" \
  -o colliedesk-export.zip https://desk.example.com/api/v1/export
```

The server builds the ZIP while it sends it, so the download starts at once and large workspaces
do not need extra memory. The file is named `colliedesk-<workspace slug>-<UTC time>.zip`.

Only one export of a workspace runs at a time. A second request gets "409 Conflict" until the
first one has sent its last byte. A server also runs at most two exports at once; another one
gets "429 Too Many Requests" with a `Retry-After` header. Wait and try again.

Keep reading while the file downloads. If your client stops reading for more than 60 seconds, the
server cancels the export and frees its resources; start it again. One export may run for at most
2 hours in total, however fast the client reads.

## Check that the file is complete

A complete export contains `manifest.json`. If the server cannot read a stored file, or a file does
not match its recorded checksum, it aborts the download. You then get a broken ZIP without
`manifest.json`. Run the export again; if it fails again, report the attachment id in the server log.

## Archive layout (format version 1)

| Path | Content |
|---|---|
| `manifest.json` | Format, version, export time (UTC), workspace, record counts, and every attachment file with its size and SHA-256 |
| `agents.ndjson` | One agent per line: `id`, `email`, `display_name`, `role`, `timezone_override`, `created_at` |
| `contacts.ndjson` | One contact per line: `id`, `name`, `email`, `organization`, `created_at`, `updated_at` |
| `tags.ndjson` | One tag per line: `id`, `name`, `color`, `created_at` |
| `conversations.ndjson` | One conversation per line, with its messages and time entries |
| `attachments/<attachment id>/<file name>` | The original bytes of each attachment |

NDJSON means one JSON object per line. Read it line by line. All times are UTC (RFC 3339).

A conversation line has `id`, `contact_id`, `subject`, `status`, `priority`, `assignee_id`
(an agent id or `null`), `origin` (where an imported ticket came from, or `null`), `tag_ids`,
`created_at`, `updated_at`, `messages` and `time_entries`.

Each message has `id`, `conversation_id`, `author_agent_id`, `author_name`, `kind`
(`incoming`, `outgoing` or `note`), `body`, `body_html`, `origin`, `email_message_id`,
`email_meta` (the sender, authentication result and attachment names of a received email),
`created_at` and `attachments`. Each attachment entry has `id`, `filename`, `content_type`,
`size`, `sha256`, `created_at` and `path`, the file's location in the ZIP.

Each time entry has `id`, `conversation_id`, `agent_id`, `agent_name`, `minutes`, `note`,
`billable` and `executed_at`.

Attachment files are stored without compression, under a directory named by the attachment id, so
two files with the same name never collide. File names are cleaned: directories and control
characters are removed, and an empty name becomes `file`. To verify a file, compute its SHA-256
and compare it with `sha256` in `manifest.json`.

## What is not exported

Sessions and API tokens, pairing and invite links, the email channel's inbound secret, queued
email delivery state, and billing records. They are credentials or server internals, not your data.

## Limits

The rows are read in one snapshot, so a conversation and its messages always agree. Changes made
after the export starts are not in the file. The snapshot is open only while the records are sent;
the attachment files follow without it. Messages are sent one at a time, so a very long thread
does not need extra memory. The list of attachment files is kept in a temporary file on the
server, so the server needs a writable temp directory.
