# Connect Claude Code

Connect [Claude Code](https://code.claude.com) to Colliedesk with the Colliedesk plugin: one
install adds the Colliedesk MCP server and the [agent skills](agent-skills.md). Claude Code keeps
the token in your system's credential store, not in a settings file.

## Before you start

1. Sign in to Colliedesk and open **Settings → External AI agents**.
2. Name the token (for example "Claude Code on my laptop"), pick a tier (`draft` is a good start: the agent
   can sort and annotate, and nothing reaches a customer) and choose **Create token**. See [Agent tokens](sessions.md#agent-tokens).
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
with your name, your workspace, the token's tier and the tools it may use. Then, for example: "Triage the
open conversations nobody owns: tag billing questions with `billing` and leave a note with a
summary."

## What each tier can do

Colliedesk has 227 tools. A token calls the tools of its tier directly; any other call
becomes an [agent request](agent-requests.md) a person approves in **Requests**.

| Tier | Tools it calls directly |
|---|---|
| `read` | 109 of 227 |
| `draft` | 138 of 227 |
| `send` | 176 of 227 |
| `admin` | 227 of 227 |

Every tool and its tier: [Connect an AI agent (MCP)](mcp.md#tools).

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

