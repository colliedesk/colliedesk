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
2. `contracts/openapi.yaml`: the type in the documented values of `ChannelType` ("Shipped types: …") and of
   `ConversationChannel` ("Values: …"), `<Type>ChannelConfig` in `ChannelConfig`, its input in
   `ChannelConfigInput`, its own routes. Both schemas take any type's name (`^[a-z_]{1,40}$`), so a type a
   client does not know yet (the loopback chat stand-in of end-to-end builds, a new chat type before the web
   app has its screens) is listed by its name instead of failing the inbox or Settings → Channels. The web
   app's known values (`knownChannels` in `web/src/api/schemas.ts`) are what the Filter menu offers.
   `TestEveryRegisteredTypeIsWhole`, `TestTheConversationChannelOfTheContractIsTheRegistryAndTheOrigins`
   and `web/src/channels/registry.test.ts` fail until all three agree. The server's conversation filter,
   saved views and MCP tool schemas read the registry (`mcpserver/channel-values.go`).
3. `server/internal/channels/<type>/`: the adapter, its own routes in `Definition.Routes` (mounted by the
   API under `/api/v1`), its routes on the help center in `Definition.HelpCenterRoutes` (mounted by the
   help center below every address of an open help center) and tools in `Definition.Tools`. Its directory's name is unique among the
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

## Rules decide first

What a message is (a new conversation, part of one, chatter kept without one, held for a person, a
cancel) is `intake/rules.Decide`, a pure function of facts the store reads in the order the rules use
them: the cancel before the rate limit and the storage check, a scope's chatter and review before the
contact is looked up (archived chatter creates no contact), then the reopen reference and the thread.
Each decision has a basis: **match** (a command, a reference, a thread key: evidence the message
carries), **default** (what the scope does with a message that carries none), or **person** (a held
message put in a conversation). The journal keeps both (`channel_inbound_events.decision_basis`,
`decision_rule`); a duplicate, an echo, a channel off and a message still being collected keep none,
since the frame handled them before the rules. An AI decider may only ever replace a default.

1. The code is the table, not the design's: an open command whose thread key finds a conversation
   joins it; a reference the join policy refuses lets the next reference try, while a refused thread key
   ends the search (`join_policy`).
