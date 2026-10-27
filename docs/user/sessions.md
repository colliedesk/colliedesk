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
shows **You are signed out** instead of the inbox. On instances with cloud sign-in it opens the
sign-in page instead and signs the browser out of the sign-in provider too, so the next sign-in
asks for a code again. To sign in again on a revoked device, ask an admin for a new link.

## Agent tokens

An agent token lets an AI agent, such as Claude Code, Codex or a script, work your inbox as you
with a limit you choose. It is a session with a label and a **scope**. It appears in
**Settings → Sessions** next to your browser sessions, with its scope as a badge and the date it
stops working. **Revoke** ends it at once, as for any session. Admins see and revoke the tokens of
every agent.

Create a token in **Settings → External AI agents**, which every agent sees: give it a name you will
recognise in Sessions (1 to 80 characters, for example **Claude Code on my laptop**), pick its scope
(**Read**, **Draft**, **Send** or **Admin**; **Draft** is selected) and choose **Create token**. Only an
admin can pick **Admin**; for an agent it is shown but cannot be chosen. The token is shown once,
with **Copy token**. Choose **I saved it, hide the token** when you have it; leaving the page hides it too,
and Colliedesk cannot show it again. If you lose it, create a new token and revoke the old one. The new
token appears in Sessions at once. Below it, **Connect your agent** has the steps for Claude Code,
Codex, Cursor, VS Code and Claude Desktop with your address filled in, and the way to remove each
one; while the token is shown, **Add to Cursor** puts it into Cursor in one click. To check that an
agent is connected, ask it "Who am I in Colliedesk?".

To give a token exactly what it needs, choose **Show its permissions**: every part of Colliedesk is
a row (conversations, messages, the knowledge base, the team…) with a chip for each action (view,
create, edit, delete, and actions such as **Send to customers** or **Publish**). Choosing a tier ticks
its permissions; ticking or unticking one makes the token **Custom**, with exactly what is ticked. An
orange dot marks what reaches outside the workspace (customers, the public help center, another
service); a ring marks what always waits for a person. Only an admin can give an admin-only
permission. In Sessions a custom token shows how many permissions it holds; open it to see them.

A program can create a token with `POST /api/v1/auth/tokens` and a body such as
`{"label": "Claude Code", "scope": "draft"}`, or `{"label": "KB writer", "permissions":
["kb_articles:read", "kb_articles:create", "kb_articles:update"]}` for a custom token. Each permission
is `resource:action`; the table below lists them by the tier that holds them. On your own server you can also use the
[command line](../self-host/troubleshooting.md#create-an-agent-token-from-the-command-line). The agent
sends the token as `Authorization: Bearer <token>`.

| Scope | The agent can |
|---|---|
| `read` | Read conversations, messages, contacts, tags, saved replies and the team list, and see the session and invite lists and the email channel and web form settings. No answer ever holds an invite link, a token or the inbound email secret. |
| `draft` | Everything in `read`, plus change status, priority, assignee and tags of one conversation or several at once, create a tag, add private notes, and set your own timezone. Nothing reaches a customer. |
| `send` | Everything in `draft`, plus send replies to customers, retry a failed reply, rename and delete tags, create contacts and conversations, and create, edit, delete and restore [saved replies](saved-replies.md#ai-agents) |
| `admin` | Everything in `send`, plus change the workspace name and timezone, deactivate and reactivate agents, revoke invites, set up the web form and the email channel, verify the email domain (10 times an hour), read the plan, revoke agent tokens, and sign out your own devices. Only an admin can create it. |

Start with `read` or `draft`. Give `admin` only to an agent you trust to run the workspace.

| Tier | Permissions it holds (and every lower tier's) |
|---|---|
| `read` | `conversations:read`, `messages:read`, `ai_assist:read`, `contacts:read`, `tags:read`, `saved_replies:read`, `saved_views:read`, `kb_articles:read`, `kb_media:read`, `help_center:read`, `results:read`, `agent_requests:read`, `agent_requests:cancel`, `team:read`, `channels:read`, `ai_settings:read`, `autofix:read`, `autofix_settings:read` |
| `draft` | `conversations:update`, `messages:note`, `ai_assist:create`, `tags:create`, `saved_views:create`, `saved_views:update`, `saved_views:delete`, `profile:update` |
| `send` | `messages:send`, `contacts:create`, `contacts:update`, `tags:update`, `tags:delete`, `saved_replies:create`, `saved_replies:update`, `saved_replies:delete`, `kb_articles:create`, `kb_articles:update`, `kb_articles:delete`, `kb_articles:publish`, `kb_media:create`, `kb_media:update`, `kb_media:delete` |
| `admin` | `kb_media:purge`, `help_center:update`, `results:share`, `team:invite`, `team:revoke`, `team:manage_roles`, `team:create_tokens`, `channels:update`, `channels:manage_credentials`, `workspace:update`, `workspace:export`, `billing:read`, `billing:manage`, `imports:read`, `imports:run`, `imports:manage_credentials`, `imports:undo`, `ai_settings:update`, `ai_settings:manage_credentials`, `autofix_settings:update` |

A tier is worked out on every call, so it keeps what its table row says as Colliedesk grows. A custom
token holds only its own list. `messages:note` writes private notes; `messages:send` reaches customers.
`kb_articles:publish` is needed for anything that changes the public help center: creating or making
an article published, editing, trashing or restoring a published one, replacing an image it shows, and
renaming, moving or deleting a collection. Without it, those changes wait for a person. Accepting or
undoing an AI triage suggestion also needs `conversations:update`. A token with an admin-only
permission acts with your role, like an `admin` token.

An `admin` token acts with your role, checked on every call. If you are demoted to agent, the token
keeps working with the agent role only; if you are deactivated, it stops working. A `read`,
`draft` or `send` token always has the agent role, even when an admin creates it. An agent that
asks for an `admin` token files a request; it is created only when an admin approves it.

These actions are always requests, for every tier including `admin`: creating an invite (the
person who approves sees its link once; no token ever does), making someone an admin, any change to
an admin (demoting, deactivating, reactivating), and signing out another person's device. When an
agent token revokes a session, the session list shows it as **Revoked** and the server log names
the token that did it.

A call the token holds no permission for is not refused: it becomes an [agent request](agent-requests.md) that a
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
