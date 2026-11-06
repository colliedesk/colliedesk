# Colliedesk for Claude

Work your [Colliedesk](https://colliedesk.com) support inbox from Claude: find, read and triage
conversations, leave notes, draft replies for a person to send, and chase customers who stopped
answering. Colliedesk is an open-source helpdesk; this plugin works with Colliedesk Cloud and with a
server you run yourself.

## What it adds

- **The Colliedesk MCP server** at the address you give (`https://<your Colliedesk>/mcp`). Claude
  calls it with your agent token; what the token may do is set when you create it in Colliedesk.
- **Skills**: `colliedesk-triage` (sort and assign new conversations, never writes to a customer),
  `colliedesk-draft-reply` (drafts a reply, sends only the exact text you approve),
  `colliedesk-chase-pending` (follows up on stalled conversations) and `colliedesk-setup-channel`
  (adds or checks a support email, web form, help chat or Zalo channel).
- **`/colliedesk-token`** in Claude Code: replaces the token after the old one was revoked or expired.

## Set up

1. In Colliedesk, open **Settings → External AI agents** and create a token. It starts with
   `cdk_agent_` and is shown once.
2. Install the plugin with your address, then give it the token when Claude asks:

   ```sh
   claude plugin install colliedesk --marketplace colliedesk/colliedesk --config url=https://desk.example.com/mcp
   ```

Full steps: [Connect Claude Code](https://colliedesk.com/docs/connect-claude-code).

## What it sends, and where

Every call goes only to the Colliedesk address you configured, with your token in the
`Authorization` header. `/colliedesk-token` runs `scripts/colliedesk-token.sh`, which checks a new
token with one request to that same address before saving it, and never prints it. The plugin
sends nothing anywhere else.

## Privacy and support

[Privacy policy](https://colliedesk.com/privacy) · [Terms](https://colliedesk.com/terms) ·
[Report a problem](https://github.com/colliedesk/colliedesk/issues). License: AGPL-3.0-only.
