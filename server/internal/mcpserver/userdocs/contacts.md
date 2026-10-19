# Customers (contacts)

A **contact** is a customer who writes to you, or whom you write to. Colliedesk adds one by itself the
first time mail, the web form or an import brings an address it does not know. One address is one
contact in a workspace.

## Open a customer's page

- From a conversation: select the customer's name under the subject, above the timeline.
- From search: type a name or an email address in **Search conversations**. Customers who match are
  listed under **Customers** above the conversations; select one.
- The address `/contacts/<id>` opens the page directly, so it can be bookmarked or shared with a teammate.

The page shows the name, the email address, the company when one is known (for example from a
Freshdesk company), and when the customer was added. Below that, **Conversations** lists every
conversation with the customer: any status, imported ones included (marked **Imported**), the most
recently active first, 25 to a page. Select one to open it in the inbox.

## Rename a customer

Select **Rename**, change the name and select **Save name**. The team sees the new name everywhere,
and later emails address the customer by it. The email address does not change. Rename again to undo.

## Merge two contacts of the same person

Imports and email can create two contacts for one person, for example when they write from a work
and a home address.

1. Open the page of the contact to **keep**.
2. Select **Merge a duplicate…**, search for the other contact and select it.
3. Read the confirmation and select **Merge**.

What a merge does:

- Every conversation of the duplicate moves to the kept contact. Their last activity does not change.
- Replies on those conversations go to the kept contact's email address.
- The duplicate keeps its name and address. It no longer appears in searches: searching for its name
  or address finds the kept contact. Its page says which contact it was merged into. New mail from its address joins the kept contact.
- Nothing is deleted.

The kept contact's page lists each merge under **Merged into this customer**, with how many
conversations moved. **Undo merge** moves those conversations back (the ones still with the kept
contact) and makes the duplicate a contact of its own again. Conversations that arrived after the
merge stay with the kept contact.

Limits: a contact merged already cannot be merged again until that merge is undone, and a contact
that others were merged into cannot itself be merged into a third one. Undo those merges first. A
merged contact cannot start a new conversation; start it with the contact it was merged into.

Replies always go to the kept contact's email address, on every plan and when self-hosted, also on
a conversation the customer started from the other address after the merge. Undoing the merge sends
replies on the conversations it moved to the other address again; conversations that arrived after
the merge stay with the kept contact.

On the hosted cloud trial, a reply goes only to an address that wrote to you first. After a merge
that rule looks at the kept contact's address: a conversation whose customer wrote from the other
address is a first email to the kept address, so it waits for a verified sending domain or a plan.
The refusal says so and names the address the reply would go to.

## Through the API and MCP

- `GET /api/v1/contacts/{id}`: the contact, `conversation_count`, `merged_into` and `merges`.
- `GET /api/v1/conversations?contact={id}`: the history, a page at a time.
- `GET /api/v1/contacts?search=...&limit=...`: find contacts; a merged contact is left out, and a search
  that matches it returns the contact it was merged into.
- `PATCH /api/v1/contacts/{id}` renames, `POST /api/v1/contacts/{id}/merges` merges, and
  `DELETE /api/v1/contacts/{id}/merges/{mergeId}` undoes a merge.
- MCP tools: `colliedesk_contact_get`, `colliedesk_contacts_find`, `colliedesk_contact_update`,
  `colliedesk_contact_merge` and `colliedesk_contact_unmerge` (see [MCP](mcp.md)).

The [export](export.md) keeps every merge, undone or not.
