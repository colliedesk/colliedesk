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
2. `contracts/openapi.yaml`: the type in `ChannelType` and in `ConversationChannel`, `<Type>ChannelConfig`
   in `ChannelConfig`, its input in `ChannelConfigInput`, its own routes. The web app's copy of
   `ConversationChannel` (`channelSchema` in `web/src/api/schemas.ts`) gets the value too: it parses every
   conversation, and a missing value fails the inbox list. `TestTheConversationChannelOfTheContractIsTheRegistryAndTheOrigins`
   and `web/src/channels/registry.test.ts` fail until all three agree. The server's conversation filter,
   saved views and MCP tool schemas read the registry (`mcpserver/channel-values.go`).
3. `server/internal/channels/<type>/`: the adapter, its own routes in `Definition.Routes` (mounted by the
   API under `/api/v1`) and tools in `Definition.Tools`. Its directory's name is unique among the
   server's packages, since `go test -c` in CI names each test binary by it: email's is `channelemail`,
   apart from the parser `internal/email`.
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
- **Several channels of a type.** Nothing is one per workspace: a type has any number of live
  channels, one of them its default (`channels.is_default`, one per type by `channels_default_idx`,
  never on a removed row). What names no channel goes to the default: the singular routes
  (`/channels/email`, `/public/forms/<slug>`), MCP tools without `channel_id`, a new conversation
  without `channel_id`. Removing the default hands it to the oldest live channel of the type in the same
  transaction, after the `removed` event, with a `default_changed` event.
- **Anti-duplication has three layers.** The store checks under the type's advisory lock
  (`<workspace>:channels:<type>`) and answers `channels.NameTakenError` / `AddressTakenError` with the
  channel that has the value; the API turns them into 409 with `conflict: {channel_id, channel_name}`;
  the web app puts the refusal at the field with a link. The unique indexes (`channels_name_idx` on
  `lower(name)` per type, `email_channels` on `mailbox_key`, `mailbox_connections` on the address) are
  the net under a race; they surface as `ErrConflict`.
- **One mailbox, one channel.** `email_channels.mailbox_key` (generated) drops a `+tag` and the case,
  so `support+x@` and `support@` are one mailbox. `hosted_local` names each channel's Cloud forwarding
  address: `support` for the workspace's first, the support address's local part for later ones.
- **Verification belongs to the domain** (`email_domains`, keyed by workspace and domain): mailboxes on
  one domain share it, and a domain row goes when its last mailbox does (`releaseUnusedDomain`).
- **Gmail per channel.** A new connection must be the channel's support address (`address_mismatch`)
  and an account sends for one channel (`account_in_use`). A connection made before that rule keeps
  sending and reads `address_matches: false`.
- **Where a reply leaves** is one rule (`routeReply`): the conversation's own channel when its type
  sends (removed: a `SharedIdentity` type answers through its default); else the channel the replies
  are pinned to (`conversations.reply_channel_id`; removed: the default stands in, `replies_via.instead_of`
  says so, nothing is pinned again); not pinned yet: an import's source mailbox (`origin.mailboxes`)
  when the workspace has it, else the type that carries other replies (`CarriesOtherReplies`, email) by
  its default. The first reply pins. A pinned channel that is off fails `channel_off`; it never falls
  back to the default.
- **The web form's older address** (`/public/forms/<slug>`) belongs to the form with `legacy_slug`,
  set on the workspace's first form; every form also answers `/public/web-forms/<id>`.
- **Removing** is a soft delete: the row keeps its name for old conversations; settings and secrets go.
- **A server without SMTP and Gmail** leaves email replies to a server that can send them and sends
  every other type's (`outbox.Options.Skip`).

## Rollback is the backup

The migration `20261025090000-channels` keeps no layer for an image older than it: configuration is
keyed by `channel_id` in one place, Message-IDs live in `messages.external_id`, reply tokens in
`conversation_thread_keys`, and `email_outbox` is gone. An older image does not run on the new schema,
so the way back is the backup and the old image ([upgrade](../self-host/upgrade.md#roll-back)), as for
every release. This is a deliberate exception to expand/contract: a compatibility layer would have
had to fold several channels of a type into one row. The down migration exists for tests and refuses
a database with two channels of a type, naming them.

## Traps

- `channel_types` is not granted to the app role (row-level security gate); foreign keys check it as the
  owner, and nothing at runtime reads it.
- The upgrade gives a workspace whose email or web form setup was gone before it a **removed** channel
  (with its `added` and `removed` events), so its old conversations keep a channel.
- `mailbox_connections.channel_id` stays nullable for an OAuth state started on the singular route
  before the upgrade; `TakeOAuthState` compares `COALESCE(channel_id::text,'')`.
- `channel_outbox.channel_id` may be null only for a delivery that failed `channel_not_connected`
  (`channel_outbox_channel_known`).
- `contacts.email` is nullable. The web app parses responses with zod at runtime: a schema that still
  says `string` breaks the whole list, which typecheck does not catch (`api/schemas.test.ts`).
- `channel_inbound_events` with an empty `EventIDs` array: compare with `COALESCE($n::text[],'{}')`;
  `ANY(NULL)` filters every row out.
