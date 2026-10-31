# Channels

A channel is one way customers reach your workspace: your [support email](email-channel.md), your
website's [web form](web-form.md), or the [help chat](help-chat.md) where an AI answers from your
help articles and a customer can hand the chat to your team. In **Settings**, **Channels** has a
page per type under it:

- **Email** (`/settings/channels/type/email`): the setup of your support address, then the table of
  your addresses with how each receives, how its replies leave and its state. See
  [Email in Settings](email-channel.md#email-in-settings).
- **Web form** (`/settings/channels/type/web_form`): your web form, or the form to add one.
- **Help chat** (`/settings/channels/type/help_chat`): your [help chat](help-chat.md), or the form to add one.
- **Channels** itself (`/settings/channels`) lists every channel with what each is doing, and lets an
  admin add one, turn it off and on, or remove it, and its **History** tab lists what changed.

Members of the team who are not admins see these pages and cannot change them.

## The list

Each channel shows its type, its name, one line about it (the support address, the website origins)
and its state:

- **Working**: it receives and sends. A channel is Working only once a first message has arrived on it.
- **Waiting for first message**: it is set up, but nothing has arrived yet. Send a test message (to
  your support address from a different address, or through the form) and check that it opens a
  conversation in the inbox; the channel then reads Working. Mail sent from the support address itself,
  or an automatic reply, still ends the wait but opens no conversation. Through the API the state is
  `waiting` with reason `no_message_yet`.
- **Off**: an admin turned it off. See [Turn a channel off](#turn-a-channel-off).
- **Needs attention**: it works only in part, and says why with a link to fix it. Email: sending is
  paused after spam complaints, the server cannot send (no SMTP relay and no Gmail), the Gmail
  connection broke, the support address domain is not verified yet, or (on your own server) no
  inbound secret is set, so customer email forwarded from your mailbox cannot arrive yet. A test
  message works without it. Web form: it is on but lists no website origin, so no
  page can post to it. Help chat: the AI chat feature is off in **Settings → Built-in AI**, so the
  help center shows no chat.

## Add a channel

Select **Add channel** and pick a type (with no channel yet, the page lists the types at once:
**Connect your support email**, **Add a web form**). Live chat is listed as not available yet. The
channel exists once you save its first settings: for email, a name and the support address (step 1 of
the [email setup](email-channel.md#connect-your-support-email), which then goes on with forwarding);
for the web form, its allowed origins.

A workspace can have several channels of a type: a mailbox for support and one for billing, a form
on your shop and one on your careers page. Pick the type again to add another.

- **Names.** Two live channels of a type never share a name (any case). The first channel of a type
  is called after it (**Email**, **Web form**); a later mailbox left without a name takes its support
  address, a later form gets a number (**Web form 2**). Rename a channel at the top of its page; a
  name another channel has is refused at the field, with a link to that channel.
- **The default.** One channel of each type is its default, marked **Default** in the list. What
  names no channel goes to it: a new conversation started without choosing a mailbox, a reply to a
  conversation that came through no channel, the older web form address. **Make default** on a
  channel's page moves it. Removing the default makes the oldest other channel of the type the
  default, and the question says which one.
- **What two channels cannot share** is said at the field, with the channel that has it: two email
  channels never have the same support address (`support+x@` is the same mailbox as `support@`), and
  a Gmail account sends for one email channel only. Two web forms may list the same website.

Each channel has its own page with its type's tabs (email: **Connection**, how mail arrives, and
**Replies**, how replies leave; web form: **Embed**), **AI** and **History**; on **Settings → Email** it is
**Manage**. An email address still waiting for its first email also has its setup,
`/settings/channels/<id>/setup` (**Continue setup**). Old links to the email and web form sections of
Settings open the type's page; a bookmark to `/settings/channels/<id>` opens the same channel.

## Turn a channel off

Turning a channel off keeps its settings and its conversations. While it is off:

- **Email**: mail that arrives is kept, not opened. The channel shows **N messages arrived while
  off**; an admin opens them as conversations (**Open them as conversations**, as they would have
  been had the channel been on) or discards them (**Discard N messages…**). Through the API they can
  also be added to an existing conversation. Discarding cannot be undone; the history
  keeps how many. Kept messages count toward your storage until they are opened or discarded.
- **Web form**: submissions are refused exactly as if the form did not exist, so the address of your
  form cannot be probed.
- **Replies**: a reply written while its channel is off is saved, and its delivery shows the channel
  was off. After you turn the channel on, select **Retry** on the reply; nothing is sent again by
  itself. The channel shows how many replies wait.

A reply written where no channel can carry it yet (a web form conversation before email is
connected) is saved and reads **Saved, not sent**; once you connect email, select **Retry** on it.
Nothing is sent by itself.

Turning a channel off asks first. The composer of a conversation whose replies leave through a
channel that is off says so ("Email is off: this reply is saved but not sent until the channel is
on"), with a link to the channel for an admin. Turning it on again starts receiving and sending at
once.

## Remove a channel

A channel that holds messages that arrived while it was off is not removed: open or discard them
first (the Remove question and **Disconnect email** say how many). Removing a channel deletes its
settings and secrets. Its conversations and their messages stay and
still name it ("Email (removed)"). Removing email is **Disconnect email** on its **Connection** tab; other channels have
**Remove channel**, which asks first.
A conversation of a removed email channel is answered through the default mailbox, and its composer
says so ("Billing was removed; replies send from Support").

## Several mailboxes in the inbox

- **Start a conversation** asks **Send from** when the workspace has two email channels, with the
  default chosen; a channel that is off is listed but cannot be picked.
- **Replies** leave from the mailbox the conversation came through. A conversation that came through
  a web form, or was started by the team, keeps the mailbox of its first reply. With two mailboxes the
  composer says which one ("Replies send from Billing").
- **Filter → Email channels** (shown when a type has two channels) lists the conversations of one
  channel: those that came through it and those whose replies leave from it. A removed channel still
  filters ("Careers (removed)"), and a saved view keeps the choice.

## History

**Settings → Channels → History**, and the **History** tab of each channel, list who added, renamed,
turned on or off, changed (which settings: the support address, the sender name, the allowed origins),
made the default or removed a channel, replaced one of its secrets (never its value), and how many held messages were
opened or discarded.

## Chat threads

On a chat channel, every message said in a place (a one-to-one chat, a group) is kept with that place,
whatever became of it: the customers' messages that opened or joined a conversation, chatter the channel
kept without one, what waits for review, and what the team and the channel sent. A conversation can show
the place around its own messages, with other customers of a group marked. Nothing of a place is ever on
a page a customer sees. Email and web forms have no places. For AI agents and the API:
`GET /api/v1/conversations/{id}/chat` (`linked=only` keeps the conversation's own), `GET
/api/v1/channels/{id}/threads`, `GET /api/v1/channels/{id}/threads/{threadId}/items` and `GET
/api/v1/channel-thread-items` (messages nobody concluded on), and the MCP tools
`colliedesk_conversation_chat_get`, `colliedesk_channel_threads_list`, `colliedesk_channel_thread_items` and
`colliedesk_channel_unresolved_items`.

## Chat rules

A chat channel's scopes (its one-to-one chats, its groups) can each set how a customer's messages become
requests, in the channel's settings (`settings.scopes.<scope>`, an admin's change):
- `gather` collects what a customer says close together into one batch: `quiet_seconds` of silence
  closes it, `max_seconds` after its first message at the latest. With `pre_command_seconds`, what the
  customer said that long before a command (a screenshot sent just before "report a bug") is part of
  the command's request; what they say after it inside the quiet window is too. A batch that already
  named a command never joins the next one: three commands are three requests.
- `cut_on_command` (on unless set to `false`): a command closes what the customer was collecting and
  starts its own batch; off, a second command joins the batch being written (a one-to-one chat composing
  one request).
- A cancel command (such as "Hủy") cancels only a batch of its sender that named a command; with none
  being collected it is an ordinary message.
- How a command's phrases match (`settings.commands.<name>.match`): `fuzzy` (the default) compares the
  message's first words without accents or case and forgives one wrong, missing, added or swapped letter
  in a phrase of five letters or more; `words` compares them the same way but forgives no wrong letter,
  only a missing or doubled space between the phrase's words ("báolỗi" is "Báo lỗi", "báo lỗix" is not);
  `exact` is the whole message, accents kept, in any case ("Hủy" matches "hủy", never "huy" or "Hủy đi").
  A phrase that starts with `#` or `/` keeps it: `/loi` matches "/loi …", not a message starting "lỗi".
- `unmatched: join_open`, on a scope that opens conversations only on a command (`mode: commands`): a
  message that names no command joins the customer's open or pending conversation on the channel when
  they have exactly one. With none or several it is kept, unresolved, in the chat place: it shows in the
  place and among the messages nobody concluded on, and joins nothing.
- `unmatched: notice`, on a scope that opens conversations only on a command: a message that names no
  command opens and joins nothing, is kept in its chat place, and is answered there with the channel's
  `settings.notices.unmatched` (say which command opens a request); every such message gets it, a
  second delivery of the same one does not. The notice is in the place as the channel's own message,
  with whether it went out (`delivery_status`: queued, sent or failed). Without that text nothing is sent.
- A notice's text (`settings.notices.received`, `settings.notices.unmatched`) may only name placeholders
  the channel fills; it fills none yet, so `{ref}` and the like are refused when the settings are saved.
- The channel's received notice (`settings.notices.received`) is sent by the channel, not by the team:
  its message is marked (`notice_kind: received`) and shows in the conversation as "Sent by the channel ·
  not a reply", and the conversation still waits for a first reply in the inbox and in Results; a teammate
  who opened it before the notice is not told someone replied.
- A team member writing in a group on the chat app itself is part of the place, never a customer's
  request: it opens and joins nothing, unless they name a command that opens one.

## Intake queue

On a chat channel's scope that collects messages (its groups, on most chat types), what one customer says
in one go is a **card**: messages close together are collected, each customer apart from the others even
when they write in turn. The channel's rules decide each card as they always did: a command opens a
conversation, a reply to a message continues its conversation (in a group, only a conversation that
customer wrote in), and what the rules hold for a person waits in the queue. When AI intake runs by itself
on the scope (**Auto**, [AI per channel](ai.md#what-ai-does-by-itself-per-channel)), a card the rules
could only decide by default waits up to two minutes for AI, held like any held message; if AI does not
decide in time, the rules' decision applies (the card says **fallback**), and nothing is ever lost.
A card behind one of the same customer that waits for a person waits right after it, and goes back to
be decided once that one is.

AI reads one card of a customer at a time, in the order they were said. It sees what the customer wrote
with the messages just before it in the same place and the conversations the card may join, with people
named only "Customer A" or "Team 1", phone numbers, email addresses and account or ID numbers masked, and
passwords and codes hidden (also a login and its password sent as two messages). For now AI never acts on
its own, even at **Auto**: it says what it would do (open a conversation, add the card to one, or set it
aside, how sure it is, and why) and the rules decide at once, without waiting the two minutes. Its
suggestion shows on the card until a person decides. At **Suggest**, the rules decide as they always did and AI's suggestion shows on
the card. When a person decides as AI suggested, the decision says it followed AI; otherwise that it
corrected it. A person who adds a card to a conversation that was merged into another adds it to the one it
was merged into.

A card names the chat place it was said in, what the rules decided and whether they had evidence (a
command, a reply to a message of a conversation: **match**) or decided by default. While it waits, it lists
the conversations it may join without the reply going to someone else: in a group, the open or pending
conversations of that group active in the last 72 hours; in a one-to-one chat, that customer's on the same
channel; then the ones resolved within the reopen window (7 days unless the channel's intake settings say
otherwise), which reopen when a card joins them. A closed conversation is never offered.

A person decides a card: **open a conversation** with what it said, as one message (never joining a
conversation found since; optionally with one of the channel's commands, for its tags and priority, and a
subject), **add it** to a conversation, or **set it aside** (it stays held and can be put back), saying why
if they like: not a request, answered in the chat, said already, or another reason. Deciding
the same way twice changes nothing; once a card is decided, another decision is refused and says how it
was decided. Messages of a card are decided in the queue, not one by one as held messages; only what was
set aside can be discarded with the held messages. Email and web forms have no cards: every message is
decided by the channel's rules, as before. In the web app, people decide cards on [Review](review.md).

For AI agents and the API: `GET /api/v1/intake/gatherings` (`state=review`, `deciding`, `ignored` or
`decided`, `channel_id`, `scope`, `decided_by` `rules`, `ai` or `agent`), `GET
/api/v1/intake/gatherings/{id}` (with `candidates` and `commands`), `POST
/api/v1/intake/gatherings/{id}/decide` (`action` `create`, `attach` with `conversation_id`, or `ignore`
with an optional `ignore_reason` `chat`, `handled_in_chat`, `duplicate` or `other`; `follows_decision_id`
names the card's `suggestion` it follows), `POST
/api/v1/intake/gatherings/{id}/requeue`, `GET /api/v1/intake/summary` (how many cards wait for a person
and for AI, and the channels and scopes they come from), `GET /api/v1/intake/stats` and `GET
/api/v1/conversations/{id}/intake` (why each message is in a conversation), and the MCP tools
`colliedesk_intake_list`, `colliedesk_intake_get`, `colliedesk_intake_decide`, `colliedesk_intake_requeue`,
`colliedesk_intake_summary`, `colliedesk_intake_stats` and `colliedesk_conversation_intake_get`. Reading
cards needs `messages:read`, the summary and counts `conversations:read`;
opening a conversation from one needs the send tier (the channel may tell the customer it was received),
the other decisions the draft tier; below it, a decision becomes a [request a person
approves](agent-requests.md). Through the API, send the same `Idempotency-Key` header on every retry of one
decision or requeue: a repeat answers what the first one got and decides nothing more.

## AI on a channel

Each channel's **AI** tab sets what each AI feature does by itself on it (triage and drafts; intake
comes on chat channels), apart from the workspace's default; see [AI per channel](ai.md#what-ai-does-by-itself-per-channel).
Agents see the tab; only admins change it.
A change shows in the channel's history as "changed what AI triage does by itself".

## For AI agents and the API

Everything above is available through `GET/POST /api/v1/channels` (`include_removed=true` lists
removed channels too; each type says its `default_channel_id`), `GET/PATCH/DELETE
/api/v1/channels/{id}` (`is_default: true` makes it the default; a name or address another channel
has is a 409 with `conflict` naming that channel), `/api/v1/channels/{id}/held/replay` and `/held/discard`, and
`/api/v1/channels/events`, and through the MCP tools `colliedesk_channels_*` ([MCP](mcp.md)). A channel's
rules (`settings`: how messages join conversations, default tags and priority, rate limits) can be
changed there only, not in Settings yet. Removing a channel and discarding held messages are, for an
agent token, requests a person approves. Storing a channel's secret needs an admin token; a lower
token is refused, so a secret never waits in a request. A channel's AI levels are `GET /api/v1/channels/{id}/ai` and
`PUT /api/v1/channels/{id}/ai/{slot}` (`colliedesk_channel_ai_get`, `colliedesk_channel_ai_set`).
