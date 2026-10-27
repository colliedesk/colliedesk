# Connect VS Code

Connect GitHub Copilot in [VS Code](https://code.visualstudio.com) to Colliedesk. VS Code asks for
the token once and stores it securely; it never appears in the configuration file.

## Before you start

1. Sign in to Colliedesk and open **Settings → External AI agents**.
2. Name the token (for example "VS Code on my laptop"), pick a tier (`draft` is a good start: the agent
   can sort and annotate, and nothing reaches a customer) and choose **Create token**. See [Agent tokens](sessions.md#agent-tokens).
3. Copy the token. It starts with `cdk_agent_` and is shown once.

The steps below use `https://desk.example.com`: put the address you open Colliedesk at in its place.
Under **Connect your agent**, Settings shows these steps for VS Code with your address already in.

## Connect

1. In VS Code, run **MCP: Open User Configuration** from the Command Palette.
2. Add the `inputs` entry and the `colliedesk` server below to the file (keep what is already
   in it):

   ```json
   {
     "inputs": [
       {
         "type": "promptString",
         "id": "colliedesk-token",
         "description": "Colliedesk agent token",
         "password": true
       }
     ],
     "servers": {
       "colliedesk": {
         "type": "http",
         "url": "https://desk.example.com/mcp",
         "headers": {
           "Authorization": "Bearer ${input:colliedesk-token}"
         }
       }
     }
   }
   ```

3. Start the server from the file or the **MCP Servers** view. VS Code asks for the Colliedesk
   agent token once: paste it.

The `colliedesk` tools then show in the tools picker of Copilot chat in agent mode.

## Try it

Ask VS Code: "Who am I in Colliedesk?" It calls `colliedesk_whoami`, which answers
with your name, your workspace, the token's tier and the tools it may use. Then, for example: "Triage the
open conversations nobody owns: tag billing questions with `billing` and leave a note with a
summary."

## What each tier can do

Colliedesk has 158 tools. A token calls the tools of its tier directly; any other call
becomes an [agent request](agent-requests.md) a person approves in **Requests**.

| Tier | Tools it calls directly |
|---|---|
| `read` | 69 of 158 |
| `draft` | 85 of 158 |
| `send` | 122 of 158 |
| `admin` | 158 of 158 |

Every tool and its tier: [Connect an AI agent (MCP)](mcp.md#tools).

## Remove it

Delete the `colliedesk` server and the `colliedesk-token` input from the file. Then revoke the token in **Settings → Sessions**: it stops working at once, wherever it was
copied.

## Other hosts

- [Claude Code](connect-claude-code.md)
- [Codex](connect-codex.md)
- [Cursor](connect-cursor.md)
- [Claude Desktop](connect-claude-desktop.md)
- [Agent skills](agent-skills.md): triage, draft replies and chase pending conversations

