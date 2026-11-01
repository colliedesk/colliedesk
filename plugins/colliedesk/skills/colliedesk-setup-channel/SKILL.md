---
name: colliedesk-setup-channel
description: Add or fix a Colliedesk channel (support email, web form, help chat, Zalo OA) so customers can reach the workspace. Use when asked to connect, add, configure or check a channel. Never asks for a secret in the chat. Needs the Colliedesk MCP server.
license: AGPL-3.0-only
---

# Set up a channel in Colliedesk

## What it is for

Connect a way for customers to reach the workspace, or find out why one stopped working, and leave
the person with the channel working or a short list of what only they can do.

## When to use

1. The person asks to add, connect or configure a channel: email, web form, help chat or Zalo OA.
2. The person says messages do not arrive or replies are not delivered on a channel.

## Boundaries

1. Never ask the person to paste a secret (a password, key or token) into the chat, and never pass a
   secret someone typed in the chat to a tool: send them to the channel's page in Colliedesk to enter
   it there.
2. Say plainly which steps happen outside Colliedesk (a DNS record, a webhook address in another
   service) and wait for the person to confirm them.
3. When another service (such as Zalo) will take a webhook only on an address it has verified, give the
   person the exact address to verify that Colliedesk shows for the channel, and set on the channel the
   code that service gives them, so they need no DNS change; then give them the webhook address.
4. When your token may not change channels, give the person the steps from the channel's page
   instead of a dead end.
5. Check the channel's state at the end and report it as Colliedesk gives it.

## Before you start

1. Call `colliedesk_whoami` and work only with the tools in its `tools_you_can_use`; stop and tell
   the person when it says the workspace is locked. A tool's description says how to call it.
2. Read the pages under Reference: they say how the feature works today.
3. When a page describes something your tools do not offer, the person's server is older than the
   page: trust `tools_you_can_use` and tell the person what their server does not have yet.
4. Without web access, `colliedesk_docs_search` with the page's name returns the same guide from
   the person's server.

## Reference

1. [Channels](https://colliedesk.com/docs/channels.md)
2. [Email channel](https://colliedesk.com/docs/email-channel.md)
3. [Web form](https://colliedesk.com/docs/web-form.md)
4. [Help chat](https://colliedesk.com/docs/help-chat.md)
5. [Zalo OA](https://colliedesk.com/docs/zalo.md)
6. [Review](https://colliedesk.com/docs/review.md)
7. [Agent requests and reply drafts](https://colliedesk.com/docs/agent-requests.md)
8. [Agent skills](https://colliedesk.com/docs/agent-skills.md)
