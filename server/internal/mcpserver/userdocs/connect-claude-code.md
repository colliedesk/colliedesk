# Connect Claude Code

Connect [Claude Code](https://code.claude.com) to Colliedesk with the Colliedesk plugin: one
install adds the Colliedesk MCP server and the [agent skills](agent-skills.md). Claude Code keeps
the token in your system's credential store, not in a settings file.

## Before you start

1. Sign in to Colliedesk and open **Settings → External AI agents**.
2. Name the token (for example "Claude Code on my laptop"), choose what it may do (follow a role,
   or pick permissions; leaving out sending to customers is a good start) and choose **Create token**.
   See [Agent tokens](sessions.md#agent-tokens).
3. Copy the token. It starts with `cdk_agent_` and is shown once.

The steps below use `https://desk.example.com`: put the address you open Colliedesk at in its place.
Under **Connect your agent**, Settings shows these steps for Claude Code with your address already in.

## Connect

1. Install the plugin with your address:

   ```sh
   claude plugin install colliedesk --marketplace colliedesk/colliedesk --config url=https://desk.example.com/mcp
   ```

2. Start `claude` and give it the token: run

   ```text
   /plugin configure colliedesk@colliedesk
   ```

   and paste the token when it asks. Restart Claude Code.

`/mcp` in Claude Code lists `colliedesk` as connected.

### Change the token

When `/mcp` shows `colliedesk` as failed with HTTP 401, the token was revoked or has expired.
Create a new one in **Settings → External AI agents** and give it to Claude Code:

```text
/colliedesk-token <your token>
```

Colliedesk checks the new token first and Claude Code saves it only when Colliedesk accepts it, so a
mistyped token never replaces a working one. Then run `/reload-plugins` to reconnect. What you type
stays in the session's history; `/plugin configure colliedesk@colliedesk` asks for the token without
showing it.

### Without the plugin

The MCP server alone, with the token in an environment variable. Put the `export` line in your
shell profile (`~/.zshrc` or `~/.bashrc`) so every new terminal has it:

```sh
export COLLIEDESK_TOKEN=<your token>
claude mcp add --transport http colliedesk https://desk.example.com/mcp --header 'Authorization: Bearer ${COLLIEDESK_TOKEN}'
```

Keep the single quotes: Claude Code then stores `${COLLIEDESK_TOKEN}` and reads the variable each time
it starts, so the token is not written into `~/.claude.json`. Add `--scope user` to use it in every
project.

## Try it

Ask Claude Code: "Who am I in Colliedesk?" It calls `colliedesk_whoami`, which answers
with your name, your workspace, your roles, the token's permissions and the tools it may use. Then, for example: "Triage the
open conversations nobody owns: tag billing questions with `billing` and leave a note with a
summary."

## What the agent can do

Colliedesk has 243 tools. The agent acts as you, with the permissions you gave the token,
never more than you hold now. A tool it has the permissions for runs at once; any other is refused
with the permission it lacks. See [Roles and permissions](roles-and-permissions.md).

Every tool and what it needs: [Connect an AI agent (MCP)](mcp.md#tools).

## Remove it

- Plugin: `claude plugin uninstall colliedesk@colliedesk`
- Without the plugin: `claude mcp remove colliedesk`

Then revoke the token in **Settings → Sessions**: it stops working at once, wherever it was
copied.

## Other hosts

- [Codex](connect-codex.md)
- [Cursor](connect-cursor.md)
- [VS Code](connect-vscode.md)
- [Claude Desktop](connect-claude-desktop.md)
- [Agent skills](agent-skills.md): triage, draft replies and chase pending conversations

