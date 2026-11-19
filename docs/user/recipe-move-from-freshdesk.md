# Recipe: move from Freshdesk and let an agent check the result

Import your Freshdesk account in the app, then ask an AI agent to read the import report: what
arrived, what did not, and which tickets to look at in Freshdesk before you close it. The agent
only reads; it cannot start, stop or undo an import.

## 1. Start a workspace

[Start a free trial of Colliedesk Cloud](https://app.colliedesk.com/sign-in?intent=trial&utm_source=colliedesk.com&utm_medium=docs&utm_campaign=cookbook-move-from-freshdesk),
or use your own server. You need the Admin role: imports are admin only.

## 2. Import in the app

The key of your Freshdesk account stays in the app this way; it never passes through a chat with an
agent. The full steps are in [Import from Freshdesk](import-freshdesk.md#import-in-the-app).

1. Open **Settings > Data > Import from Freshdesk**.
2. Enter your `<account>.freshdesk.com` address and the API key of a Freshdesk admin, then choose
   **Read account**. Colliedesk shows what it found. Nothing is written yet.
3. Choose **Start import** within the hour. The last 90 days come first; **Open the inbox** works
   from the first minute, and your team can answer while the rest follows.
4. At the end the page shows **In Freshdesk** next to **In Colliedesk** for each kind of data, with
   **Download report**.

## 3. Give the agent a read-only token

1. Open **Settings → AI & agents → External AI agents** and choose **Create token**.
2. Name it **Import check**.
3. Under **Access**, choose **Pick permissions (fixed)** and tick only `imports:read`.
4. Choose **Create token** and copy the token. It starts with `cdk_agent_` and is shown once.

Connect your host with this token ([Claude Code](connect-claude-code.md),
[Codex](connect-codex.md), [Cursor](connect-cursor.md), [VS Code](connect-vscode.md)). Revoke the
token when the check is done.

## 4. Ask the agent

```
Read the last Freshdesk import report in Colliedesk. Is it OK? List anything that did not arrive
and the Freshdesk ticket numbers I should check before I close Freshdesk.
```

The agent lists the imports (`colliedesk_imports_list`) and reads the report of the last one
(`colliedesk_import_report`). Its answer gives:

- the status of the report: `OK`, `FAILED`, `UNVERIFIED`, or `DRY_RUN` for a preview that wrote
  nothing;
- the counts in Freshdesk and in Colliedesk for tickets, replies, contacts, companies, attachments,
  saved replies and articles, and any that differ;
- the ticket numbers to check, for example archived tickets your Freshdesk plan does not let
  Colliedesk read.

It does not call `colliedesk_import_start` or `colliedesk_import_undo`: the token has no
`imports:run` or `imports:undo`, so such a call is refused with the permission it lacks.

## 5. Before you close Freshdesk

Do not close the Freshdesk account until the last report says `OK`. With `UNVERIFIED`, open the
tickets the report names in Freshdesk, or run the import again once the reason is fixed: finished
items are never copied twice. With `FAILED`, the report lists the Freshdesk ids that are missing;
run the import again and see [Import from Freshdesk](import-freshdesk.md).

## Let the agent run the import too

An Admin can give a token `imports:manage_credentials` and `imports:run` to let the agent preview
and start the import. Then the Freshdesk API key passes through the chat with the agent and is kept
by its host, so prefer the app. Leave `imports:undo` out of that token: an undo removes the imported
conversations at once, with the status, owner and tag changes your team made on them (a
conversation your team wrote in is kept). More: [Roles and permissions](roles-and-permissions.md).
