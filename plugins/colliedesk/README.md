# Colliedesk plugin

Work your [Colliedesk](https://colliedesk.com) support inbox from Claude or Codex: find, read and triage
conversations, leave notes, draft replies for a person to send, and chase customers who stopped
answering. Colliedesk is an open-source helpdesk; this plugin works with Colliedesk Cloud and with a
server you run yourself.

## What it adds

- **The Colliedesk MCP server** of Colliedesk Cloud, `https://app.colliedesk.com/mcp`. The first
  time Claude uses it, you sign in to Colliedesk in your browser, choose the workspace and choose
  **Allow**. Claude then acts as you, with what you may do in that workspace. No token to copy.
- **Skills**: `colliedesk-triage` (sort and assign new conversations, never writes to a customer),
  `colliedesk-draft-reply` (drafts a reply, sends only the exact text you approve),
  `colliedesk-chase-pending` (follows up on stalled conversations) and `colliedesk-setup-channel`
  (adds or checks a support email, web form, help chat or Zalo channel).

## Set up

1. Install the plugin:

   ```sh
   claude plugin install colliedesk --marketplace colliedesk/colliedesk
   ```

2. In Claude Code, run `/mcp`, choose `plugin:colliedesk:colliedesk` and **Authenticate**. Sign in,
   choose the workspace and choose **Allow**.

On a server you run yourself, keep the plugin for its skills, add your own address with
`claude mcp add --transport http colliedesk https://desk.example.com/mcp`, authenticate it in `/mcp`,
and turn the plugin's Cloud server off there.

Full steps: [Connect Claude Code](https://colliedesk.com/docs/connect-claude-code).

### Codex

```sh
codex plugin marketplace add colliedesk/colliedesk
codex plugin add colliedesk@colliedesk
codex mcp login colliedesk
```

The last command opens Colliedesk in your browser: sign in, choose the workspace and choose **Allow**.
Full steps: [Connect Codex](https://colliedesk.com/docs/connect-codex).

## What it sends, and where

The plugin runs no program on your computer. Claude calls only the Colliedesk address above (or the
one you added), with the sign-in Claude keeps for it. You see and end each sign-in in Colliedesk
under **Settings → Connected AI apps**. The plugin sends nothing anywhere else.

## Privacy and support

[Privacy policy](https://colliedesk.com/privacy) · [Terms](https://colliedesk.com/terms) ·
[Report a problem](https://github.com/colliedesk/colliedesk/issues). License: AGPL-3.0-only.
