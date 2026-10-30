# Saved replies

A saved reply is text your whole team inserts into the reply box, for example a greeting or the steps
of a refund. Every saved reply is shared by the workspace. Every agent can add, edit and delete them;
there is no admin-only setting. AI agents can work on them too (see [AI agents](#ai-agents)). Every
change is kept as a version you can restore, and a deleted reply waits in a trash for 30 days.

## Insert a saved reply

1. Open a conversation.
2. Select **Insert saved reply** under the message box, or press `/` when no text field has focus.
   Inside the message box, press `Ctrl+/` (`Cmd+/` on a Mac): a plain `/` there is just a slash.
3. Type to search by title. Case and accents do not matter: `chao` finds `Chào`.
4. Press the arrow keys to move, then `Enter` (or click) to insert. `Esc` closes the list and
   changes nothing.

The text goes in at the cursor and replaces nothing. The rest of your draft and the files you chose
stay. You read and edit the text before you send it. It also works in a private note. To take an
inserted reply back out, press `Ctrl+Z` (`Cmd+Z`): the text goes in like typing, so undo works as usual.

If the list is still loading it says so; it never claims there are no saved replies before it knows.
When no saved reply exists, the list has no search box; **Manage saved replies** opens Settings in a new
tab, so your draft and files stay where they are.

### Variables

Two variables are filled in when you insert a reply:

| Variable | Filled with |
|---|---|
| `{{customer.name}}` | The name of the contact of the conversation you are in |
| `{{agent.name}}` | Your own name (the display name on your account). If your account has no display name yet, the part of your email address before the `@` (without a `+tag`) |

Spaces inside the braces are fine: `{{ customer.name }}`. If a name is not known, the variable is
removed together with the space before it, so `Hi {{customer.name}}, thanks` becomes `Hi, thanks`. A
raw `{{customer.name}}` is never left in the box. Any other `{{…}}` stays as written, so you see it
and can fix it before sending. If a reply (not a private note) still contains `{{…}}` when you select
**Send reply**, Colliedesk lists it and asks you to confirm, because the customer would see it as written.

## Manage saved replies

Open **Settings → Saved replies**.

- **Add**: select **New saved reply**, type a **Title** and the **Text**, then select **Add saved reply**
  (or **Cancel**).
- **Edit**: select **Edit** next to a reply, change it, then **Save saved reply**. Select **Cancel** to
  leave it as it was. The change applies for the whole team at once. If a teammate saved the same
  reply after you opened it, your edit is refused (nothing is overwritten) with "changed by someone
  else"; select **Reload** to see their version, then edit again.
- **Delete**: select **Delete**, then confirm. The reply moves to the trash: it disappears from the
  list and from **Insert saved reply** for everyone. If a teammate saved the reply after you opened
  the page, the delete is refused like an edit, so you never delete text you have not seen. Conversations and messages that used it are not
  changed, because inserting only copies text.

Under each reply, a line says who made the latest change and when, for example
`Edited by Ada via Claude triage`. "via" names the [agent token](sessions.md) an AI agent used;
"(approved request)" means a person approved the change an agent asked for.

### History and restore

1. Select **History** next to a reply. The panel lists the versions, newest first: who changed it,
   through which agent token, the title and text it had, and whether that version changed the title,
   the text or both. A long text is folded; select **Show all** to read it. **Show older versions**
   loads the next 50.
2. Select **Restore version N** on an older version. Its title and text become the current reply.

A reply that existed before history was kept starts with **State before history**: its text at that
moment, with no author, because who last edited it is not known.

Restoring adds a new version; the history is never rewritten, so you can restore again the same way.
If someone else saved the reply after you opened the panel, the restore is refused and nothing changes.
Select **Close history** (or **History** again) to close the panel.

### Trash

1. Select **Trash** under the list. It shows the replies deleted in the last 30 days, who deleted each
   one, and how many days are left. **Show more** loads the next 50.
2. Select **Restore** next to a reply. It comes back as it was, with its history.

After 30 days in the trash a reply is deleted for good together with its history. The server checks
once a day. If another reply took the title in the meantime, the restore is refused: rename one of
them first.

Rules:

- The title is unique among the replies outside the trash. `Refund` and `refund` count as the same title.
- A title holds at most 200 characters and the text at most 20 000.
- A workspace holds at most 500 saved replies, counting the ones in the trash. Deleting does not make
  room until the reply leaves the trash after 30 days; this keeps a create-and-delete loop from
  growing the trash without bound.
- The text is plain text. Saved replies hold no files and no formatting.

## AI agents

An AI agent connected through [MCP](mcp.md) or the API with an agent token can read, write and
restore saved replies. What happens depends on the tier of its token:

| Token tier | Read, history, trash | Create, edit, delete, restore |
|---|---|---|
| `send` | Yes | Yes, at once. The version names the token |
| `draft`, `read` | Yes | Proposed: Colliedesk files an [agent request](agent-requests.md) and nothing changes until a person approves it |

On **Requests**, a proposed change shows the reply before and after: removed text marked `−`, added
text marked `+`. Colliedesk keeps the reply as the agent saw it when it asked. If someone changed the
reply since, the request says so and shows the reply as it is now; approving such an edit or delete
fails without changing anything, and the agent must ask again. A proposed delete shows what would go
to the trash. A proposed restore shows the version it brings back against the current text.

## API

`GET`, `POST`, `PATCH` and `DELETE` on `/api/v1/saved-replies`, `GET /api/v1/saved-replies/trash`,
`POST /api/v1/saved-replies/{id}/restore` (out of the trash), `GET /api/v1/saved-replies/{id}/versions`
and `POST /api/v1/saved-replies/{id}/versions/{version}/restore` (see `contracts/openapi.yaml`). Every
write sends back the `updated_at` the caller read (`DELETE` as `?updated_at=`); a reply saved since
answers 409 `changed_elsewhere`. The trash and the versions come in pages (`limit`, at most 200;
`cursor` / `before_version`). Reading
needs `conversations:read`; writing needs `conversations:write`, which every agent has. The API
returns the text as stored, with the variables unfilled.

## Import and export

- [Import from Freshdesk](import-freshdesk.md) brings your canned responses over as saved replies.
- [Export all your data](export.md) includes them in `saved-replies.ndjson`, the ones in the trash
  too, and every version in `saved-reply-versions.ndjson`.
