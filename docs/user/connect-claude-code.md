# Connect Claude Code

Connect [Claude Code](https://code.claude.com) to Colliedesk. Claude Code signs in with OAuth: you
allow it in your browser and choose the workspace, with no token to create or paste. The Colliedesk
plugin adds the Colliedesk MCP server of Colliedesk Cloud and the [agent skills](agent-skills.md) in
one install.

## Connect to Colliedesk Cloud

1. Install the plugin:

   ```sh
   claude plugin install colliedesk --marketplace colliedesk/colliedesk
   ```

2. Start `claude`, run `/mcp`, choose `plugin:colliedesk:colliedesk` and **Authenticate**. From your shell
   instead:

   ```sh
   claude mcp login plugin:colliedesk:colliedesk
   ```

3. Your browser opens Colliedesk. Sign in if it asks, choose the workspace and choose **Allow**.

`/mcp` lists `plugin:colliedesk:colliedesk` as connected, and **Settings → External AI agents → Connected AI
apps** in Colliedesk lists Claude Code.

## Connect to a server you run yourself

The plugin's server is Colliedesk Cloud, `https://app.colliedesk.com/mcp`. For your own server, add its address
(`https://desk.example.com` stands for it) and sign in:

```sh
claude mcp add --transport http colliedesk https://desk.example.com/mcp
claude mcp login colliedesk
```

Add `--scope user` to the first command to use it in every project. To get the agent skills too,
install the plugin as above, then in `/mcp` choose `plugin:colliedesk:colliedesk` and turn it off, so Claude Code
does not ask you to sign in to Cloud. Claude Code remembers that per project.

## Connect again

When `/mcp` shows `colliedesk` or `plugin:colliedesk:colliedesk` as needing authentication, the connection
was disconnected in Settings, went 30 days unused, or your account was deactivated. Choose it in
`/mcp` and **Authenticate** again. To use another workspace, run `claude mcp logout <name>`, then
`claude mcp login <name>` and choose that workspace.

## Try it

Ask Claude Code: "Who am I in Colliedesk?" It calls `colliedesk_whoami`, which answers
with your name, your workspace, your roles, the token's permissions and the tools it may use. Then, for example: "Triage the
open conversations nobody owns: tag billing questions with `billing` and leave a note with a
summary."

## What the agent can do

Colliedesk has 246 tools. Claude Code acts as you, with everything you can do in the workspace
you allowed, read from your roles on every call: a permission you lose stops it at once. A tool you
have no permission for is refused with the permission it lacks. See
[Roles and permissions](roles-and-permissions.md) and [Connected AI apps](sessions.md#connected-ai-apps).

Every tool and what it needs: [Connect an AI agent (MCP)](mcp.md#tools).

## Remove it

- Plugin: `claude plugin uninstall colliedesk@colliedesk`
- Your own server: `claude mcp remove colliedesk`

Removing it in Claude Code does not disconnect it in Colliedesk: choose **Disconnect** next to it in
**Settings → External AI agents → Connected AI apps**.
## Other hosts

- [Codex](connect-codex.md)
- [Cursor](connect-cursor.md)
- [VS Code](connect-vscode.md)
- [Claude Desktop](connect-claude-desktop.md)
- [Agent skills](agent-skills.md): triage, draft replies and chase pending conversations

