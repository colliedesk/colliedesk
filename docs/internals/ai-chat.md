# AI chat

The help center's AI chat: end users ask, an AI answers from the published articles. User side:
[AI chat](../user/ai-chat.md), [Help chat](../user/help-chat.md). This page records decisions and traps.

## Shape

- **Two switches.** How visitors reach the chat and hand it to the team is a channel type, `help_chat`
  (`channels/helpchat`): its surface (greeting, suggestions, extra instructions, embedding sites) is
  `help_chat_channels`, its hand-off an `Inbound` through the shared ingest. The AI is a feature slot,
  `chat` (`ai_slots`), with its provider, model and consent. The chat answers only while the channel and
  the slot are both on; anything else is the help center's 404.
- **Routes below the help center.** A help center host never reaches `/api/v1`, so the chat's pages and
  calls are the type's `HelpCenterRoutes` ([channels](channels.md#decisions)). The team's side (usage,
  chats, transcripts) is under `/api/v1/ai/chat/*` with tokens and MCP tools like any route.
- **The AI is the ai package's.** Turns, the articles it reads, limits and the chat's cookie live in
  `internal/ai` (`chat*.go`); the adapter holds the surface, the hand-off and the mount point.

## Decisions

- **Its own money bucket.** Chat never spends the team's included AI (`ai_usage` feature `chat` is left
  out of the allowance): a stranger asking questions must not stop drafts and triage. On the cloud's own
  provider the limits are the plan's (`domain.ChatLimitsFor`: answers and micro-dollars a month, a tenth
  a day), in the code, not in a table an admin edits. On a workspace's own provider, or a server without
  plan limits, the only limit is the slot's `monthly_cap_micro_usd`, if set.
- **Transcripts are messages.** Reading them needs `messages:read` (agent role reads), deleting one is
  an admin's and every agent token's delete is a request a person approves; the deletion is an
  `ai_settings_events` row without texts. Texts go 90 days after a chat's last turn, the numbers stay
  400 days (`sweepChats` in the AI worker's sweep), transfer codes a day after they expired.
- **Rate limits of the hand-off count the address, not the email.** An end user types any email, so
  `Inbound.RateKey` (a hash of the client address per workspace) is what the journal's `sender_key`
  holds for a hand-off; the contact is still found by the email.

## Traps

- `ai_settings_events.kind` and `ai_slots.slot` / `ai_usage.feature` are CHECK lists: a new slot or
  event kind changes the migration, the contract's enums, the MCP schemas (`catalog-ai.go`) and the web
  app's zod enums together.
