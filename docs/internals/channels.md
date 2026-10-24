# Channels

How a channel type plugs in, and the decisions and traps behind it. User side: [channels](../user/channels.md).

## Shape

A channel is a row of `channels` plus its type's adapter (`server/internal/channels/<type>`), which has
exactly three parts:

1. **Config** (`ConfigPart`): validates and saves the type's own table (`email_channels`,
   `web_form_channels`), shows its public configuration, its state (`working` or `needs_attention`
   with a reason code) and what Parse needs of its tables (`Snapshot`).
2. **Input** (`InboundPart`): `Parse` turns a raw event into normalized `Inbound` messages. It is pure:
   the same raw event and snapshot give the same messages, which a replay from the journal relies on.
   A type whose media are links implements `MediaSource` too.
3. **Output** (`OutboundPart`): `Prepare` runs in the transaction that stores a reply (fixes the ids
   retries must reuse, applies the trial's rules); `Send` runs in the outbox worker. A type that sends
   nothing names the type that does (`RepliesVia`: the web form replies by email).

Everything else is shared and knows no type: the store's ingest (`store/channel-ingest.go`, the only
writer of conversations, contacts and messages from a channel), the journal (`channel_inbound_events`),
the outbox (`channel_outbox`, `channels/outbox`), configuration and history (`store/channel-config.go`),
secrets (`channel_credentials`, sealed with `MAILBOX_TOKEN_KEYS`), the API (`httpapi/channel-handlers.go`),
the MCP tools (`mcpserver/catalog-channels.go`) and the web pages. `TestTheFrameNamesNoChannelType`
fails when shared code names a type; `TestEveryRegisteredTypeIsWhole` when a registered type lacks its
`channel_types` row, its contract test, its guide or its schema in the contract.

## Adding a type

1. A migration: one `channel_types` row, its own table if it has configuration (with `channel_id`,
   row-level security), fixture rows.
2. `contracts/openapi.yaml`: the type in `ChannelType`, `<Type>ChannelConfig` in `ChannelConfig`, its
   input in `ChannelConfigInput`, its own routes.
3. `server/internal/channels/<type>/`: the adapter, its own routes in `Definition.Routes` (mounted by the
   API under `/api/v1`) and tools in `Definition.Tools`.
4. One line in `server/internal/channels/registry/registry.go`.
5. A test that runs `channeltest.Run` against it: the contract every type passes (new message, duplicate,
   thread and reopen, off, other workspace, rate limit, failed processing, sending, retries, off while
   replying, configuration without secrets).
6. Web: `web/src/channels/<type>/` and one line in `web/src/channels/registry.ts`.
7. `docs/user/<guide>` (Definition.Guide) and a line in `docs/user/channels.md`.

No file of the shared code changes. When one has to, the frame lacks something: fix the frame first, in
its own change.

## Decisions

- **Journal timing.** An Async type (a chat webhook that must answer within seconds) commits the raw
  event first and a worker processes it. A Sync type (email from a forwarder, a form post) writes its
  journal row in the processing transaction: its sender retries an error, and committing first would
  store mail that is then dropped, a duplicate or refused, outside the storage quota. A Sync row keeps
  a summary (`Inbound.Summary`), not the bytes; email's raw is its `original-message.eml.gz` attachment.
- **Held events count toward storage** (`raw_bytes`, `channel_inbound_files`, trigger) until they become
  attachments or are discarded; `DiscardObject` never deletes an object a waiting event points at.
- **Rate limits** count the journal (`sender_key`), so they are the same for every type. Right after the
  upgrade the journal is empty: mail of the hour before is not counted (accepted).
- **A reply whose channel is off, removed or cannot reach the customer** gets a failed delivery with
  `last_error_code` (`channel_off`, `channel_removed`, `no_recipient`), so the thread says why and Retry
  works once it is fixed. Nothing is resent by itself.
- **Where a reply leaves** is one rule (`routeReply`): the conversation's channel when its type sends;
  else the type it names; for a conversation that came through none, the type that carries other
  replies (`CarriesOtherReplies`, email). A removed channel of a singleton type gives way to the live one.
- **Removing** is a soft delete: the row keeps its name for old conversations; settings and secrets go.
- **A server without SMTP and Gmail** leaves email replies to a server that can send them and sends
  every other type's (`outbox.Options.Skip`).

## Older images (expand, contract)

The migration `20261025090000-channels` keeps an image older than it working on the new schema after a
rollback that keeps it: `email_outbox` is a view of the outbox's email rows, and triggers fill the new
columns for its writes (a channel row when it connects email or saves the form, `channel_id` of new
conversations and deliveries, `external_id` from `email_message_id` and back, thread keys from reply
tokens, the channel's on and off from `web_form_channels.enabled`). Current code writes both sides.
Remove the view, the triggers, `email_message_id`, `email_reply_token` and `web_form_channels.enabled` in
one later migration, once no image older than this one is a rollback target.

## Traps

- `channel_types` is not granted to the app role (row-level security gate); foreign keys check it as the
  owner, and nothing at runtime reads it.
- The upserts of `email_channels` and `web_form_channels` fire their BEFORE INSERT trigger even when they
  update: the trigger reuses the live channel instead of making a second one.
- `contacts.email` is nullable. The web app parses responses with zod at runtime: a schema that still
  says `string` breaks the whole list, which typecheck does not catch (`api/schemas.test.ts`).
- `channel_inbound_events` with an empty `EventIDs` array: compare with `COALESCE($n::text[],'{}')`;
  `ANY(NULL)` filters every row out.