2. **Shared scopes** (`Definition.SharedScopes`, a chat's groups): a reference to another sender's
   message (its journal row's `sender_key`) is no match there (`reference_other_customer`, default), so
   a reply to someone else never files the message in their request. Only a shared scope reads the
   sender, so email gains no query.

## Episodes

The chat rules of a scope (`Gather.PreCommandSeconds`, `Scope.CutOnCommand`, `Scope.Unmatched`), the
staff rule and the cancel rule are fields whose zero value is the behaviour before them, so email, web
forms and `channeltest.Run` never change.

1. **The journal knows a batch's command.** `channel_inbound_events.command` is written with the pending
   row. A cancel command cancels only a batch with one (where messages are collected; elsewhere it is
   still dropped), and a command keeps what came within `pre_command_seconds` before it only when the
   batch named none (else three quick commands would merge).
2. **Join open is by the sender's open conversations, not the thread.** `unmatched=join_open` skips the
   thread lookup of a commands-only scope and joins the only open or pending conversation of the
   sender's contact on the channel (`unmatched_join_open`); otherwise the item stays `unresolved`
   (`unmatched_kept`), counted in `/api/v1/channel-thread-items`. The hint a one-to-one chat answers
   with (support-desk's "N-07") needs notices to a place without a conversation: until that ships, a
   chat type relying on it must not go live.
3. **Staff.** `Inbound.Role = staff` that names no command that opens is a `staff` item and nothing
   else; it never gathers, so it never makes or joins a customer's batch.

## Intake

A scope a type declares in `Definition.IntakeScopes` (a chat's groups) gives every batch the rules decide
an `intake_gatherings` row (a card), written in the ingest transaction with what the rules decided
(`rules_action`, `rules_rule`, `rules_target`); its journal events, chat items and AI tasks point at it.
Email and web forms declare none, so their path gains no row and no query beyond reading the scope.

1. **Shared scopes collect per sender.** The aggregate key of a shared scope ends with the sender, and a
   place's thread key there continues only a conversation the sender wrote in (a journal row with their
   `sender_key` and a message of it): a group's key is everyone's, so it is no evidence of whose request a
   message is.
2. **Auto holds only a default.** At intake level `auto`, what the rules decide by `default` (a new
   request, a review hold, a commands-only scope's chatter) is held with `hold_reason='intake'` through
   `holdInbound`, so it is stored and counted exactly as a channel-off hold, and the contact is found only
   when it is stored: a held batch creates nothing. A match is applied at once, never held.
3. **Applying is a replay of the whole batch.** `pipeline.ApplyGathering` parses the held events again
   and replays them merged as one message (`IngestRequest.Intake`): `ForceNew` skips the thread lookup,
   so a new request never joins a conversation opened since the hold; the store locks the gathering and
   applies only while it is `deciding` or `review` and holds exactly those events (`items_seen` of IN3's
   decisions), so a person and the sweep never apply twice (the losing one gets `ErrGatheringDecided`,
   also when SKIP LOCKED skips its events). A person's and AI's decisions are `intake_decisions` rows;
   the rules' decisions stay in the journal only.
4. **The sweep is the fallback.** `intake.Service.Sweep` (every 15 s in `collie serve`, claimed across
   workspaces under `PrivilegeIntakeDueClaim`) applies the rules' decision to what is past `decide_by`
   (two minutes): `create` is replayed through the rules as they stand then, without `ForceNew` (a
   sender's batch held while an earlier one waited too continues the conversation that one opened, as
   without AI; the journal names the rule that applied), `review` goes to the queue (`no_command`), and a
   commands-only scope's chatter goes to the queue too (`ai_unavailable`): an admin turned AI on so it
   would be looked at. `fallback=true` says so.
5. **Lanes.** A gathering's lane is its place and sender (`lane_key`). At `auto`, a batch behind a card
   of its lane in review waits in review (`lane_blocked`); deciding that card gives the next blocked one
   back to `deciding` while it is younger than two hours.
6. **Held readers.** `HeldEvents` and the held replay leave out a gathering's events while it is
   deciding, in review or set aside; `DiscardHeld` may throw away what was set aside, and a gathering
   whose events are all discarded is `cancelled`. Removing a channel still counts every held event, and
   its refusal says how many are cards still to be decided (`queued_count`).
7. **Candidates are safe for the reply.** A card offers to join only what a reply would reach the same
   people through: a group's open or pending conversations active in 72 hours, a one-to-one customer's on
   the same channel, then resolved ones within `reopen_window_days`; never a closed one. They are built
   when the card is read, so they include what the card before it just opened. A person may still attach
   anywhere in the workspace through the API; a conversation merged away (`merged_into_id`) is followed to
   the one it was merged into.

### AI decides

1. **One task a card, one card a lane.** The ingest transaction queues the `intake` AI task in SQL (the
   store cannot import `ai`): `holdForIntake` and every conclusion of a card (`unblockLane`) ask for the
   lane's next turn, which goes to the oldest `deciding` card that never had a task, only while no card of
   the lane has one waiting or running and none waits for a person. At `suggest`, a card the rules applied
   by default or held for a person gets a task of its own. `ai_tasks_intake_gathering` keeps one live task
   a card. The worker polls (5 s), so a card waits seconds for AI, never the request that brought it.
2. **The prompt is built from a snapshot.** `store.IntakeSnapshotOf` reads the card, its scope's level and
   options, the workspace's consents, its items, 40 items of the place before it (the prompt shows 15; the
   rest only pair a login with its password), the quoted items, the candidates (the same set a person is
   offered, the rules' target first) and the channel's commands. `intake/prompt` lays it out with no clock
   and no database, so an evaluation replays the same text, and refuses an answer naming a conversation it
   was not offered (`target_invalid` after one more ask). Prompts and answers are not stored; the decision
   keeps the manifest (sources, counts, cuts, what was hidden) and `prompt_version`.
3. **A gate per action.** `intake.Gate` decides whether AI may act on its own; the default opens an
   action only when the scope's readiness does (13 below), else `gate_closed`. AI applies only at `auto`, on a
   card still `deciding`, through an open gate, at or above the scope's threshold, without a credential,
   with something to read, within `auto_max_age_seconds`, and for attach while the customer has one open
   conversation it could mean (`attach_requires_single_candidate`). Otherwise its decision is recorded
   with `applied=false` and why, and a card that waited at `auto` gets the rules' decision at once
   (`fallback=false`; true when the model failed): the customer never waits for AI that may not act. A
   commands-only scope's chatter then waits for a person with the reason AI did not act.
4. **Who decided first wins.** The suggestion is written in the task's own transaction (`Run.Record`), so a
   cancelled task records nothing; a card a person decided meanwhile records `person_first`, one the rules
   decided past its time `too_old`. An applied decision goes through `Service.Decide` like a person's and
   loses to whoever locked the card first.
5. **A task that ends without deciding hands the card to the rules.** When the worker cancels or fails an
   intake task (the day's `intake_per_day`, the feature off, a locked workspace, retries spent), it sets
   the card's `decide_by` to now, so the next sweep applies the rules instead of waiting two minutes.
6. **People label AI.** A person's decision is compared with the card's newest AI decision nobody labelled
   yet, applied or not (or the one `follows_decision_id` names, which must be one of the card's): the same action and conversation
   follows it (`follows_decision_id`, taking its `reply_kind`), anything else corrects it
   (`corrects_decision_id`). These are the labels intake's evaluation reads.
7. **A thank-you keeps resolved.** A decision's `reply_kind` travels with an attach (`IntakeApply.ReplyKind`):
   `acknowledgement` joins without reopening a resolved conversation (a closed one opens, so the message is seen), and only
   then is the message stored with `messages.intake_reply_kind = 'acknowledgement'` (joined to an open or closed one it is
   `substantive`: it kept nothing), at insert, which `ai_draft_on_customer_message` reads to queue no
   automatic draft. The AI's own label stays in `intake_decisions.reply_kind`. The rules never set it, so email keeps reopening on a thank-you as it always did.
8. **A failing model is left alone.** Five failed calls in a row (provider errors or answers that are not
   a decision) pause a workspace's intake for ten minutes in the process (`intake_paused`): its cards go to
   the rules at once.
9. **Reasons go first.** AI's `reason` is written from what customers said; `PruneIntakeReasons` erases it
   after 90 days (daily, `PrivilegeIntakeReasonPrune`). The decision and its labels stay.
10. **Pictures need three yeses.** The images source on, the workspace's `images` consent, and
   `aiprovider.ImageTaker` saying the model and its fallback read pictures (Claude: always; OpenRouter:
   `architecture.input_modalities` from the same hourly `/models` cache as reasoning). The bytes are the
   files the frame fetched before the batch was held (`channel_inbound_files`, stored), at most four of
   5 MB; the manifest says `consent`, `model_text_only`, `off` or `credential` when none went.
11. **A channel's patterns run before the frame's phone numbers**, so a number of the channel's own shape
   is named as its kind. They are checked when saved (RE2, at most 2000 instructions compiled, never the
   empty match); one that no longer compiles masks nothing rather than failing the card.
12. **The prompt's Version pins what it writes.** `TestThePromptVersionNamesWhatTheModelReads` hashes the
   golden prompt: a change without a new `prompt.Version` fails, since an evaluation counts only for the
   Version and the requested model (`run.Binding.Model`, not the id a provider answers with) it ran on.
13. **Gates open on explicit labels only.** `readinessGate` (the default of `intake.New`) asks
   `store.IntakeReadinessOf` for the scope, the model the binding asks for and `prompt.Version`: people's
   follows and corrections of the last 30 days plus the newest run of each dataset of that model and prompt.
   A decision nobody looked at is no label: at auto most cards go to the rules unseen, so silence would
   read as accuracy. `gate_override` opens an action by hand.

## Chat threads

A chat type gives each message a place (`Inbound.Place`: a one-to-one chat or a group, with whether a
message there may open a conversation) and a role (`Inbound.Role`). The store writes the place
(`channel_threads`) and the message (`channel_thread_items`) in the transaction that writes its journal
row, whatever the rules decided: collected (`gathering`), kept as chatter (`archived`), held
(`pending_review`, or `unresolved` while the channel is off), cancelled (`not_request`), or part of a
conversation (`conversation`, with a `conversation_thread_items` link saying how). Collected messages
stored as one conclude their items together. A reply or notice the channel sent is an item of the thread
its conversation's items are in (`FinishOutbox`), so the copy the channel sends back is the echo it always
was and adds none. One item per journal event (a unique index) and per channel id: a replay never doubles
one. No public route reads a thread; agents read them with `messages:read`.

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
- **A mailbox per channel.** A channel has one mailbox connection, Gmail or Microsoft 365
  (`mailbox_connections.provider`); connecting the other provider replaces it. A new connection must be the
  channel's support address (`address_mismatch`) and an account sends for one channel of a workspace at any
  provider (`account_in_use`). A connection made before that rule keeps sending and reads
  `address_matches: false`. A consent state belongs to its provider (`TakeOAuthState` filters by it), and a
  disconnect removes only that provider's connection ([email](email.md#sending-through-microsoft-365-issue-95-step-1)).
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
- **Routes on the help center.** A help center host never reaches `/api/v1`, so what a type serves to
  the visitors of a help center (a chat) is a `HelpCenterRoute`, below the help center's root on its own
  host, below `/help/<slug>` and on its custom domain alike. The help center resolves the workspace and
  refuses a help center that is off or locked first; the route gets the workspace (`channels.HelpCenter`)
  and finds its channel through the kit (the default, or the one a `channel` query names), never by an
  id alone. A channel that is off, missing or of another workspace answers `HelpCenter.NotFound`: the
  help center's own 404, byte for byte. Routes never shadow the built-in pages (`/`, `/search`, `/a/`,
  `/c/`, `/images/`, `/media/`, `/_/`), and the API builds their handlers, so the help center imports no
  type and no pipeline. Tests prove the frame with `channeltest.Loopback`, run by an API through
  `Config.ExtraChannelTypes`.
- **A server without SMTP, Gmail and Microsoft 365** leaves email replies to a server that can send them and sends
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

- An address of the help center that moves (the path address when `HELP_CENTER_DOMAIN` is set, every
  other address once a custom domain is active, a claim's CNAME target) moves a GET with a 301; a write
  to a type's help center route gets the help center's 404 there instead, because a browser follows a
  301 with a GET and would drop the body. A page posts to the address it was served from.
- Every help center answer carries the help center's headers (`script-src 'none'`,
  `frame-ancestors 'none'`, `X-Frame-Options: DENY`); a route that serves a page with a script or one
  meant for a frame sets its own policy on that answer only.

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
