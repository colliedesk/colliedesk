# Manage your team

**Settings → Team** lists everyone who can sign in to the workspace: name, email, roles, whether
they are **Active** or **Deactivated**, and when they were last active. Seeing the list needs
`team:read`. Renaming people, changing their roles, deactivating and reactivating them needs
`team:manage_roles` (the Admin role has it); without it the list is read-only. To add someone, see
[Invite your team](invites.md). What each role holds is in
[Roles and permissions](roles-and-permissions.md).

## Set a display name

A display name is what the team sees instead of the email address: in the team list, the assignee
lists, on notes, and in the account menu. The `{{agent.name}}` variable of a saved reply uses it too.

- **Your own name:** in **Settings → Profile**, type it in **Display name**, then select **Save
  name**.
- **Someone else's name** (`team:manage_roles`): select **Rename** in their row, type the name, then select
  **Save**. Use it to correct a typo or to name someone who has not signed in yet.

Spaces at the start and end are removed. A name has at most 80 characters and no line breaks. To
remove a name, clear the field and save: the email address is shown again. The email address stays
visible next to the name in the team list.

An AI agent can do the same through MCP: `colliedesk_my_display_name_set` (`profile:update`) for the
person the token acts as, and `colliedesk_team_member_update` with `display_name`
(`team:manage_roles`) for anyone in the workspace who does not hold more than the token's person.

## Change someone's roles

A person can hold several roles and gets every permission of each. Select **Change roles** in their
row, tick the roles they should hold, then select **Apply**. Nothing changes until you select
**Apply**, and then it applies to their next action at once, on every device. Everyone needs at
least one role.

- You can give or take only a role whose permissions you all hold; the others show "you can't give
  it". You cannot change someone who holds permissions you do not (their row says "Holds
  permissions you don't hold"); an Admin can always do it.
- Taking a role away also revokes their unused invites that would give it back, and stops every
  agent token of theirs that follows that role; the form names those tokens first
  (**Remove and stop 1 token**). Giving the role back starts the tokens again.
- A workspace always keeps at least one active person with the Admin role. Taking Admin from the
  last one, or deactivating them, is refused (`last_admin`: "A workspace needs at least one active member with the Admin role"); give
  Admin to someone else first. In **Change roles** their Admin box is locked: point at, focus or tap the lock beside it
  to read why.

An AI agent sets a person's roles with `colliedesk_member_roles_set` (`team:manage_roles`), by name or
id, under the same rules. See [Roles and permissions](roles-and-permissions.md#safety-rules).

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

If you deactivate yourself, you are signed out. You can only do that while another active person holds the Admin role.

## Reactivate someone

Select **Reactivate** in a deactivated row. On the hosted cloud this uses a seat again; if all seats
are taken you get the plan-limit message, and you free a seat first (deactivate someone or revoke an
invite). Reactivating does not give back their conversations or devices. Send them a new invite link,
or create a pairing link, so they can sign in. An invite to the email of a deactivated agent is
refused with "reactivate them in the team list".
