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
**Connect your support email**, **Add a web form**). A workspace has one email channel and one web
form for now; a type you already have cannot be picked again ("One per workspace for now"). Live chat
is listed as not available yet. The channel exists once you save its first settings: for email, the
support address; for the web form, its allowed origins.

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
Adding the type again makes a new channel; conversations of a removed email channel are answered
through the new one.

## History

**Settings → Channels → History**, and the **History** tab of each channel, list who added, renamed,
turned on or off, changed (which settings: the support address, the sender name, the allowed origins)
or removed a channel, replaced one of its secrets (never its value), and how many held messages were
opened or discarded.

## For AI agents and the API

Everything above is available through `GET/POST /api/v1/channels`, `GET/PATCH/DELETE
/api/v1/channels/{id}`, `/api/v1/channels/{id}/held/replay` and `/held/discard`, and
`/api/v1/channels/events`, and through the MCP tools `colliedesk_channels_*` ([MCP](mcp.md)). A channel's
rules (`settings`: how messages join conversations, default tags and priority, rate limits) can be
changed there only, not in Settings yet. Removing a channel, storing its secret and discarding held
messages are, for an agent token, requests a person approves.
