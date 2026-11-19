# Recipe: the agent drafts, a person sends

An AI agent writes the replies; a person on your team reads each one, edits it if needed and sends
it. The agent's token cannot send, so nothing reaches a customer without a person. This is a safe
first step before you let an agent answer on its own.

It works with any host: [Claude Code](connect-claude-code.md), [Codex](connect-codex.md),
[Cursor](connect-cursor.md), [VS Code](connect-vscode.md) or
[Claude Desktop](connect-claude-desktop.md).

## What the agent may do

1. Open **Settings → AI & agents → External AI agents** and choose **Create token**.
2. Name it **Reply drafts**.
3. Under **Access**, choose **Pick permissions (fixed)** and tick only:
   `conversations:read`, `messages:read`, `ai_assist:create`, `kb_articles:read`,
   `saved_replies:read`, `contacts:read`, `tags:read`, `saved_views:read` and `team:read`.
   Leave **Send to customers** (`messages:send`) off.
4. The dialog says that the agent leaves reply drafts above the reply box for a person to send.
   Choose **Create token** and copy the token. It starts with `cdk_agent_` and is shown once.

Connect your host with this token, as its page says. With the Colliedesk plugin installed, turn the
plugin's own server off for this work (in Claude Code: `/mcp`), because it acts with all your
permissions, including sending.

## Ask for drafts

Install the [agent skills](agent-skills.md), then ask, for example:

```
Use colliedesk-draft-reply on the open conversations about refunds. Leave each reply as a draft.
```

For each conversation the agent reads the thread, your saved replies, help articles and solved
conversations like it, and leaves a draft (`colliedesk_reply_draft`). It ends with each draft and
where its content came from, and names what a person must fill in, such as an order number it did
not know.

## Check and send

1. Open the inbox with the filter for drafts that wait. On Colliedesk Cloud:
   [app.colliedesk.com/?has_pending_draft=true](https://app.colliedesk.com/?has_pending_draft=true).
   The chip reads **Reply draft waiting to be sent**.
2. Open a conversation. The draft shows above the reply box with the name of the token.
3. Edit it, then send it, or discard it. A sent draft is a reply like any other: if someone replied
   after the draft was written, Colliedesk shows that reply and asks before it sends yours.

The sent reply shows "by <your name> via Reply drafts", so the team can see later which replies an
agent wrote ([Inbox overview](inbox.md)).

## What happens when the agent tries to send

The token has no `messages:send`, so a send is refused with `permission_missing`. The message
names the permission and who holds it, and the agent tells you instead of trying again:

```
This agent token was not given messages:send. Lan holds it: create a token that includes it in
Settings → External AI agents, then use that token.
```

Ask the agent "Who am I in Colliedesk?" to see the token's permissions at any time.

## When to let it send

Keep this recipe until the drafts need few edits. Count them for a week: open the drafts filter
each day and note how many you sent unchanged. To let the agent send, create a second token with
**Send to customers** for the conversations you trust it with; its replies then reach customers at
once, with nothing waiting for approval ([Agent tokens](sessions.md#agent-tokens)).

## Try it

No workspace yet? [Start a free trial of Colliedesk Cloud](https://app.colliedesk.com/sign-in?intent=trial&utm_source=colliedesk.com&utm_medium=docs&utm_campaign=cookbook-draft-then-approve),
add a few [saved replies](saved-replies.md) and help articles, and ask for drafts on the first
conversations that come in.
