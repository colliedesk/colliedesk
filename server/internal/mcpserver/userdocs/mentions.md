# Mentions: ask a teammate in a private note

When you need a teammate on one conversation, ask them in a private note instead of reassigning it or
writing in another chat tool. They are told in their **Mentions** list and by email, and you see when they
read it.

## Ask a teammate

1. Open the conversation and choose **Private note** (or press N).
2. Type **@**. A list of your active teammates opens under the box. Keep typing to filter it by name or
   email.
3. Pick a person with the arrow keys and Enter (or Tab), or click them. **Esc** closes the list and keeps
   what you typed. Their name goes into the note as **@Name**.
4. Press **Add private note**.

- Only active members of your workspace are listed and can be asked. Customers and deactivated teammates
  never are, and you are never asked by your own note.
- **@** in a reply to the customer does nothing: only a private note asks teammates.
- Deleting **@Name** from the text before you send it takes the ask back.

## See that they were told

Under the posted note, each mention is in bold and a quiet line says what happened:

- **Long Bui was told · in Mentions and by email** right after you post it (**in Mentions** only, when they
  turned the email off).
- **Long Bui saw this Nov 3, 2:12 PM** once they open the note.

The person you asked sees **Hoàng Tú asked you · marked read when you opened it** instead.

## Your Mentions list

**Mentions** in the sidebar counts the notes that asked you and that you have not read.

- **Unread N** lists the ones you have not read, **All** every one, newest first. **Mark all read** clears
  the count.
- Each row shows who asked, the conversation's subject, the start of the note, the time and a dot while it
  is unread. A note an AI agent wrote through an agent token reads **Mai Tran via Claude**: the person the
  token acts as, and the token's name.
- Opening a row opens the conversation beside the list, scrolled to the note, and marks it read. Opening the
  conversation any other way marks it read too.

## Email

Each mention sends one email to the person asked, from the server's platform address, with the start of the
note and a link that opens the conversation at the note. Adding the same person again never sends a second
one.

To stop these emails, untick **Email me when someone mentions me** in **Settings → Account → Preferences**.
It is on for everyone at first, and it is yours on every browser. Mentions still reach your list while it is
off. A server that cannot send mail (no SMTP relay or no `MAIL_FROM_DOMAIN`) sends none; the list still
works.

## Ask more people on a note you wrote

An AI agent, or a script with an agent token, can add teammates to a note its person wrote
(`PUT /api/v1/conversations/{id}/messages/{messageId}/mentions`, the MCP tool `colliedesk_note_mention`).
Only the people the note did not ask yet are told. A mention cannot be taken back once the person was told.

## For AI agents (MCP)

| Tool | What it does |
| --- | --- |
| `colliedesk_note_add` | Adds a private note; `mentions` asks teammates by email or id |
| `colliedesk_note_mention` | Asks more teammates on a note you wrote; only the new ones are told |
| `colliedesk_mentions_list` | Lists the notes that ask the person your token acts as, unread by default, with the unread count |
| `colliedesk_mentions_mark_read` | Marks those mentions read, or unread again: some, those on one conversation, or all |
| `colliedesk_my_mention_email_set` | Turns the mention email on or off for the person your token acts as |

Each works within the token's permissions: asking needs `messages:note`, reading and marking your mentions
`messages:read`, the email setting `profile:update`. The API is in `contracts/openapi.yaml`
(`/api/v1/mentions`, `/api/v1/mentions/read`, `/api/v1/agents/me/mention-email`).
