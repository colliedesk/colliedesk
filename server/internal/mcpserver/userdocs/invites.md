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

An AI agent can ask for an invite with any [agent token](sessions.md#agent-tokens). The invite is
created only when a person approves it in [Requests](agent-requests.md), and the person who
approves sees the link once and sends it; the agent never sees it. An agent with an `admin` token
can revoke invites directly.

Roles:

| Role | Can do |
|---|---|
| Agent | Work conversations, contacts and tags; manage own sessions and timezone |
| Admin | Everything an agent can, plus invites, all sessions, the workspace name and the workspace timezone |

## Agent limit on the hosted cloud

On the hosted cloud, an active agent and a pending invite each take one seat; a [deactivated](team.md)
agent takes none. The trial and Team 25 have 25 seats; Team and Founding have 10. Creating an invite
when every seat is taken fails with a message that names the limit.
Revoke an invite you no longer need. See [Plan and billing](billing.md). A locked workspace (trial or
subscription ended) cannot create or accept invites until it has a plan.
