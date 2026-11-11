# Connect Codex

Connect [Codex](https://developers.openai.com/codex) to Colliedesk. The Codex CLI, its IDE
extension and the ChatGPT desktop app share one configuration, `~/.codex/config.toml`.

## Connect to Colliedesk Cloud with the plugin

The Colliedesk plugin adds the MCP server of Colliedesk Cloud, `https://app.colliedesk.com/mcp`, and the
[agent skills](agent-skills.md). It signs in with OAuth: no token to create or paste.

1. Install the plugin:

   ```sh
   codex plugin marketplace add colliedesk/colliedesk
codex plugin add colliedesk@colliedesk
   ```

2. Sign in:

   ```sh
   codex mcp login colliedesk
   ```

3. Your browser opens Colliedesk. Sign in if it asks, choose the workspace and choose **Allow**.

`codex mcp list` shows `colliedesk` as logged in, and **Settings → Account → Connected
AI apps** in Colliedesk lists Codex. Codex acts as you, with everything you can do in the workspace you
allowed; see [Connected AI apps](sessions.md#connected-ai-apps). To use another workspace, run
`codex mcp logout colliedesk`, then sign in again and choose that workspace. Remove the plugin with
`codex plugin remove colliedesk@colliedesk`; then choose **Disconnect** next to Codex in Settings.

## Connect with an agent token

For a server you run yourself, or to give Codex fewer permissions than you hold, use an agent token.

## Before you start

1. Sign in to Colliedesk and open **Settings → AI & agents → External AI agents**.
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

Colliedesk has 246 tools. The agent acts as you, with the permissions you gave the token,
never more than you hold now. A tool it has the permissions for runs at once; any other is refused
with the permission it lacks. See [Roles and permissions](roles-and-permissions.md).

Every tool and what it needs: [Connect an AI agent (MCP)](mcp.md#tools).

## Remove it

```sh
codex mcp remove colliedesk
```

Delete the `export` line from your shell profile. Then revoke the token in **Settings → AI & agents → Sessions**: it stops working at once, wherever it was
copied.

## Other hosts

- [Claude Code](connect-claude-code.md)
- [Cursor](connect-cursor.md)
- [VS Code](connect-vscode.md)
- [Claude Desktop](connect-claude-desktop.md)
- [Agent skills](agent-skills.md): triage, draft replies and chase pending conversations

