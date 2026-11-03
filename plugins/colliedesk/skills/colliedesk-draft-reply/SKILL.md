---
name: colliedesk-draft-reply
description: Draft a reply to a Colliedesk conversation for a person to check and send. Use when asked to answer, reply to or draft a response for a customer ticket. For reminders to customers who stopped answering, use colliedesk-chase-pending instead. Sends only when the person approves the exact text. Needs the Colliedesk MCP server.
license: AGPL-3.0-only
---

# Draft a reply in Colliedesk

## What it is for

Write a reply in the team's voice, built on what the team already says (saved replies, help
articles, solved conversations), and leave it as a draft a person edits, sends or discards in
Colliedesk.

## When to use

1. The person asks to answer, reply to or draft a response for a conversation.
2. A customer waits for an answer the team has given before.

## Boundaries

1. Never carry one customer's data to another: take only the idea of a solved conversation, never
   its names, addresses, order or account numbers, amounts or dates. When unsure, leave it out and
   say what the person should fill in.
2. Send only when the person approves the exact text in this turn. Otherwise leave a draft.
3. If someone else is already writing a reply, or replied since you read the conversation, stop and
   show the person before anything else.
4. Show the person the draft and where its content came from.

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
2. [Saved replies](https://colliedesk.com/docs/saved-replies.md)
3. [Knowledge base](https://colliedesk.com/docs/knowledge-base.md)
4. [AI providers and features](https://colliedesk.com/docs/ai.md)
5. [Roles and permissions](https://colliedesk.com/docs/roles-and-permissions.md)
6. [Agent skills](https://colliedesk.com/docs/agent-skills.md)
