# Agent skills

Agent skills tell an AI agent when to take on a Colliedesk routine, what it is for and what it must
never do, and link the pages of these docs that explain the feature. Colliedesk ships the skills below, in
the open [Agent Skills](https://agentskills.io) format, so Claude Code, Codex, Cursor, VS Code and Claude
load the same files. They work through the Colliedesk MCP server: connect your host first
([Claude Code](connect-claude-code.md), [Codex](connect-codex.md), [Cursor](connect-cursor.md),
[VS Code](connect-vscode.md), [Claude Desktop](connect-claude-desktop.md)).

| Skill | What it is for |
|---|---|
| `colliedesk-triage` | Sorts the open conversations nobody owns: tags, priority and owner, with a private note on each. Never writes to a customer. |
| `colliedesk-draft-reply` | Drafts a reply from what your team already says and leaves it for a person to check. It takes only the idea of another customer's conversation, never their details, and sends only text you approve. |
| `colliedesk-chase-pending` | Drafts a reminder for customers who have not answered for some days, and notes the conversations your team keeps waiting for their owner. Never closes a conversation on its own. |
| `colliedesk-setup-channel` | Adds or fixes a channel (support email, web form, help chat, Zalo OA) and says which steps only you can do. Never asks for a secret in the chat. |

A skill names no tool beyond `colliedesk_whoami` and `colliedesk_docs_search`, and holds no steps
or permissions, so a change in Colliedesk never leaves it wrong.
The agent learns them when it runs: it asks Colliedesk which tools your token may use
(`colliedesk_whoami`), reads each tool's description, and reads the linked pages on colliedesk.com.
An agent without web access gets the same pages from your server with `colliedesk_docs_search`. When
a page describes something your server does not have yet, the agent trusts your server and says so.
A step your token has no permission for is refused with the permission it needs; the agent tells
you which one, and you give it a token that holds it
([Roles and permissions](roles-and-permissions.md#when-a-permission-is-missing)).

The Colliedesk plugin for Claude Code also adds `/colliedesk-token`, a command you run yourself to
replace the plugin's token; see [Connect Claude Code](connect-claude-code.md).

## Install

The skills are in the Colliedesk repository, in
[`plugins/colliedesk/skills`](https://github.com/colliedesk/colliedesk/tree/main/plugins/colliedesk/skills).

- **Claude Code:** the Colliedesk plugin installs them with the MCP server; see
  [Connect Claude Code](connect-claude-code.md). Ask for them by name, for example "use
  colliedesk-triage on the queue", or let Claude pick one.
- **Codex, Cursor and VS Code:** copy the skill folders into `~/.agents/skills/` (every project) or
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
