# Export all your data

Download everything in your workspace as one ZIP file. Use it as a backup, to move to another
helpdesk, or to keep a copy when a cloud plan ends. The format is open and documented here.

## Who can export

Admins only, because the file holds every contact. Agents get "403 Forbidden".

The export also works when the workspace is locked (a cloud trial or subscription ended) and on
self-hosted installs.

## How to export

In the app, open **Settings → Export all data** and select **Export all data**. On a locked cloud
workspace the same button is on the locked page. Your browser then downloads the file itself, with its
own progress bar, straight to disk: a large workspace is never held in memory. Keep the page open until
the download finishes. If the server is busy with other exports you are told how long to wait. If
another export of your workspace is still running you are told to wait for it. A file that stops early
has no `manifest.json`; download it again.

Without the app, send an authenticated `GET /api/v1/export` (the app uses `POST /api/v1/export/link`, which returns a one-time URL valid for 60 seconds that streams the same file without a token). With an API token:

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
| `agents.ndjson` | One agent per line: `id`, `email`, `display_name`, `role`, `timezone_override`, `created_at`, `deactivated_at` (`null` for an active agent) |
| `contacts.ndjson` | One contact per line: `id`, `name`, `email`, `organization`, `created_at`, `updated_at` |
| `tags.ndjson` | One tag per line: `id`, `name`, `color`, `created_at` |
| `saved-replies.ndjson` | One saved reply per line, including the ones in the trash: `id`, `title`, `body`, `created_by_agent_id` (an agent id or `null`), `created_at`, `updated_at`, `deleted_at` (when it went to the trash, or `null`), `deleted_by_agent_id`. The body keeps the variables `{{customer.name}}` and `{{agent.name}}` unfilled |
| `saved-reply-versions.ndjson` | Every [version](saved-replies.md#history-and-restore) of every saved reply, one per line, ordered by reply and version: `saved_reply_id`, `version`, `kind` (`create`, `edit`, `delete`, `restore` or `undelete`), `restored_from_version`, `title`, `body` (the reply after the change), `changed_by_agent_id`, `token_id`, `token_label` (the agent token that made or asked for it), `agent_request_id` (the approved agent request that made it), `created_at` |
| `agent-requests.ndjson` | One [agent request](agent-requests.md) per line: `id`, `kind`, `status`, `method`, `route`, `path`, `body` (the call as the token sent it), `edited_body` (a reply draft as a person edited it, or `null`), `summary`, `conversation_id`, `token_id`, `token_label`, `token_scope`, `requested_by_agent_id`, `decided_by_agent_id`, `decided_at`, `reason`, `result_status`, `result_note`, `created_at`, `expires_at`. The answer of an approved call is not exported. `body` and `edited_body` are `null` 90 days after the decision |
| `conversations.ndjson` | One conversation per line, with its messages and time entries |
| `attachments/<attachment id>/<file name>` | The original bytes of each attachment |

NDJSON means one JSON object per line. Read it line by line. All times are UTC (RFC 3339).

A conversation line has `id`, `contact_id`, `subject`, `status`, `priority`, `assignee_id`
(an agent id or `null`), `origin` (where an imported ticket came from, or `null`), `tag_ids`,
`created_at`, `updated_at`, `messages` and `time_entries`.

Each message has `id`, `conversation_id`, `author_agent_id`, `author_name`, `kind`
(`incoming`, `outgoing` or `note`), `body`, `body_html`, `origin`, `email_message_id`,
`email_meta` (the sender, authentication result, `via`, Cc addresses and attachment names of a
received message; `via` is `platform` or `workspace` for email and `web_form` for text typed into
the web form, whose sender address is unverified and whose `authentication` is `none`), `cc` (the
addresses a reply was copied to, or `null`), `agent_request_id` (the agent request whose approval sent
this reply, or `null`), `created_at` and `attachments`. Each attachment entry has `id`, `filename`, `content_type`,
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
