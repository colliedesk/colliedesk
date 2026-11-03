---
name: colliedesk-triage
description: Triage the Colliedesk support inbox. Use when asked to sort, tag, prioritize or assign open conversations that nobody owns, or to "triage the queue". Not for chasing stalled conversations (colliedesk-chase-pending). Never writes to a customer. Needs the Colliedesk MCP server.
license: AGPL-3.0-only
---

# Triage the Colliedesk inbox

## What it is for

Sort the conversations the team has not picked up yet, so each one has the right tags, priority
and owner, with a short private note that says what the customer wants and why it was sorted
that way.

## When to use

1. The person asks to triage, sort, tag, prioritize or assign the queue.
2. Open conversations nobody owns are piling up.

## Boundaries

1. Never reply to a customer: replies belong to `colliedesk-draft-reply`, and a person sends them.
2. Reuse the workspace's tags; do not invent near duplicates.
3. One note per conversation; never repeat a call that succeeded.
4. End with a list for the person: each conversation, what changed, and what you were unsure of.

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
2. [Tags](https://colliedesk.com/docs/tags.md)
3. [Manage your team](https://colliedesk.com/docs/team.md)
4. [AI providers and features](https://colliedesk.com/docs/ai.md)
5. [Roles and permissions](https://colliedesk.com/docs/roles-and-permissions.md)
6. [Agent skills](https://colliedesk.com/docs/agent-skills.md)
