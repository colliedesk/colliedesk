# Connect Cursor

Connect [Cursor](https://cursor.com) to Colliedesk. Cursor has no place to keep a secret apart
from its configuration, so the token ends up in your own `~/.cursor/mcp.json` or in an
environment variable.

## Before you start

1. Sign in to Colliedesk and open **Settings → External AI agents**.
2. Name the token (for example "Cursor on my laptop"), pick a tier (`draft` is a good start: the agent
   can sort and annotate, and nothing reaches a customer) and choose **Create token**. See [Agent tokens](sessions.md#agent-tokens).
3. Copy the token. It starts with `cdk_agent_` and is shown once.

The steps below use `https://desk.example.com`: put the address you open Colliedesk at in its place.
Under **Connect your agent**, Settings shows these steps for Cursor with your address already in.

## Connect

**From Colliedesk (one click).** After you create the token, Settings shows **Add to Cursor**. It
opens Cursor, which asks you to install the Colliedesk server, with the token in its
configuration (`~/.cursor/mcp.json` in your home folder, readable by programs you run). Use a
`read` or `draft` token for it, and revoke the token when you stop using it.

**By hand, with an environment variable.** Put the token in your shell profile:

```sh
export COLLIEDESK_TOKEN=<your token>
```

then add this to `~/.cursor/mcp.json` (create the file if it does not exist; keep any other
servers in `mcpServers`):

```json
{
  "mcpServers": {
    "colliedesk": {
      "url": "https://desk.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${env:COLLIEDESK_TOKEN}"
      }
    }
  }
}
```

Cursor reads `${env:COLLIEDESK_TOKEN}` only when it is started from a terminal that has the variable
(`cursor .`); started from the Dock or the Start menu, it sends no token and Colliedesk answers 401.

In **Cursor Settings → MCP**, `colliedesk` shows its tools when it is connected.

## Try it

Ask Cursor: "Who am I in Colliedesk?" It calls `colliedesk_whoami`, which answers
with your name, your workspace, the token's tier and the tools it may use. Then, for example: "Triage the
open conversations nobody owns: tag billing questions with `billing` and leave a note with a
summary."

## What each tier can do

Colliedesk has 197 tools. A token calls the tools of its tier directly; any other call
becomes an [agent request](agent-requests.md) a person approves in **Requests**.

| Tier | Tools it calls directly |
|---|---|
| `read` | 87 of 197 |
| `draft` | 110 of 197 |
| `send` | 148 of 197 |
| `admin` | 197 of 197 |

Every tool and its tier: [Connect an AI agent (MCP)](mcp.md#tools).

## Remove it

Delete the `colliedesk` entry from `~/.cursor/mcp.json`, or turn it off in **Cursor Settings →
MCP**. Then revoke the token in **Settings → Sessions**: it stops working at once, wherever it was
copied.

## Other hosts

- [Claude Code](connect-claude-code.md)
- [Codex](connect-codex.md)
- [VS Code](connect-vscode.md)
- [Claude Desktop](connect-claude-desktop.md)
- [Agent skills](agent-skills.md): triage, draft replies and chase pending conversations

