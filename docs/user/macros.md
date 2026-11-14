# Macros

A macro fills the reply box and, when you ask it to, sets the status, the priority, the assignee and
the tags of the conversation in the same step. You still read the reply and send it yourself.
Nothing is emailed by applying a macro, and a status it sets does not tell the customer in chat.

A **shared** macro is for the whole team. A **personal** macro is only yours: other people do not
see it, and it is not listed for them. There is no group to assign to.

## Apply a macro

1. Open a conversation.
2. Select **Apply macro** under the message box.
3. Type to search by title, then choose the macro.

The reply text goes in at the cursor. The status, priority, assignee and tags change at once, and
the page says **Applied** with **Undo**. Undo puts those fields back where they are still as the
macro left them. A field you changed after applying is left as you set it. The text comes out of
the box when it is still the text the macro inserted; if you have edited it, it stays.

`{{customer.name}}` and `{{requester.name}}` become the contact's name, `{{agent.name}}` becomes
your name, and `{{ticket.id}}` becomes the conversation's number (such as `#12`). A name that is
not known is removed with the space before it. Any other `{{…}}` stays as written, so you see it
before you send.

## Manage macros

Open **Settings → Macros**. You need `macros:read` to see them, and `macros:create`,
`macros:update` or `macros:delete` to change them. The Agent role includes these.

- **Add**: select **New macro**, give it a title, choose shared or personal, and add reply text or
  at least one action (a status, a priority, an assignee or tags). Select **Save**.
- **Edit** and **Delete** are on the row. A delete asks you to confirm, then the macro waits in
  **Trash** for 30 days. **Restore** brings it back.
- Titles are unique ignoring case among shared macros, and among your personal ones. A workspace
  holds at most 500 shared macros and you hold at most 100 personal ones, not counting the trash.

## AI agents

An AI agent uses the same macros through [MCP](mcp.md). `colliedesk_macro_apply` fills the text and
sets the fields in one call; `colliedesk_reply_send` sends the reply. `colliedesk_macro_undo` puts
the fields back.

| To | Needs |
|---|---|
| List, including the trash | `macros:read` |
| Create or edit | `macros:create` or `macros:update` |
| Delete or restore | `macros:delete` |
| Apply or undo on a conversation | `conversations:update` |

## API

`GET` and `POST /api/v1/macros`, `GET /api/v1/macros/trash`, `PATCH` and `DELETE /api/v1/macros/{id}`,
`POST /api/v1/macros/{id}/restore`, `POST /api/v1/conversations/{id}/macros/{macroId}/apply` and
`POST /api/v1/conversations/{id}/macro-applications/{applicationId}/undo` (see
`contracts/openapi.yaml`). Applying returns the filled-in text and the conversation. It does not
send a message.

## Import and export

Freshdesk canned responses stay [saved replies](saved-replies.md): they are text, without actions.
Freshdesk macros are not imported.

[Export all your data](export.md) includes every macro, personal ones and the trash, in
`macros.ndjson`. The text keeps its variables unfilled.
