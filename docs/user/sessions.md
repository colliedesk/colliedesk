# Sessions and devices

**Settings → Account → Sessions** lists every browser or client signed in to your account, with when it
was last used. A browser is named by its browser and system, for example **Chrome on Linux**; the
browser you are using is marked **This device**. This applies to pairing links and cloud sign-in alike;
a client that sends no browser name shows **Browser session** or **Cloud sign-in**.

- Select **Revoke** to sign a device out immediately. The device stays in the list as **Revoked**;
  a session that ran out shows **Expired**.
- On **This device** the button reads **Sign out**: it signs this browser out at once.
- To sign out of the browser you are using, you can also select your avatar at the foot of the left rail (top
  right on a phone) and choose **Sign out**. The menu shows your name and roles; the workspace and your address
  are in the bar above every page.
- You see your own sessions. Someone with `team:revoke` (the Admin role has it) sees every session in
  the workspace and can revoke those of anyone who does not hold more than they do.
- A session signed in through the API with `requested_role: agent` shows **Limited to Agent**: it
  acts with the Agent role only, even if you hold more. Any other sign-in acts with all your roles.

A session lasts 30 days and extends while you use Colliedesk. A browser without a valid session
shows **You are signed out** instead of the inbox. On instances with cloud sign-in it opens the
sign-in page instead and signs the browser out of the sign-in provider too, so the next sign-in
asks for a code again. To sign in again on a revoked device, ask an admin for a new link.

## Connected AI apps

An AI app you connect with OAuth (see [Connect an app with OAuth](mcp.md#connect-an-app-with-oauth))
acts as you in the workspace you allowed it in, with everything you can do there, as your roles are on
each call: a permission taken from you is taken from the app at once, and given back, it works again
without connecting again. **Settings → Account → Connected AI apps** lists each connection:

- the app's name and the browser you allowed it from (no IP address is kept);
- when you connected it and when it was last used;
- **Active**, or **Needs reconnecting** when it was disconnected, ran out after 30 days without use, or
  your account was deactivated, with how to connect again in each app;
- **Disconnect**, which stops it at once: its next call gets HTTP 401.

Each sign-in is its own row, so the same app on two computers shows twice. Claude and ChatGPT call from
their own servers, so their row is one per account, not per computer. Someone with `team:revoke` sees
and disconnects the apps of every person they do not rank below. Deactivating a person disconnects
their apps; after they are reactivated they connect again.

Allowing an app is something only a person does in a browser: an app or an agent token cannot allow
another app (HTTP 403 `person_only`). A note or a reply an app writes shows "by <your name> via <app
name>" in the conversation.

## Agent tokens

An agent token lets an AI agent, such as Claude Code, Codex or a script, work your inbox as you,
with the permissions you choose. It is a session with a label. **Settings → AI & agents → External AI agents**
lists your tokens under **Agent tokens**, apart from your browser sessions (in Account): each row has the token's
name and person, what it may do, **Last used** (when an agent last called with it) and **Expires** (the date it
stops working). **Sessions**, further down the same page, lists them too. Someone with `team:revoke` sees and
revokes the tokens of every person they do not rank below; AI & agents is in their settings menu even when they
may not create tokens or read the built-in AI.

To revoke a token, open the **⋯** menu at the end of its row and choose **Revoke** (in Sessions, its **Revoke**
button). Colliedesk asks first, naming the token: revoking ends it at once, the agent that uses it is refused on
its next call, and notes and replies it wrote keep showing "by <your name> via <token label>". A revoked
token cannot be brought back; its row then reads **Revoked** and has no actions.

Create a token in **Settings → AI & agents → External AI agents**: choose **Create token** (it needs
`team:create_tokens`, which the Agent and Admin roles hold). In the dialog, give it a name you will recognise in
the list (1 to 80 characters, for
example **Claude Code on my laptop**), then under **Access** choose one of:

- **Follow a role**: pick any role whose permissions you all hold: your own roles, a narrower one,
  and for an Admin every role (for example Agent, so the token cannot export or change roles). The
  token gets that role's permissions as they are on each call: when someone changes the role, the
  token changes with it. If you stop holding every permission of the role (it is taken from you, or
  it gains one you lack), the token stops until you hold them again. A role that can export data,
  change roles or reach outside the workspace says so; pick permissions to leave some out.
- **Pick permissions (fixed)**: tick the permissions the token gets, out of the ones you hold. The
  list is fixed when you create the token. An orange dot marks what reaches outside the workspace (a
  customer, the public help center, another service).

When the token may not **Send to customers** but may leave AI drafts, the dialog says so: the agent leaves
reply drafts above the reply box for a person to send. With Send to customers on, the agent's replies reach
customers at once; nothing waits for approval.

Either way, a token never does more than you can do right now: if you lose a permission, every
token of yours loses it too, and if you are deactivated, your tokens stop working. See
[Roles and permissions](roles-and-permissions.md) for the list of permissions.

Choose **Create token** in the dialog. The token is shown once, with **Copy token**. Choose **I saved it,
hide the token** when you have it; closing the dialog or leaving the page hides it too, and Colliedesk cannot
show it again. If you lose it, create a new token and revoke the old one. The new token appears in the list at
once. Under the token, and under the list on the page, **Connect your agent** has the steps for Claude Code,
Codex, Cursor, VS Code and Claude Desktop
with your address filled in, and the way to remove each one; while the token is shown, **Add to
Cursor** puts it into Cursor in one click. To check that an agent is connected, ask it "Who am I in
Colliedesk?": it answers with your name, your roles and what the token may do.

In the list and in Sessions a token shows one of:

- **Follows role Agent** (or the role it follows); open it to see **What it may do**.
- **Fixed · 5 permissions**: a token made with a list.
- **Stopped: Long Bui no longer holds all of Support lead**: giving them the role starts it again.
- **Created before roles · was Send**: a token made with the old tiers. It keeps the permissions it
  had, except those that used to wait for a person's approval; create a new token for those
  ([which ones](roles-and-permissions.md#tokens-created-before-roles)).

Creating a token is something only a person does: an AI agent's token cannot create another one
(HTTP 403 `person_only`), because a token that made tokens would leave them behind after it was
revoked. A program signed in as a person can create one with `POST /api/v1/auth/tokens` and a body
such as `{"label": "Claude Code", "role_id": "<id of a role whose permissions you hold>"}`, or `{"label": "KB writer",
"permissions": ["kb_articles:read", "kb_articles:create", "kb_articles:update"]}` for a fixed list.
Each permission is `resource:action`. The old `scope` field is refused. On your own server you can
also use the [command line](../self-host/troubleshooting.md#create-an-agent-token-from-the-command-line).
The agent sends the token as `Authorization: Bearer <token>`.

An AI agent does what its token's permissions allow, at once; nothing waits for a person to approve
it. A call without the permission it needs is refused with HTTP 403 `permission_missing`, naming the
permission and whether the token or you lack it
([details](roles-and-permissions.md#when-a-permission-is-missing)). A reply the token may not send
can be left as a draft above the reply box (`ai_assist:create`) for a person to send. When an agent
token revokes a session, the session list shows it as **Revoked** and the server log names the
token that did it.

A token starts with `cdk_agent_` so that secret scanners recognise it if it ends up in a file by
mistake. It works only in the `Authorization` header. A browser cookie that holds a token is
refused. A token stops working 30 days after its last use, and in any case one year after you
created it, however often it is used. Create a new token before that and revoke the old one.

A note or a reply that an agent writes shows "by <your name> via <token label>" in the
conversation, also after you revoked the token. Status, priority, assignee and tag changes do not
show it yet.
