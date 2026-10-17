# Sessions and devices

**Settings → Sessions** lists every browser or client signed in to your account, with when it
was last used. A browser is named by its browser and system, for example **Chrome on Linux**; the
browser you are using is marked **This device**. This applies to pairing links and cloud sign-in alike;
a client that sends no browser name shows **Browser session** or **Cloud sign-in**.

- Select **Revoke** to sign a device out immediately. The device stays in the list as **Revoked**;
  a session that ran out shows **Expired**.
- On **This device** the button reads **Sign out**: it signs this browser out at once.
- To sign out of the browser you are using, you can also select your avatar at the foot of the left rail (top
  right on a phone) and choose **Sign out**.
- Agents see their own sessions. Admins see and can revoke every session in the workspace.

A session lasts 30 days and extends while you use Colliedesk. A browser without a valid session
shows **You are signed out** instead of the inbox; on instances with cloud sign-in it links to **Sign in**. To sign in again on a revoked device, ask an
admin for a new link.

## Agent tokens

An agent token lets an AI agent, such as Claude Code, Codex or a script, work your inbox as you
with a limit you choose. It is a session with a label and a **scope**. It appears in
**Settings → Sessions** next to your browser sessions, with its scope as a badge and the date it
stops working. **Revoke** ends it at once, as for any session. Admins see and revoke the tokens of
every agent.

Create a token with the command line on your own server:

```sh
docker compose exec server /app/collie token create \
  --workspace <slug> --email you@example.com --scope draft --label "Claude Code"
```

or with `POST /api/v1/auth/tokens` and a body such as `{"label": "Claude Code", "scope": "draft"}`.
Settings has no button for it yet. The token is shown once and cannot be recovered. If you lose
it, create a new token and revoke the old one. The agent sends it as `Authorization: Bearer <token>`.

| Scope | The agent can |
|---|---|
| `read` | Read conversations, messages, contacts, tags, saved replies and the team list |
| `draft` | Everything in `read`, plus change status, priority, assignee and tags of one conversation or several at once, create a tag, and add private notes. Nothing reaches a customer. |
| `send` | Everything in `draft`, plus send replies to customers, retry a failed reply, rename and delete tags, and create contacts and conversations |

Start with `read` or `draft`.

Whatever the scope, a token cannot manage sessions, tokens or invites, and cannot change
workspace, email channel, web form, billing or export settings. It cannot read the lists of
sessions and invites or the email and web form settings either. This holds when an admin creates
the token: it has the agent role. A call outside the scope fails with HTTP 403 and the error code
`scope_insufficient`. The message names the scope the action needs.

A token starts with `cdk_agent_` so that secret scanners recognise it if it ends up in a file by
mistake. It works only in the `Authorization` header. A browser cookie that holds a token is
refused. A token stops working 30 days after its last use, and in any case one year after you
created it, however often it is used. Create a new token before that and revoke the old one.

A note or a reply that an agent writes shows "by <your name> via <token label>" in the
conversation, also after you revoked the token. Status, priority, assignee and tag changes do not
show it yet.
