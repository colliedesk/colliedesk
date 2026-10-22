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

Create a token with `POST /api/v1/auth/tokens` and a body such as
`{"label": "Claude Code", "scope": "draft"}`. On your own server you can also use the
[command line](../self-host/troubleshooting.md#create-an-agent-token-from-the-command-line). Settings has no
button for it yet. The token is shown once and cannot be recovered. If you lose
it, create a new token and revoke the old one. The agent sends it as `Authorization: Bearer <token>`.

| Scope | The agent can |
|---|---|
| `read` | Read conversations, messages, contacts, tags, saved replies and the team list, and see the session and invite lists and the email channel and web form settings. No answer ever holds an invite link, a token or the inbound email secret. |
| `draft` | Everything in `read`, plus change status, priority, assignee and tags of one conversation or several at once, create a tag, add private notes, and set your own timezone. Nothing reaches a customer. |
| `send` | Everything in `draft`, plus send replies to customers, retry a failed reply, rename and delete tags, create contacts and conversations, and create, edit, delete and restore [saved replies](saved-replies.md#ai-agents) |
| `admin` | Everything in `send`, plus change the workspace name and timezone, deactivate and reactivate agents, revoke invites, set up the web form and the email channel, verify the email domain (10 times an hour), read the plan, revoke agent tokens, and sign out your own devices. Only an admin can create it. |

Start with `read` or `draft`. Give `admin` only to an agent you trust to run the workspace.

An `admin` token acts with your role, checked on every call. If you are demoted to agent, the token
keeps working with the agent role only; if you are deactivated, it stops working. A `read`,
`draft` or `send` token always has the agent role, even when an admin creates it. An agent that
asks for an `admin` token files a request; it is created only when an admin approves it.

These actions are always requests, for every tier including `admin`: creating an invite (the
person who approves sees its link once; no token ever does), making someone an admin, any change to
an admin (demoting, deactivating, reactivating), and signing out another person's device. When an
agent token revokes a session, the session list shows it as **Revoked** and the server log names
the token that did it.

A call above the scope is not refused: it becomes an [agent request](agent-requests.md) that a
person approves or rejects, and nothing changes until then (HTTP 202). A reply from a token below
`send` becomes a reply draft a person sends. Creating tokens, invites, any change to an admin or that makes
one, signing out another person, export, billing checkout and portal, disconnecting the email channel, rotating its secret and
changing the workspace address always wait for a person, whatever the scope, `admin` included.
Approving or rejecting a request is for people only.

A token starts with `cdk_agent_` so that secret scanners recognise it if it ends up in a file by
mistake. It works only in the `Authorization` header. A browser cookie that holds a token is
refused. A token stops working 30 days after its last use, and in any case one year after you
created it, however often it is used. Create a new token before that and revoke the old one.

A note or a reply that an agent writes shows "by <your name> via <token label>" in the
conversation, also after you revoked the token. Status, priority, assignee and tag changes do not
show it yet.
