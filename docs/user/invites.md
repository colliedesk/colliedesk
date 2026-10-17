# Invite your team

Admins invite people from **Settings → Invites**.

1. Enter the person's email in **Invite email**.
2. Choose the **Role**: **Agent** or **Admin**.
3. Select **Create invite** and copy the **One-time invite link**. It is shown only once.
4. Send the link to the person through a channel you trust.

The role is a maximum: an invite can never give more access than the role chosen, even if
the person already has a higher role. Revoke an unused invite with **Revoke** in the invite list.

Each invite shows its state: **Pending** (not used yet), **Accepted**, **Revoked** or **Expired**.
Only a pending invite can be revoked. To end a teammate's access after they joined, deactivate them in [Manage your team](team.md).

An AI agent with an `admin` [agent token](sessions.md#agent-tokens) can create and revoke invites
with the **Agent** role. It never sees the link: the invite appears in the list, and an admin
shares a link. An invite with the **Admin** role from any agent waits for an admin to approve it in
[Requests](agent-requests.md); the person who approves sees the link once.

Roles:

| Role | Can do |
|---|---|
| Agent | Work conversations, contacts and tags; manage own sessions and timezone |
| Admin | Everything an agent can, plus invites, all sessions and the workspace timezone |

## Agent limit on the hosted cloud

On the hosted cloud, during the trial and on the Team and Founding plans, an active agent and a pending
invite each take one of 10 seats; a [deactivated](team.md) agent takes none. Creating an eleventh fails with a message that names the limit.
Revoke an invite you no longer need. See [Plan and billing](billing.md). A locked workspace (trial or
subscription ended) cannot create or accept invites until it has a plan.
