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
- Filter by **Status** (All, Open, Pending, Resolved, Closed), **Assignee** (Any assignee, Assigned to me,
  Unassigned) and **Tag**.
- **Search conversations** matches subject and message text, in every status unless you picked a
  status tab first, so imported closed tickets are found; the view stays on **All** after you clear
  the search. Characters such as `%` and `_` are matched
  literally.
- A search for a name or an email address also lists the matching customers under **Customers**,
  above the conversations. Select one to open the customer's page with every conversation they had;
  see [Customers](contacts.md).
- The customer's name under the subject of an open conversation opens the same page.
- A conversation you opened stays on screen after you resolve or close it, although it leaves the
  Open list.
- When nothing matches, **Clear filters** below the message resets search, assignee and tag, and
  returns to **Open**, in one step.
- The list is ordered by latest activity and grouped under **Today** and **Earlier**. Use **Previous page** and **Next page** below the list to move; they appear only when there is more than one page.
- Dates from an earlier year show the year, for example imported history.
- The list loads again each time you open the inbox, so mail that arrived meanwhile is there.
  You do not need to reload to see new mail.

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

**Change many conversations at once**
- Tick the box at the left of a row to select it, or tick **Select all on this page**. Ticking a box
  does not open the conversation. With the keyboard, press Tab to reach a box and Space to tick it.
- An action bar replaces the title and filters while at least one row is selected. It shows how many
  rows are selected and offers **Set status**, **Assign to** (**Me**, **Unassigned** or a teammate),
  **Add tag**, **Remove tag** and **Clear selection**. There is no bulk delete.
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
