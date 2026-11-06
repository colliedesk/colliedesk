# Invite your team

Invite people from **Settings → Invites** (it needs `team:invite`, which the Admin role holds).

1. Enter the person's email in **Invite email**.
2. Choose their **Roles** (**Agent** is selected). You can pick several; you can give only roles
   whose permissions you all hold.
3. Select **Create invite** and copy the **One-time invite link**. It is shown only once.
4. Send the link to the person through a channel you trust.

When the person joins, they get the invite's roles. See
[Roles and permissions](roles-and-permissions.md) for what each role allows. An invite is for someone
new: inviting the address of someone who is already in the workspace is refused with
`already_a_member`. Change their roles in [Manage your team](team.md) instead. An invite link made
before its address joined the workspace no longer signs anyone in. Revoke an unused invite
with **Revoke** in the invite list (`team:revoke`). Taking a role away from someone also revokes
their unused invites that would give it back.

Each invite shows its state: **Pending** (not used yet), **Accepted**, **Revoked** or **Expired**, and
its roles. Only a pending invite can be revoked. To end a teammate's access after they joined,
deactivate them in [Manage your team](team.md).

An AI agent whose [token](sessions.md#agent-tokens) holds `team:invite` creates an invite at once with
`colliedesk_invite_create`, naming the roles by name or id, and gets the one-time link back to give
to you; nothing waits for approval. It lists invites with `colliedesk_invites_list` (`team:read`) and
revokes one with `colliedesk_invite_revoke` (`team:revoke`). A token created before roles cannot
create invites; create a new token for it.

## Agent limit on the hosted cloud

On the hosted cloud, an active agent and a pending invite each take one seat; a [deactivated](team.md)
agent takes none. The trial and Team 25 have 25 seats; Team and Founding have 10. Creating an invite
when every seat is taken fails with a message that names the limit.
Revoke an invite you no longer need. See [Plan and billing](billing.md). A locked workspace (trial or
subscription ended) cannot create or accept invites until it has a plan.
