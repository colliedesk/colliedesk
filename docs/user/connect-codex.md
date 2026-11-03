# Connect Codex

Connect [Codex](https://developers.openai.com/codex) to Colliedesk. The Codex CLI, its IDE
extension and the ChatGPT desktop app share one configuration, `~/.codex/config.toml`; the token
stays in an environment variable and never in that file.

## Before you start

1. Sign in to Colliedesk and open **Settings → External AI agents**.
2. Name the token (for example "Codex on my laptop"), choose what it may do (follow a role,
   or pick permissions; leaving out sending to customers is a good start) and choose **Create token**.
   See [Agent tokens](sessions.md#agent-tokens).
3. Copy the token. It starts with `cdk_agent_` and is shown once.

The steps below use `https://desk.example.com`: put the address you open Colliedesk at in its place.
Under **Connect your agent**, Settings shows these steps for Codex with your address already in.

## Connect

1. Put the token in your shell profile (`~/.zshrc` or `~/.bashrc`) so every new terminal has it,
   and open a new terminal:

   ```sh
   export COLLIEDESK_TOKEN=<your token>
   ```

2. Add Colliedesk:

   ```sh
   codex mcp add colliedesk --url https://desk.example.com/mcp --bearer-token-env-var COLLIEDESK_TOKEN
   ```

`codex mcp list` shows `colliedesk` with `Bearer token`. Codex reads the variable when it
starts, so start it from a terminal that has it. The IDE extension and the desktop app only see it
when they are started from such a terminal.

## Try it

Ask Codex: "Who am I in Colliedesk?" It calls `colliedesk_whoami`, which answers
with your name, your workspace, your roles, the token's permissions and the tools it may use. Then, for example: "Triage the
open conversations nobody owns: tag billing questions with `billing` and leave a note with a
summary."

## What the agent can do

Colliedesk has 230 tools. The agent acts as you, with the permissions you gave the token,
never more than you hold now. A tool it has the permissions for runs at once; any other is refused
with the permission it lacks. See [Roles and permissions](roles-and-permissions.md).

Every tool and what it needs: [Connect an AI agent (MCP)](mcp.md#tools).

## Remove it

```sh
codex mcp remove colliedesk
```

Delete the `export` line from your shell profile. Then revoke the token in **Settings → Sessions**: it stops working at once, wherever it was
copied.

## Other hosts

- [Claude Code](connect-claude-code.md)
- [Cursor](connect-cursor.md)
- [VS Code](connect-vscode.md)
- [Claude Desktop](connect-claude-desktop.md)
- [Agent skills](agent-skills.md): triage, draft replies and chase pending conversations

