# Zalo OA

A Zalo Official Account (OA) is a channel: customers write to it one to one, and in the Zalo groups the OA
is a member of. Each message lands in your inbox as the [chat rules](channels.md#chat-rules) of the
channel decide, and your replies go back where the conversation was said: into its group, or to the
customer alone, quoting the message they answer.

## Connect an OA

An admin opens **Settings → Channels → Add channel → Connect Zalo OA** and fills in:

1. **OA ID** and **App ID**: the numbers Zalo for Developers shows for the Official Account and the app it
   is reached through. One OA is one channel on the whole server: an OA already connected, in this
   workspace or another, is refused with "This OA is connected elsewhere."
2. **App secret** and **OA secret key** (optional here, settable later): stored encrypted, never shown
   again. Each is stored on its own: one the server refuses (for example a server without
   `MAILBOX_TOKEN_KEYS`) leaves the channel added and the other secret stored, and the Connection tab says
   which one was not saved and why.

The channel's page then has four tabs of its own, before AI and History:

1. **Connection**: the **Webhook URL** to paste in the Zalo app's webhook settings (the server's address
   followed by `/api/v1/channels/zalo/webhook`; one app may serve several OAs, and a delivery of an OA no
   channel is answers 200 and is dropped), the OA and app ids, and the **Secrets**. Each secret shows
   whether it is set, its last four characters and when it was last written; **Replace** or **Add**
   stores a new one, and the current one keeps working until then. **Add tokens** / **Replace tokens**
   takes the access and the refresh token together and saves the refresh token first: if only it got in,
   the dialog asks for the access token alone. The tokens show no last four characters, since they
   change each time they are renewed.
   **References** picks the [number series](inbox.md#conversation-numbers) the OA's conversations are
   numbered in and shows the code the next one gets (`CHD-2026-0143`); the series themselves are changed in
   Settings → Conversation numbers.
2. **Groups**: every group that wrote to the OA (and every group linked before it did). **Link…** files the
   group under an organization, written as your contacts' organization is: a conversation of that group
   then names it. **Unlink** stops that for new conversations; earlier ones keep their organization.
3. **Staff**: the Zalo accounts of your team. What they write in a group is the team's, never a customer's
   request. **Add staff account** picks one of the people who wrote to the OA lately, or takes a pasted
   Zalo user id; **Remove** asks first, then their new messages count as a customer's.
4. **Messages**: the commands customers type and what the OA says by itself; see
   [The Messages tab](#the-messages-tab).

Agents see the same tabs and change nothing.

**Pause processing**, at the top of the channel's page (admins), asks first, then keeps every message
Zalo delivers and opens nothing, adds nothing and sends nothing until **Resume processing**: what arrived
is then handled in order by the channel's rules. While paused the channel reads **Processing paused**,
here and in **Channels**, with since when and how many messages wait (counted again every 20 seconds
while the page is open). Agents see the same and are told an admin resumes it.

Pausing is not turning the channel off: a channel turned off keeps its messages under **Held** for
someone to open or discard. A channel paused and off says to turn it on first, and stays paused when
you do.

With the API instead:

1. `POST /api/v1/channels` with `{"type": "zalo", "config": {"oa_id": "<the OA's id>", "app_id": "<the Zalo app's id>"}}`.
2. Its four secrets, one at a time, with `PUT /api/v1/channels/{id}/credentials` (`name`, `value`):
   `oa_secret_key` (the OA secret key of the app's webhook settings), `app_secret` (the app's secret
   key), `access_token` and `refresh_token` (the OA's tokens). The channel's `config.credentials` says for
   each whether it is set, its last four characters and when it was last written (a token refresh too).
3. `config` also takes `staff_user_ids` and `groups` (a group's id to its organization).
   `PATCH /api/v1/channels/{id}` changes them; a field left out is kept. Its `settings` (commands,
   notices, scopes) are replaced whole by a change: read `settings` from the channel, change it, send it
   back.

Set `oa_secret_key` and `app_id` before deliveries arrive: a delivery whose signature does not match is
dropped (answered as one of an OA no channel is, so nobody learns which OAs are here), and Zalo does not
send it again. The channel then says **Needs attention** (`zalo_signature_failed`) for a day, or until a
delivery is signed right again. Without the key it says `zalo_secret_missing`; without the tokens
or the app's secret, `zalo_token_missing` (deliveries are taken, replies cannot be sent). The channel's
line in **Channels** reads "OA 1234…5678 · 2 groups linked".

## The Messages tab

**Save messages** saves the whole tab at once. Agents read it and change nothing.

### Commands

What a customer can type to the OA, in a chat or a group: **Open a request**, **Cancel**, **Reopen**
(followed by a conversation's code), **Confirm** (the answer to the question below) and **Help**. Each has
its phrases and how they match: **Allow one typo** (one wrong letter in a phrase of five letters or more),
**Whole words**, or **Exact** (the whole message, accents kept, in any case; best for short words such as
"OK"). A command with no phrase is off.

**When a customer sends only the command** asks what the request is about when "Open a request" comes
with nothing before it. A request still waiting for its description after **Forget a request still
waiting for its description after** minutes is thrown away (0 never forgets; one-to-one chats and groups
alike).

### What the OA says

Each of these is sent by the OA itself, never counts as your team's first reply, and is left unsent
while empty:

1. **When a conversation opens**: `{ref}` becomes its code.
2. **When a customer asks for help**: the answer to Help, in a chat or a group; it opens nothing. A chat
   gets it at most once in the time set in **Answer help in one chat at most every** (300 seconds unless
   you change it; 0 answers every time).
3. **When a resolved or closed conversation is reopened** (by the customer's Reopen command, or by your
   team), and **When Reopen names a conversation still open** (the message joins it): `{ref}` is its code.
4. **When Reopen names a code that is not found**: `{ref}` is the code typed. A code of another chat or
   group reads the same, since a code reopens only a conversation of its own group or of the customer's
   own one-to-one chat.
5. **When Reopen names a code from before Colliedesk**: `{old_ref}` is the code typed, `{ref}` the new
   conversation's.
6. **Reply to a one-to-one message that is not a request**: sent only where one-to-one messages open a
   conversation on a command and **Answer such a message with this reply** is on. A chat gets it at most
   once in the time set in **Send the unmatched reply in one chat at most every** (300 seconds unless you
   change it).
7. **When a request is cancelled**: after the Cancel command.
8. **When a request waiting for its description is forgotten**: see Commands above.

### When the status of a conversation changes

The OA tells the customer when someone on your team, an AI agent or the closing rule below sets a
conversation's status, in that chat: one to one as a reply to the customer's first message; in a group,
and for a chat moved into another conversation, without a quote. `{ref}` is the
conversation's code and `{subject}` its subject; an empty box sets that status quietly:

1. **When set to Pending**.
2. **When set to Resolved**: also sent by **Send and resolve**.
3. **When set to Closed**: also sent when a conversation closes by itself.

Reopening is told with **When a resolved or closed conversation is reopened** above. To change a status
without telling the customer, see [Telling the customer](inbox.md#telling-the-customer).

**Close resolved conversations after** a number of hours (empty or 0: never; at most 720, 30 days) closes
a conversation that stayed resolved that long, counted in clock hours (not business hours) from the
resolve or the customer's last message, whichever is later. It applies to conversations resolved from
the time you turn it on: earlier ones never close by themselves. A new number while it is on also applies
to those resolved since; a smaller one closes those already past it within minutes of saving. The
sentence under the box says what it will do, and whether the customer is told (only when **When set to
Closed** has words, and the conversation was not resolved quietly: one resolved without telling closes
quietly too). A conversation that closed by itself opens again with **Reopen**.

### Ask before opening (one-to-one)

With **Ask the customer to confirm** on, once a customer who named a command (say "tạo phiếu") stops
writing for **Wait for the customer to stop writing** seconds, the OA sends the **Question** (it must be
written: without it the OA asks nothing). Then:

1. **Confirm** (say "OK"), as the answer to the latest question, opens the conversation at once.
2. Silence for **Open anyway after** seconds opens it too.
3. **Cancel** throws the request away.
4. More messages join the request and the question comes again; an OK sent before it is asked again
   counts as words of the request.

Off, nothing is asked. The same quiet wait comes before asking for a description; its field shows while
either is used.

These apply where one-to-one messages open a conversation on a command (`settings.scopes.direct.mode:
commands`). The page does not change that mode: set it with the API (`PATCH /api/v1/channels/{id}` with
the whole `settings`). Where every one-to-one message opens a conversation, the tab says nothing is
asked.

## How messages come in

1. One to one (scope `direct`): every message is the customer's, and opens or joins their conversation
   as the channel's rules say.
2. In a group (scope `group`): by default a conversation opens on a command only (`settings.commands`);
   what the members say in between is collected and decided by the rules, or by
   [AI intake](ai.md) where it is on.
3. Pictures, files and videos are fetched and kept with the message. A sticker, a location or another
   kind Zalo sends is kept as a message saying what it was, never dropped.
4. A customer is called by their Zalo id until their name is read from Zalo, right after their first
   message is taken: their profile as the OA sees it, or for a group member who does not follow the OA,
   the group's member list (read once for every member, at most once an hour a group). The contact and
   the chat then show it. Zalo is asked at most once an hour for someone it has no name for, and again
   with the next message when it could not answer (a token it refused, the network); your team's Zalo
   users are never looked up.
5. What the OA sends (your replies, and what someone writes in the OA's own chat on Zalo) is the team's.

## Replies

A reply goes to the group of the conversation, or to the customer alone, quoting their last message (sent
again without the quote when Zalo cannot make it). A reply longer than 2000 characters goes in parts. When
Zalo refuses the access token, Colliedesk refreshes it once with the refresh token and keeps both new
tokens; only one sender refreshes at a time, because a refresh token is used once.

A reply carries pictures (JPEG or PNG, read from the file itself, 1 MB at most each; not GIF yet): each goes up to Zalo and is sent as a
message of its own after the text. Another file, or a larger picture, fails the reply before anything is
sent and says why and what to send instead (`zalo_no_files`, `zalo_file_too_large`). Such a reply offers no
Retry (`delivery.retryable` is false; a retry answers 409 `delivery_final`): write it again without the file.

Not yet: other files in a reply, connecting an OA from Zalo's own consent page.
