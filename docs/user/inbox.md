# Work the inbox

The inbox has three panes: the **conversation list** on the left, the **timeline** in the middle
with the composer below it, and **conversation details** on the right. On a narrower screen the
details pane is hidden; open it with **Show details** above the timeline.

On a phone the list and the conversation take turns. A conversation you open fills the screen, with its
subject at the top and the composer at the bottom. **Back to Inbox** above the subject, or the browser's
Back, returns to the list with the same filters and scrolled to where you left it. The workspace name
and your address stay at the top while the page scrolls.

The [setup guide](setup-guide.md) waits in the bottom-left corner while setup is unfinished, also when
conversations exist; folded to one button, it does not cover them. It lists **Receive customer email**,
**Import from Freshdesk**, **Invite your team** and **Answer your first email**, each with its state. A new workspace has no conversations yet; the list then explains
that and the guide shows what to do next. A workspace
with an email channel also offers **Send yourself a test message**: it opens a conversation from your
own address that you can answer, and the reply comes to your mailbox (see
[Send yourself a test message](email-channel.md#send-yourself-a-test-message)). If the list
cannot be loaded, it says so with **Try again** instead.

**Find conversations**
- The inbox opens on **Open** conversations, the ones that still need an answer. **All** shows every
  status. When nothing is open but the workspace has conversations, the list says so instead of
  showing the new-workspace steps.
- Pick a **Status** tab (All, Open, Pending, Resolved, Closed), and narrow further with **Filter** and
  **Sort**; see [Filter and sort](#filter-and-sort) below.
- **Search conversations** matches the subject, message text and the customer's name and email, in every status unless you picked a
  status tab first, so imported closed tickets are found; the view stays on **All** after you clear
  the search. Characters such as `%` and `_` are matched
  literally.
- A search for a name or an email address also lists the matching customers under **Customers**,
  above the conversations. Select one to open the customer's page with every conversation they had;
  see [Customers](contacts.md).
- The customer's name under the subject of an open conversation opens the same page.
- A conversation you opened stays on screen after you resolve or close it, although it leaves the
  Open list.
- When nothing matches, **Clear filters** below the message resets the search and every filter, and
  returns to **Open**, in one step.
- By default the list is ordered by latest activity and grouped under **Today** and **Earlier**. Use **Previous page** and **Next page** below the list to move; they appear only when there is more than one page.
- A row under **Today** shows only its time; an earlier row shows its date. Days follow your timezone
  (see [Timezones](timezones.md)). The full date and time are in the tooltip of the row and are read out
  by a screen reader.
- Dates from an earlier year show the year, for example imported history.
- **Move with the keyboard:** press **J** to open the next conversation in the list and **K** for the
  previous one. While a row (or its box) has the focus, **Down arrow** and **Up arrow** do the same. The
  focus moves to the row you opened. With no conversation open, **J** opens the first one. The keys do
  nothing while you type in a box, while you hold Ctrl, Cmd, Alt or Shift, or at the first and last row of the
  page; use **Next page** to go on.
- The list loads again each time you open the inbox, so mail that arrived meanwhile is there.
  You do not need to reload to see new mail.

## Conversation numbers

Every conversation has a number and a code its customer sees and can type back: `#12` in the workspace's
default series. A channel can number what it opens in a series of its own (`settings.number_series` of
the channel), with its own code format, for example `REF-{yyyy}-{n:04}` for `REF-2026-0170`: letters,
digits, `#`, `-`, `_`, `/` and `.`, with `{n}` once (`{n:04}` pads it to four digits, 2 to 8), and `{yyyy}`
for the year it was opened in the workspace's timezone. A series counts on and never restarts with the
year; its next number can be raised, never lowered. When you move to Colliedesk from a system that
numbered its own way, set the series' next number (and `start`, `start_year`) to where that system
stopped: codes below it that are not here belong to the system before.

In the web app:

1. A conversation numbered in a series of its own shows its code first on its row and in its header,
   with a button that copies it. The default series' `#12` is not shown, so an inbox of email and web
   forms reads as before.
2. **Settings → Conversation numbers** lists the series with the code the next conversation gets.
   Admins add a series and change one: its format (codes already given keep theirs and still find their
   conversations), its next number (it only goes up: a lower one is refused before anything is sent),
   and where it was taken over (**Taken over at**, **In the year**). Agents read the page.
3. A channel picks its series on its own page (Zalo OA: Connection → References).
4. A link with `?ref=` opens the list on that conversation, in every status, with a **Reference** chip
   that removes it.

`GET /api/v1/conversation-number-series` lists the series; `PUT /api/v1/conversation-number-series/{series}`
adds or changes one (admins; an agent token asks a person first). MCP: `colliedesk_number_series_list`,
`colliedesk_number_series_set_request`.

Find a conversation by its code with `?ref=` on `GET /api/v1/conversations` (MCP `ref` of
`colliedesk_inbox_search`): another case, spaces around it or more or fewer leading zeros match, a code of
another year does not. Search words that are a code find that conversation, ahead of the rest.

## Filter and sort

**Filter** (next to the search box) holds every filter. Ticking several values of one filter shows
conversations with any of them; each filter you add narrows the list further.
- **Priority**: Urgent, High, Normal, Low.
- **Assignee**: **Me**, each other teammate, or **Unassigned**.
- **Tags**: conversations with any of the ticked tags. Once you tick two or more, **Has every chosen tag**
  appears; tick it to show only conversations with all of them.
- **Needs attention**: **Waiting on us** (the newest message that is not a private note came from the
  customer) and **Reply delivery failed** (a reply that could not be emailed and was not retried since).
  Reply drafts that wait for approval are on the [Requests](agent-requests.md) page.
- **Channel**: how the conversation started. **Email**, **Web form** and **Help chat** are listed once the
  workspace has a channel of that type. **Imported** (from another helpdesk) and **Started by the team**
  (created in Colliedesk by a teammate or an agent token) are always listed.
  Conversations that existed before channels were recorded got theirs from their first message; one
  without a message from the customer first counts as started by the team.
- **Email channels**, **Web form channels**: shown when the workspace has two or more channels of that
  type. Each one lists the conversations that came through it or whose replies leave from it. The chip
  names the channel, with "(removed)" for a channel that was removed. In an address it is
  `?channel_id=<id>`, which **Settings → Channels** links use.
- **All conversations of** *name* in the conversation details shows that customer's conversations in
  every status.

Each active filter shows as a chip under the status tabs; select its **x** to remove it, or **Clear all**
to go back to **Open**. The Filter button counts the filters that are on.

A link can set filters that the menu does not list. They work and show as chips:
`awaiting=customer` (**Waiting on the customer**: the newest message is your team's reply),
`has_pending_draft=true`, `assignee=anyone`, `channel=problem_report`, `source=imported` or
`source=native`, `organization=<exact name, any case>`, and the date ranges `created_after`,
`created_before`, `updated_after` and `updated_before` (UTC instants; the chip shows the days in your
timezone, see [Timezones](timezones.md)).

**Sort** orders the list by **Latest activity** (the default), **Waiting longest** (conversations waiting on
you, the customer who wrote longest ago first, then everything else) or **Priority** (urgent first). Only the
latest-activity order is grouped under Today and Earlier. A link can also set `sort=updated_asc`,
`created_desc` or `created_asc`; the menu then shows that order as chosen.

The filters, the sort and the search are part of the page address, so you can bookmark a list or send its
link to a teammate. Back returns to the list you came from. A value the inbox does not know (an old or
mistyped link) is left out, and the rest of the link still applies. The search runs once you pause typing.

The same filters work in the API (`GET /api/v1/conversations`, see `contracts/openapi.yaml`) and for AI
agents (`colliedesk_inbox_search`, see [MCP](mcp.md)).

## Saved views

**Views** (next to Sort) keeps sets of filters under a name.
- Set the filters, sort and search you want, then under **Save these filters** type a name, choose **Only
  me** or **Everyone in the workspace**, and select **Save view**.
- Select a view to open its filters. The view that matches the list on screen is marked.
- A view's **⋯** menu offers **Replace with current filters**, **Rename**, **Share with the team** (or **Keep
  only for me**) and **Delete**. Anyone may change or delete a shared view; a personal view is seen and
  changed only by its owner. Making a shared view personal gives it to you. If a teammate changed the view
  after you opened the menu, Colliedesk changes nothing and says so; open the menu again and redo the change.
- **Delete** moves the view to the trash; **Undo** in the message brings it back at once. **Recently
  deleted** lists deleted views for 30 days with **Restore**; after that they are gone. Deleting a view
  never changes a conversation.
- Names are unique, ignoring case, among your personal views and among the shared ones. Each person keeps
  at most 50 personal views and a workspace at most 50 shared ones.
- Views are in the [export](export.md) (`saved-views.ndjson`), and AI agents use them through the
  `colliedesk_saved_view_*` tools.

**Know when something new arrives**
- Any page of Colliedesk you have open (including Settings) checks every 20 seconds. The inbox reloads the list by itself when a conversation is created, when a teammate assigns one
  to you, or when any conversation changes (a customer message, a status, assignee or tag change by a teammate).
  A conversation you have open shows a new customer message within about 25 seconds, without a reload.
  Conversations you assign to yourself do not count.
- A customer message on a conversation that is assigned to you, assigned to nobody, or open on your
  screen counts as a new item and can raise a notification. A message on any other teammate's
  conversation updates the screen quietly.
  Your own replies, notes and changes never raise a notification or a count; they only refresh the screen.
- When the tab is in the background, its title shows the number of new items, for example
  `(2) Colliedesk`. The count clears when you return to the tab.
- To also get a desktop notification, tick **Desktop notifications** in **Settings → Preferences**.
  Your browser asks for permission only when you tick it; untick it to stop. The choice is stored in
  this browser only.
- A notification shows the subject and contact name, never message text. More than three new items at
  once become one summary notification. The title of a customer message notification is **New customer message**.
- If your browser blocked notifications for this site, the checkbox is disabled and the section says so;
  allow them in the browser's site settings, then reload the page. The list refresh and the title count keep working.
- Colliedesk does not email agents about new or assigned conversations.

**See who else is on a conversation**
- A row in the list shows the initials of the teammates who have that conversation open right now,
  three at most and then a count. Hover the initials to read the names. **Replying** next to them
  means one of them has a reply to the customer typed and not sent yet: open the conversation before
  you answer, so the customer does not get two replies.
- An AI agent working through an [agent token](sessions.md#agent-tokens) shows as the teammate it acts for,
  "via" the token's name (for example *Lan via Claude Code*).
- Your own tabs are never shown. The marks come with the list's regular refresh, so they can be up to
  20 seconds late; they are there as soon as the inbox opens.
- An open conversation says under its title who else has it open, within about 5 seconds: for
  example *Lan is writing a reply · Ben is viewing*. A reply nobody changed for more than two minutes
  shows as *Lan has a reply not sent yet, untouched since 10:02*: a draft left open in a tab, not
  someone typing now. Times are in your timezone.
- Your tab tells the others the same about you while the conversation is open. A tab in the
  background stops showing you as viewing at once; with a reply typed and not sent it keeps showing
  you as writing, so nobody answers over you. Closing the tab, opening another conversation or
  leaving the inbox takes you off at once; a tab that stops answering (a laptop that sleeps) drops
  off within 15 seconds, or 150 seconds with a reply typed.
- Only the fact that a reply is being written is shared, never its text. Private notes do not count.
- The same list is in the API: the `viewing` field of `GET /api/v1/inbox/activity`, and
  `PUT /api/v1/conversations/{id}/presence` for one conversation.

**Change many conversations at once**
- Tick the box at the left of a row to select it, or tick **Select all on this page**. Ticking a box
  does not open the conversation. With the keyboard, press Tab to reach a box and Space to tick it.
- While at least one row is selected, the **Select all on this page** row becomes an action bar. It
  shows how many rows are selected and a row of icon buttons; hover or focus one to see its name:
  **Resolve** (one press), **Set status**, **Assign to** (**Me**, **Unassigned** or a teammate),
  **Tags** and **Clear selection**. The title, search and filters stay in view. There is no bulk delete.
- With a conversation of a chat in the selection (a Zalo OA, the help center chat), **Tell customers**
  sits before the icons, ticked for each new selection: **Resolve** and **Set status** then tell each
  customer whose channel has words for the new status (see [Telling the customer](#telling-the-customer)).
  Untick it to change them quietly; the buttons' names say **Resolve quietly** and **Set status quietly**.
- In **Tags**, a check mark means every selected row has the tag and a dash means some do. Choosing a
  checked tag removes it from all of them; choosing any other tag adds it to the rows that lack it.
- Press Escape to close an open menu; press it again to clear the selection.
- A choice applies to every selected row at once, up to 50 rows. It is all or nothing: if one change
  cannot be made, no conversation changes, the selection stays and a message says so. Try again.
- After a successful change the selection clears, the list reloads and a line says
  **Updated N conversations**. Every choice has a way back: set the status again, assign again, or
  use the other tag action.
- Only rows on the current page stay selected. Changing the page, the filters or the search drops the
  rows that left the list from the selection.
- The same change is available through the API: `PATCH /api/v1/conversations` with
  `{"ids": [...], "changes": {...}}`. See `contracts/openapi.yaml`.

**Who wrote each message**
- A reply or private note shows the name of the teammate who wrote it (their email when they have no
  display name). A message an [agent token](sessions.md#agent-tokens) wrote or drafted names the teammate and the token instead.
- A received email shows the contact's name, with the sender's address below it. Mail from anyone
  else on the thread, such as a colleague the customer copied, shows that sender's address only.
- An imported message keeps the author name it had in the old helpdesk.

**Reply or take a note**
- **Reply** sends an outgoing message on the timeline. With the [email channel](email-channel.md)
  connected, it is also emailed to the contact and shows **Queued**, **Sent** or
  **Delivery failed** (with **Retry delivery**).
- **Private note** is visible only to your team.
- **Send reply** (or Ctrl/Cmd + Enter) sends what you wrote.
- **Send and resolve**, next to **Send reply** (or Ctrl/Cmd + Shift + Enter), sends the reply and then sets
  the conversation to **Resolved**, in one step. The status changes only after the reply was accepted: if the
  reply is refused, nothing changes and your text stays in the box with the reason. If the reply was sent but
  the status could not be changed, a message says so; press **Resolve** above the thread to try again.
- On a chat channel with words for Resolved, **Send and resolve** also tells the customer; its tooltip
  says so. To resolve quietly, send the reply, then choose **Resolve without telling the customer**.
- A private note has no **Send and resolve**, because a note is often written while the work goes on. Add
  the note, then press **Resolve** above the thread.
- Messages support Markdown.
- **An unsent reply is kept.** What you type stays in this browser for you and that conversation, so a
  reload, the browser's Back or a trip to Settings finds it in the box again. It is removed as soon as you
  send the reply or note. If the send fails, or a reload cuts it off, the text is written back, and the
  next time the conversation opens it is dropped if the reply or note did arrive, so it is never offered
  twice. It also goes seven days after you last opened or edited it, and for every conversation when you sign out, so a
  teammate who signs in on the same browser never sees it. Attached files are not kept. Another browser or
  device does not see it. Two tabs of the same browser share it: when you type in one, the other tab's
  box takes the newer text and says **Updated from another tab**, unless you typed different text in that
  tab too. Then your text stays, and the tab says **Another tab has a different draft** with **Use it**
  and **Keep mine**.
- **One press sends once.** The button is disabled while a message is being sent, and every send carries
  its own key, so pressing twice, or retrying after a lost connection, never posts the same note or reply
  twice. Through the API, send the same `Idempotency-Key` header on every retry of one message.
- **Someone replied while you were writing.** When a teammate's reply (or one you sent from another
  tab, or an AI agent's) reached the customer after you started your reply, Colliedesk does not send
  yours. Your text stays in the box, their reply shows above it with its time (an imported reply keeps
  its own time and says where it came from), and you choose **Send anyway** or **Edit reply**. Either
  way you are asked about those replies once. Private notes never trigger it.
- Approving an AI agent's [reply draft](agent-requests.md) works the same way: when someone replied
  after the newest reply on your screen, the draft is not sent and keeps waiting; read the new reply,
  then send the draft again if it still fits.
- **Cc** (under the message box) copies colleagues of the customer on a reply; see [Copy other people on a reply](email-channel.md#copy-other-people-on-a-reply-cc).
- **Insert saved reply** (or `/`) puts a [saved reply](saved-replies.md) into the box, with the customer's name and yours filled in. You can edit it before you send.
- **Articles** puts a link to a published [knowledge base article](knowledge-base.md#link-an-article-in-a-reply) into the box: its title and its help center address on two lines.
- **Add attachment** (the paperclip under the message box) attaches files to a reply or a private
  note. A message carries at most 10 files and 10 MiB in total. Each chosen file is listed with its
  size; select the **x** next to it to remove it before you send. Over the limit, Colliedesk says so,
  holds the **Send** button and sends nothing until you remove a file. A file you sent stays on the
  message. A reply emails its files to the contact; a private note and its files never leave your team.
- Attachments appear under their message with name and size. Select a file name to download it.
  Files always download; they are never opened inside Colliedesk.

**Start a new conversation**
- Select **New conversation** next to the inbox title.
- **To**: find the customer by name or email, or select **Add a new customer** and enter a name and an
  email address. The new customer is saved together with the conversation: if the email is refused,
  neither is stored. An address that is already a customer says so under the email field, with
  **Use <name>** to pick that customer instead (for a merged address, the customer it was merged into).
- While something is missing, a line next to **Send** says what (a customer, a subject, a message).
- Write a **Subject** and the **Message** (Markdown). **Cc** and **Add attachment** work as in a reply,
  with the same limits.
- **Send** opens the conversation and emails the message from the [email channel](email-channel.md),
  with the subject as you wrote it. The customer's answer joins the same conversation. You land on the
  new conversation, where the message shows **Queued**, **Sent** or **Delivery failed** like any reply.
- If the email is refused, nothing is stored and your text stays in the window with the reason.
- The workspace needs a connected email channel; without one the window says so and cannot send.
- On the hosted cloud trial, email goes only to customers who wrote to you first, so a new conversation
  cannot be sent until you verify your sending domain or choose a plan. The window says so before you
  type, links to Settings and the plans, and takes no input. See [Plan and billing](billing.md).
- AI agents start one with the MCP tool `colliedesk_conversation_start`, or the API
  `POST /api/v1/conversations` with `body` (and `contact: {name, email}` to add the customer with it).

**Change the conversation**
- **Resolve** above the thread sets an open or pending conversation to Resolved in one press, at any
  screen width, without opening the details. A resolved or closed conversation shows **Reopen** there
  instead, which sets it back to Open. If the change fails, a message under the button says so. When the
  change would tell the customer (below), the button reads **Resolve and tell** or **Reopen and tell**,
  and the arrow beside it offers **Resolve without telling the customer** or **Reopen without telling
  the customer** (to add a note or fix a detail).

In the details pane (**Show details** on a narrower screen):
- **Status**: every status can be changed back, for example Resolved → Open to reopen. On a chat that
  tells the customer, Status is a menu: **Tell the customer** comes first, ticked each time it opens, and
  says which statuses its channel tells; untick it and the status you pick is set quietly.
- **Priority**: Urgent, High, Normal, Low.
- **Assignee**: **Me** first, then each teammate, then **Unassigned**.
- **Tags**: see [Tags](tags.md).

**Move messages to another conversation**
A customer's message that landed in the wrong conversation can be moved, with its files: to another
conversation, into a new conversation of its own (a split), or, for a message of a chat channel's group or
chat, back to the [intake queue](channels.md#intake-queue) as a card to be decided again. Only customer
messages move; the team's replies and notes stay where they are. Nothing is sent to the customer.

In the timeline, the menu beside a customer message's time offers **Move to another conversation…**,
**Split into a new conversation…** and, for a chat message, **Take back to Review**. Moving shows the
customer's other conversations first, or finds one by subject, customer or text; tick the other customer
messages to move with it. When every customer message goes, the move can close this conversation as no new
request. A chat conversation nobody replied in or wrote a note in yet also offers **Not a new request?
Move it…** above its messages (closing is still refused once a person assigned it). The moves out of and into a conversation are listed above its
messages, newest first, with who moved what and where, and **Undo** while the move can be undone; a chat
message says why it is in the conversation (a reply to a message here, a command, a person in
[Review](review.md)). A message taken back waits in Review as a card that says who took it back.
- A message taken back to the queue leaves the conversation (it is kept, not deleted). Until its card is
  decided it is in no timeline, search, AI suggestion or reply thread, and a customer's reply quoting it
  does not find that conversation. Deciding the card adds the message to the conversation chosen, or opens
  one with it; setting it aside keeps it out. One card holds the messages of one customer in one place;
  email and web forms have no queue.
- A resolved or closed conversation that gets a moved message reopens, as it does when the customer
  writes again.
- When every customer message of a conversation goes to another and nobody replied in it, wrote a note
  or assigned it yet, the move can also close it as no new request (`close_source`); it then points to
  where its messages went. Once someone worked on it, closing is refused and only the messages move.
- A message moved into a conversation of another customer stays theirs: replies go to the conversation's
  customer only, never suggest copying the writer and never thread on their mail. The move says so
  (`other_contact`), and says when the message came through another channel (`other_channel`).
- A customer's later answer to a moved message follows it to where it went.
- Every move can be undone: the messages go back, a conversation the move closed reopens, a status it
  changed comes back while nobody changed the conversation or wrote in it since, and a conversation a
  split opened closes and points back. Only the newest move of a message can be undone, and a message
  taken back to the queue only while its card waits.

AI agents move messages with the MCP tools `colliedesk_messages_move`, `colliedesk_message_moves_list`
and `colliedesk_message_move_undo`, or the API `POST /api/v1/conversations/{id}/messages/move`
(`message_ids`, `to: {conversation_id}`, `{new: {subject}}` or `{queue: true}`, `close_source`), `GET
/api/v1/conversations/{id}/message-moves` (the moves out of and into a conversation) and `POST
/api/v1/message-moves/{id}/undo`; moving and undoing need the draft tier. Through the API, send the same
`Idempotency-Key` header on every retry of one move or undo: a repeat answers what the first one got.

**Trial and locked workspaces** (hosted cloud only)
During the trial a banner under the workspace name shows the days left. When the trial or the
subscription ends, the inbox is replaced by a page that says so, with the ways forward for admins.
See [Plan and billing](billing.md).

## Telling the customer

- A conversation of a chat (a Zalo OA) tells its customer when its status is set to Pending, Resolved or
  Closed, or reopened, if its channel has words for that status (a Zalo OA's **When a conversation's
  status changes**, see [Zalo OA](zalo.md#when-a-conversations-status-changes)). The words come from the
  channel, as its own message, shown in the timeline as **Sent by the channel · not a reply**. Email and
  web form conversations are never told.
- Every way to set a status has a quiet one: the arrow next to **Resolve** / **Reopen**, unticking **Tell
  the customer** in Status, or **Tell customers** in the bulk bar. The API takes `notify: false` on
  `PATCH /api/v1/conversations/{id}` and on the bulk change.
- When a status was set by something other than a person on your team (an AI agent, the customer
  writing or typing Reopen, the import, or the channel closing it by itself), a quiet line in the
  timeline says who, at the time it happened.
- An AI agent whose token may not write to customers asks a person first when its change would tell
  one; the request shows the words that will be sent ([Agent requests](agent-requests.md)).
