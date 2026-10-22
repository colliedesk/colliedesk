# Work the inbox

The inbox has three panes: the **conversation list** on the left, the **timeline** in the middle
with the composer below it, and **conversation details** on the right. On a narrower screen the
details pane is hidden; open it with **Show details** above the timeline.

A new workspace has no conversations yet. The list then links to the next steps: **Connect your
support email**, **Import from Freshdesk** and **Invite your team** (admins see all three). If the list
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
- Dates from an earlier year show the year, for example imported history.
- The list loads again each time you open the inbox, so mail that arrived meanwhile is there.
  You do not need to reload to see new mail.

## Filter and sort

**Filter** (next to the search box) holds every filter. Ticking several values of one filter shows
conversations with any of them; each filter you add narrows the list further.
- **Priority**: Urgent, High, Normal, Low.
- **Assignee**: Assigned to me, Unassigned, Assigned to anyone, or any teammate.
- **Tags**: conversations with any of the ticked tags, or tick **Has every chosen tag** for all of them.
- **Who replies next**: **Waiting on us** means the newest message that is not a private note came from
  the customer; **Waiting on the customer** means the newest one is your team's reply. A conversation
  with no such message is neither.
- **Needs attention**: **Reply draft waiting for approval** (an [agent request](agent-requests.md) an AI
  agent left) and **Reply delivery failed** (a reply that could not be emailed and was not retried since).
- **Channel**: how the conversation started: **Email**, **Web form**, **Imported** (from another helpdesk),
  **Started by the team** (created in Colliedesk by a teammate or an agent token) or **Problem report**.
  Conversations that existed before channels were recorded got theirs from their first message; one
  without a message from the customer first counts as started by the team.
- **Source**: **Imported** or **Created in Colliedesk**.
- **Organization**: the exact organization name of the customer, in any case.
- **Created** and **Last activity**: a range of days, picked in your timezone (see
  [Timezones](timezones.md)); both ends are included, and **To** cannot be earlier than **From**.
- **All conversations of** *name* in the conversation details shows that customer's conversations in
  every status.

Each active filter shows as a chip under the status tabs; select its **x** to remove it, or **Clear all**
to go back to **Open**. The Filter button counts the filters that are on.

**Sort** orders the list by **Latest activity** (the default), **Oldest activity**, **Newest first**,
**Oldest first** (by creation), **Priority, urgent first**, or **Waiting longest** (conversations waiting on
you, the customer who wrote longest ago first, then everything else). Only the latest-activity order is
grouped under Today and Earlier.

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
- Only the fact that a reply is being written is shared, never its text. Private notes do not count.
- The same list is in the API: the `viewing` field of `GET /api/v1/inbox/activity`.

**Change many conversations at once**
- Tick the box at the left of a row to select it, or tick **Select all on this page**. Ticking a box
  does not open the conversation. With the keyboard, press Tab to reach a box and Space to tick it.
- While at least one row is selected, the **Select all on this page** row becomes an action bar. It
  shows how many rows are selected and a row of icon buttons; hover or focus one to see its name:
  **Resolve** (one press), **Set status**, **Assign to** (**Me**, **Unassigned** or a teammate),
  **Tags** and **Clear selection**. The title, search and filters stay in view. There is no bulk delete.
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
- Messages support Markdown.
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

**Change the conversation** (details pane)
- **Status**: every status can be changed back, for example Resolved → Open to reopen.
- **Priority**: Low, Normal, High, Urgent.
- **Assignee**: assign to a teammate or set back to unassigned.
- **Tags**: see [Tags](tags.md).

**Trial and locked workspaces** (hosted cloud only)
During the trial a banner under the workspace name shows the days left. When the trial or the
subscription ends, the inbox is replaced by a page that says so, with the ways forward for admins.
See [Plan and billing](billing.md).
