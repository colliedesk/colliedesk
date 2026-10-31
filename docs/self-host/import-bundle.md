# Import a bundle

A bundle carries conversations prepared outside Colliedesk (moving from a system no importer of
Colliedesk reads) into a workspace, on the server, with their original times, codes, statuses and files.
It is an operator's tool: there is no page for it.

```sh
docker compose run --rm --no-deps -T server '/app/collie import bundle --workspace acme --dry-run /import/bundle.zip'
docker compose run --rm --no-deps -T server '/app/collie import bundle --workspace acme /import/bundle.zip'
```

1. `--dry-run` reads and checks everything, writes it in a transaction it rolls back, and prints the same
   report: run it first.
2. Everything goes in one transaction. The report reconciles each count three ways: what the system the
   bundle came from holds (`counts`), what the bundle carries, what was written. A single difference, or a
   file that does not read back as it was, is `FAILED`, and nothing stays.
3. Nothing is sent: no reply is queued, no channel notice goes out, no AI drafts or triages what came in,
   and a response target's reminder that was due before the import is not emailed.
4. A channel, a number series or an assignee the bundle names that the workspace does not have stops the
   import before anything is written, naming each. Create them first.
5. A bundle already imported is refused. `collie import undo --workspace acme --source bundle:<source>`
   removes it, keeping a conversation the team wrote to since (it lists their ids), and the contacts,
   identities, chats and tags it created that nothing else uses. While such a conversation stays, the
   source is still imported: import again under another `source`. Undo never lowers a series' next
   number, so a code is never given twice.

## The archive

A ZIP holding `bundle.json` and the files it names under `files/`. No other entry, none leaving the
archive (`..`, an absolute path); at most 200,000 entries, 256 MiB for `bundle.json`, 100 MiB a file and
8 GiB in all once unpacked. Only `bundle.json` is held in memory: each file is read once to check it and
again as it is stored, so the server needs room for the files in its attachments storage, not in memory.
The files are stored before the transaction; a failed import removes those nothing else of the workspace
uses (the same bytes are one file). A server stopped in the middle of an import can leave stored files no
conversation points at; they take space, nothing else.

## bundle.json

```json
{
  "format": "colliedesk-bundle/1",
  "source": "helpdesk-2026",
  "contacts": [
    {"id": "c1", "name": "Ada", "email": "ada@example.com", "organization": "Example",
     "identities": [{"channel_id": "<channel uuid>", "kind": "zalo_user", "value": "100", "display_name": "Ada"}]}
  ],
  "conversations": [
    {"id": "t1", "contact": "c1", "subject": "The printer jams", "status": "resolved", "priority": "urgent",
     "assignee_email": "ann@example.com", "tags": ["bug"], "channel_id": "<channel uuid>",
     "number_series": "ref", "number": 123, "number_year": 2026, "sla_applies": true,
     "thread_keys": ["user:100"], "place": {"kind": "direct", "external_id": "user:100"},
     "created_at": "2026-03-02T09:00:00Z", "updated_at": "2026-03-04T09:00:00Z",
     "origin": {"reference": "REF-2026-0123"},
     "status_changes": [{"from": "open", "to": "resolved", "at": "2026-03-04T08:00:00Z"}],
     "messages": [
       {"id": "m1", "kind": "incoming", "body": "The printer jams", "created_at": "2026-03-02T09:00:00Z",
        "files": [{"id": "f1", "name": "screen.png", "content_type": "image/png", "path": "files/screen.png",
                   "size": 14, "sha256": "<hex>"}]},
       {"id": "m2", "kind": "outgoing", "body": "Fixed", "author_email": "ann@example.com", "created_at": "2026-03-02T10:00:00Z"},
       {"id": "m3", "kind": "note", "body": "Checked the tray", "author_name": "Ann", "created_at": "2026-03-02T11:00:00Z"}
     ]}
  ],
  "counts": {"contacts": 1, "identities": 1, "conversations": 1, "incoming": 1, "outgoing": 1, "notes": 1,
             "status_changes": 1, "thread_keys": 1, "files": 1, "file_bytes": 14}
}
```

1. `source` names the bundle in letters, digits, `.`, `-` and `_` (starting with a letter or digit, no
   `..`): the import is `bundle:<source>`, and the report files are `bundle-import-<source>.json` and `.md`.
2. A contact is matched to one the workspace has by one of its identities, then by email; otherwise it is
   created. Its identities are added to it.
3. `channel_id` is a channel of the workspace: the conversation is that channel's, its replies leave
   through it, and `thread_keys` thread what the customer writes next into it (for a Zalo OA, `user:<id>`
   or `group:<id>`). Without one, the conversation is `imported`. A customer's message on the channel
   that names no command may join a conversation still open there, by the channel's join policy, as it
   would join one opened in Colliedesk.
4. `place` is the chat of that channel the conversation was opened in: `kind` `direct` (one to one) or
   `group`, `external_id` as the channel names it (`user:<id>` for a direct chat, `group:<id>` for a
   group), and a `name`. Its customer's messages and the team's replies show in that chat (notes do not),
   its customer is one of the channel's senders, a reply to it goes to that chat (a group's to the group),
   and a customer reopens it by its code from that chat only, as one opened in Colliedesk.
5. `number_series` (the series' key, in any case: `REF` is `ref`), `number` and `number_year` keep the
   code the customer has; without them the conversation takes the next number of its series. A number of this year or later in a series whose
   code shows the year (or any number in one whose code does not) moves the series' next number past it.
6. `origin` (an object) is kept on the conversation as it is; Colliedesk adds where it came from under
   `origin.import` (`source` "bundle", `bundle`, `id`), a key the bundle cannot set, and the parts the
   API reads that it left out (`system` "bundle", `external_id` the conversation's id, empty `cc` and
   `forwarded_to`). A part the API reads must have its type (`cc` a list). Each message's origin names it
   the same way, its `kind` reply, incoming or private_note.
7. `sla_applies` gives the conversation response targets (the workspace's, counted from its own times).
8. `status`, `priority` and `kind` take the values of the API (open, pending, resolved, closed; low,
   normal, high, urgent; incoming, outgoing, note). `author_email` of a reply or a note names a teammate;
   otherwise `author_name` is kept as written.
9. Every time is RFC 3339 and stored in UTC. Each file's `size` and `sha256` are checked when the archive
   is read and again once stored.
