# Connect Codex

Connect [Codex](https://developers.openai.com/codex) to Colliedesk. The Codex CLI, its IDE
extension and the ChatGPT desktop app share one configuration, `~/.codex/config.toml`; the token
stays in an environment variable and never in that file.

## Before you start

1. Sign in to Colliedesk and open **Settings → Sessions**.
2. Choose **Create agent token**, pick Codex and a tier (`draft` is a good start: the agent
   can sort and annotate, and nothing reaches a customer). See [Agent tokens](sessions.md#agent-tokens).
3. Copy the token. It starts with `cdk_agent_` and is shown once.

The steps below use `https://desk.example.com`: put the address you open Colliedesk at in its place.
Settings shows these steps with your address already in.

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

Ask Codex: "What can you do in Colliedesk?" It calls `colliedesk_whoami`, which answers
with your workspace, the token's tier and the tools it may use. Then, for example: "Triage the
open conversations nobody owns: tag billing questions with `billing` and leave a note with a
summary."

## What each tier can do

Colliedesk has 136 tools. A token calls the tools of its tier directly; any other call
becomes an [agent request](agent-requests.md) a person approves in **Requests**.

| Tier | Tools it calls directly |
|---|---|
| `read` | 56 of 136 |
| `draft` | 71 of 136 |
| `send` | 108 of 136 |
| `admin` | 136 of 136 |

Every tool and its tier: [Connect an AI agent (MCP)](mcp.md#tools).

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

