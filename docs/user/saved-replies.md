# Saved replies

A saved reply is text your whole team inserts into the reply box, for example a greeting or the steps
of a refund. Every saved reply is shared by the workspace. Every agent can add, edit and delete them;
there is no admin-only setting.

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
When no saved reply exists, **Manage saved replies** opens Settings in a new tab, so your draft and
files stay where they are.

### Variables

Two variables are filled in when you insert a reply:

| Variable | Filled with |
|---|---|
| `{{customer.name}}` | The name of the contact of the conversation you are in |
| `{{agent.name}}` | Your own name (the display name on your account). If your account has no display name yet, the part of your email address before the `@` |

Spaces inside the braces are fine: `{{ customer.name }}`. If a name is not known, the variable is
removed together with the space before it, so `Hi {{customer.name}}, thanks` becomes `Hi, thanks`. A
raw `{{customer.name}}` is never left in the box. Any other `{{…}}` stays as written, so you see it
and can fix it before sending. If a reply (not a private note) still contains `{{…}}` when you select
**Send reply**, Colliedesk lists it and asks you to confirm, because the customer would see it as written.

## Manage saved replies

Open **Settings → Saved replies**.

- **Add**: type a **Title** and the **Text**, then select **Add saved reply**.
- **Edit**: select **Edit** next to a reply, change it, then **Save saved reply**. Select **Cancel** to
  leave it as it was. The change applies for the whole team at once. If a teammate saved the same
  reply after you opened it, your edit is refused (nothing is overwritten) with "changed by someone
  else"; select **Reload** to see their version, then edit again.
- **Delete**: select **Delete**, then confirm. This removes the reply for everyone. Conversations
  and messages that used it are not changed, because inserting only copies text.

Rules:

- The title is unique in the workspace. `Refund` and `refund` count as the same title.
- A title holds at most 200 characters and the text at most 20 000.
- A workspace holds at most 500 saved replies.
- The text is plain text. Saved replies hold no files and no formatting.

## API

`GET`, `POST`, `PATCH` and `DELETE` on `/api/v1/saved-replies` (see `contracts/openapi.yaml`). Reading
needs `conversations:read`; writing needs `conversations:write`, which every agent has. The API
returns the text as stored, with the variables unfilled.

## Import and export

- [Import from Freshdesk](import-freshdesk.md) brings your canned responses over as saved replies.
- [Export all your data](export.md) includes them in `saved-replies.ndjson`.
