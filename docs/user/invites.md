# Invite your team

Invite people from **Settings → Members → Invites** (it needs `team:invite`, which the Admin role holds).

1. Enter the person's email in **Invite email**.
2. Choose their **Roles** (**Agent** is selected). You can pick several; you can give only roles
   whose permissions you all hold.
3. Select **Create invite** and copy the **One-time invite link**. It is shown only once.
4. Send the link to the person through a channel you trust.

When the person joins, they get the invite's roles, added to any roles they already hold. See
[Roles and permissions](roles-and-permissions.md) for what each role allows. Revoke an unused invite
with **Revoke** in the invite list (`team:revoke`). Taking a role away from someone also revokes
their unused invites that would give it back.

Inviting someone who is already in the workspace adds the invite's roles and signs in as them, so
only someone who may change their roles can do it: you need `team:manage_roles` and every permission
they hold. An AI agent's token cannot; it changes their roles in [Manage your team](team.md) instead.
An invite link made before its address joined the workspace no longer signs anyone in.

Each invite shows its state: **Pending** (not used yet), **Accepted**, **Revoked** or **Expired**, and
its roles. Only a pending invite can be revoked. To end a teammate's access after they joined,
deactivate them in [Manage your team](team.md).

An AI agent whose [token](sessions.md#agent-tokens) holds `team:invite` creates an invite at once with
`colliedesk_invite_create`, naming the roles by name or id, and gets the one-time link back to give
to you; nothing waits for approval. It lists invites with `colliedesk_invites_list` (`team:read`) and
revokes one with `colliedesk_invite_revoke` (`team:revoke`). A token created before roles cannot
create invites; create a new token for it.

## What the invited person sees

Opening the link shows **Join** and the workspace's name, who invited them, the email the invite is
for, its roles and when it expires, before anything is used. **Join <workspace>** signs that browser
in for 30 days and opens the inbox. Next time, on the hosted cloud, they sign in with a code sent to
that email; on a server without cloud sign-in, they ask for a new link.

When the link no longer works, the page says why and whom to ask:

- **Expired**: the date it expired; ask the person who invited you for a new one.
- **Revoked**: the invite was cancelled; ask the person who invited you if you still need access.
- **Already used**: the person already joined; **Sign in** instead.

If the workspace has no free seat (hosted cloud), the page says so, names who invited you, and says
the link keeps working until it expires: once a seat is free, **Try again** joins. A link that is not
an invite (a pairing link, or a mistyped one) shows the ordinary sign-in link page.

Looking at an invite never uses it up. An AI agent or script can read the same facts with
`POST /api/v1/auth/invite-preview` and the token; it needs no session. A client address that tries 30
tokens no link has within a minute must wait a minute before it opens or previews another link.

## Agent limit on the hosted cloud

On the hosted cloud, an active agent and a pending invite each take one seat; a [deactivated](team.md)
agent takes none. The trial and Team 25 have 25 seats; Team and Founding have 10. Creating an invite
when every seat is taken fails with a message that names the limit.
Revoke an invite you no longer need. See [Plan and billing](billing.md). A locked workspace (trial or
subscription ended) cannot create or accept invites until it has a plan.
