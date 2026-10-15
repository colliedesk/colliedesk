# Sessions and devices

**Settings → Sessions** lists every browser or client signed in to your account, with when it
was created and last used.

- Select **Revoke** to sign a device out immediately. The device stays in the list as **Revoked**;
  a session that ran out shows **Expired**.
- Agents see their own sessions. Admins see and can revoke every session in the workspace.

A session lasts 30 days and extends while you use Colliedesk. A browser without a valid session
shows **You are signed out** instead of the inbox; on instances with cloud sign-in it links to **Sign in**. To sign in again on a revoked device, ask an
admin for a new link.

## Agent tokens

An agent token lets an AI agent, such as Claude Code, Codex or a script, work your inbox as you
with a limit you choose. It is a session with a label and a **scope**. It appears in
**Settings → Sessions** next to your browser sessions. **Revoke** ends it at once, as for any
session. Admins see and revoke the tokens of every agent.

Create a token with the command line on your own server:

```sh
docker compose exec server /app/colliedesk token create \
  --workspace <slug> --email you@example.com --scope draft --label "Claude Code"
```

or with `POST /api/v1/auth/tokens` and a body such as `{"label": "Claude Code", "scope": "draft"}`.
Settings has no button for it yet. The token is shown once and cannot be recovered. If you lose
it, create a new token and revoke the old one. The agent sends it as `Authorization: Bearer <token>`.

| Scope | The agent can |
|---|---|
| `read` | Read conversations, messages, contacts, tags and agents |
| `draft` | Everything in `read`, plus change status, priority, assignee and tags of a conversation and add private notes. Nothing reaches a customer. |
| `send` | Everything in `draft`, plus send replies to customers, retry a failed reply, and create, rename or delete contacts, conversations and tags |

Start with `read` or `draft`. A tag must exist before a `draft` agent can put it on a conversation.

Whatever the scope, a token cannot manage sessions, tokens or invites, and cannot change
workspace, email channel, web form, billing or export settings. This holds when an admin creates
the token: it has the agent role. A call outside the scope fails with HTTP 403 and the error code
`scope_insufficient`. The message names the scope the action needs.

A token works only in the `Authorization` header. A browser cookie that holds a token is
refused. A token lasts 30 days and extends while the agent uses it.
