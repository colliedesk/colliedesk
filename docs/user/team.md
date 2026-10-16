# Manage your team

**Settings → Team** lists everyone who can sign in to the workspace: name, email, role, whether they
are **Active** or **Deactivated**, and when they were last active. Agents see the list read-only.
Admins can change roles, deactivate and reactivate people. To add someone, see
[Invite your team](invites.md).

## Change a role

Admins pick **Agent** or **Admin** in the role list of a row, then select **Apply**. Nothing changes
until you select **Apply**. The change applies to new requests at once. Demoting someone also cancels
their unused admin invites. A person who is already signed in keeps the access their sign-in link gave them; they sign in
again to use a higher role.

A workspace always keeps at least one active admin. For the last one, the role list is locked and
**Deactivate** is not offered; make someone else an admin first. The server also refuses it ("A
workspace needs at least one active admin").

## Deactivate someone

Use **Deactivate** instead of deleting. Nothing is lost: their name stays on old conversations and
messages, and the export still lists them with `deactivated_at`.

1. Select **Deactivate** in their row. The question shows how many open and pending conversations
   are assigned to them.
2. Choose where those conversations go: **Move them to Unassigned** (default) or **Assign them to
   another agent**.
3. Select **Yes, deactivate**.

What happens, in one step:

- They cannot sign in any more (invite link, pairing link or cloud sign-in), and every device they
  were signed in on stops working at once.
- Their open and pending conversations move as you chose. Resolved and closed conversations keep
  their assignee.
- On the hosted cloud, the seat is free again right away, so you can invite someone else.
- They no longer appear in assignee lists, and nobody can assign a conversation to them.

If you deactivate yourself, you are signed out. You can only do that while another active admin exists.

## Reactivate someone

Select **Reactivate** in a deactivated row. On the hosted cloud this uses a seat again; if all seats
are taken you get the plan-limit message, and you free a seat first (deactivate someone or revoke an
invite). Reactivating does not give back their conversations or devices. Send them a new invite link,
or create a pairing link, so they can sign in. An invite to the email of a deactivated agent is
refused with "reactivate them in the team list".
