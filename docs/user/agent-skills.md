# Agent skills

Agent skills teach an AI agent a Colliedesk routine, step by step, with the tools it should use.
Colliedesk ships three, in the open [Agent Skills](https://agentskills.io) format, so Claude Code,
Codex, Cursor, VS Code and Claude load the same files. They work through the Colliedesk MCP server:
connect your host first ([Claude Code](connect-claude-code.md), [Codex](connect-codex.md),
[Cursor](connect-cursor.md), [VS Code](connect-vscode.md), [Claude Desktop](connect-claude-desktop.md)).

| Skill | What it does | Token tier |
|---|---|---|
| `colliedesk-triage` | Sorts the open conversations nobody owns: tags, priority and assignee (AI triage suggestions when they are on), with a private note on each. Never writes to a customer. | `draft` |
| `colliedesk-draft-reply` | Drafts a reply from your saved replies, then the knowledge base, then solved conversations, and leaves it for a person to check. It takes only the idea of another customer's conversation, never their details, and sends only text you approve. | `read` to draft, `send` to send |
| `colliedesk-chase-pending` | Finds customers who have not answered for some days and drafts a reminder, and conversations the team keeps waiting and notes them for their owner. Never closes a conversation on its own. | `read` to find, `draft` for notes |

Each skill first asks Colliedesk which tools your token may use (`colliedesk_whoami`) and works
with those, so it also runs against a server older than the skill. A step above your token's tier
becomes an [agent request](agent-requests.md) a person approves; the skill tells you and waits.

## Install

The skills are in the Colliedesk repository, in
[`plugins/colliedesk/skills`](https://github.com/colliedesk/colliedesk/tree/main/plugins/colliedesk/skills).

- **Claude Code:** the Colliedesk plugin installs them with the MCP server; see
  [Connect Claude Code](connect-claude-code.md). Ask for them by name, for example "use
  colliedesk-triage on the queue", or let Claude pick one.
- **Codex, Cursor and VS Code:** copy the three folders into `~/.agents/skills/` (every project) or
  `.agents/skills/` in a project. On macOS or Linux:

  ```sh
  git clone --depth 1 https://github.com/colliedesk/colliedesk.git /tmp/colliedesk
  mkdir -p ~/.agents/skills
  cp -R /tmp/colliedesk/plugins/colliedesk/skills/colliedesk-* ~/.agents/skills/
  ```

- **Claude (claude.ai and Claude Desktop):** make a ZIP of one skill folder and upload it under
  **Customize → Skills**. Skills need code execution turned on.

Remove a skill by deleting its folder (Claude Code: `claude plugin uninstall colliedesk@colliedesk`).
The skills change with Colliedesk: copy them again after you upgrade.
