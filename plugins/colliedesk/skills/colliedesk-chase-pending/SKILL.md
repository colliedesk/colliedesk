---
name: colliedesk-chase-pending
description: Chase stalled Colliedesk conversations. Use when asked to follow up on customers who have not answered for some days, or to find conversations where the team has kept a customer waiting. To answer one conversation, use colliedesk-draft-reply; to sort new ones, colliedesk-triage. Never closes or sends without the person's yes. Needs the Colliedesk MCP server.
license: AGPL-3.0-only
---

# Chase pending conversations in Colliedesk

## What it is for

Find conversations that stalled and prepare the next step for a person: a friendly reminder draft
for customers who stopped answering, and a note for the owner where the team keeps a customer
waiting.

## When to use

1. The person asks to follow up, chase or nudge pending conversations.
2. The person wants to know which customers the team has kept waiting.

## Boundaries

1. Ask how many days count as stalled when the person did not say; 3 is a reasonable default.
2. Never close, resolve or reassign a conversation unless the person asks for exactly that in this
   turn.
3. Never send: leave reminders as drafts. One reminder per conversation, and skip a conversation
   that already has one waiting or a note that says it waits.
4. End with two lists: the reminders you drafted, and the conversations waiting on the team, oldest
   first.

## Before you start

1. Call `colliedesk_whoami` and work only with the tools in its `tools_you_can_use`; stop and tell
   the person when it says the workspace is locked. A tool's description says how to call it.
2. Read the pages under Reference: they say how the feature works today.
3. When a page describes something your tools do not offer, the person's server is older than the
   page: trust `tools_you_can_use` and tell the person what their server does not have yet.
4. Without web access, `colliedesk_docs_search` with the page's name returns the same guide from
   the person's server.

## Reference

1. [Work the inbox](https://colliedesk.com/docs/inbox.md)
2. [Response targets](https://colliedesk.com/docs/response-targets.md)
3. [Agent requests and reply drafts](https://colliedesk.com/docs/agent-requests.md)
4. [Agent skills](https://colliedesk.com/docs/agent-skills.md)
