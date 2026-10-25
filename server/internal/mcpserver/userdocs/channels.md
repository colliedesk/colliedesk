# Channels

A channel is one way customers reach your workspace: your [support email](email-channel.md) or your
website's [web form](web-form.md). **Settings → Channels** lists them with what each is doing, and
lets an admin add one, turn it off and on, or remove it. Members of the team who are not admins see
the list and cannot change it.

## The list

Each channel shows its type, its name, one line about it (the support address, the website origins)
and its state:

- **Working**: it receives and sends.
- **Off**: an admin turned it off. See [Turn a channel off](#turn-a-channel-off).
- **Needs attention**: it works only in part, and says why with a link to fix it. Email: sending is
  paused after spam complaints, the server cannot send (no SMTP relay and no Gmail), the Gmail
  connection broke, the support address domain is not verified yet, or (on your own server) no
  inbound secret is set so no mail can arrive. Web form: it is on but lists no website origin, so no
  page can post to it.

## Add a channel

Select **Add channel** and pick a type (with no channel yet, the page lists the types at once:
**Connect your support email**, **Add a web form**). Live chat is listed as not available yet. The
channel exists once you save its first settings: for email, a name and the support address; for the
web form, its allowed origins.

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
**Replies**, how replies leave; web form: **Embed**) and **History**. Old links to the email and web
form sections of Settings open these pages.

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

## For AI agents and the API

Everything above is available through `GET/POST /api/v1/channels` (`include_removed=true` lists
removed channels too; each type says its `default_channel_id`), `GET/PATCH/DELETE
/api/v1/channels/{id}` (`is_default: true` makes it the default; a name or address another channel
has is a 409 with `conflict` naming that channel), `/api/v1/channels/{id}/held/replay` and `/held/discard`, and
`/api/v1/channels/events`, and through the MCP tools `colliedesk_channels_*` ([MCP](mcp.md)). A channel's
rules (`settings`: how messages join conversations, default tags and priority, rate limits) can be
changed there only, not in Settings yet. Removing a channel and discarding held messages are, for an
agent token, requests a person approves. Storing a channel's secret needs an admin token; a lower
token is refused, so a secret never waits in a request.
