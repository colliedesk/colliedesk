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
  Your own replies, notes and changes never count.
- When the tab is in the background, its title shows the number of new items, for example
  `(2) Colliedesk`. The count clears when you return to the tab.
- To also get a desktop notification, select the bell in the left rail: **Enable desktop
  notifications**. Your browser then asks for permission; it never asks before you select the bell.
  Select the bell again (**Desktop notifications are on. Turn off**) to stop them. The choice is
  stored in this browser only.
- A notification shows the subject and contact name, never message text. More than three new items at
  once become one summary notification. The title of a customer message notification is **New customer message**.
- If your browser blocked notifications for this site, the bell says so; change it in the browser's
  site settings. The list refresh and the title count keep working.
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

**Reply or take a note**
- **Reply** sends an outgoing message on the timeline. With the [email channel](email-channel.md)
  connected, it is also emailed to the contact and shows **Queued**, **Sent** or
  **Delivery failed** (with **Retry delivery**).
- **Private note** is visible only to your team.
- Messages support Markdown.
- **Insert saved reply** (or `/`) puts a [saved reply](saved-replies.md) into the box, with the customer's name and yours filled in. You can edit it before you send.
- **Add attachment** (the paperclip under the message box) attaches files to a reply or a private
  note. A message carries at most 10 files and 10 MiB in total. Each chosen file is listed with its
  size; select the **x** next to it to remove it before you send. Over the limit, Colliedesk says so,
  holds the **Send** button and sends nothing until you remove a file. A file you sent stays on the
  message. A reply emails its files to the contact; a private note and its files never leave your team.
- Attachments appear under their message with name and size. Select a file name to download it.
  Files always download; they are never opened inside Colliedesk.

**Change the conversation** (details pane)
- **Status**: every status can be changed back, for example Resolved → Open to reopen.
- **Priority**: Low, Normal, High, Urgent.
- **Assignee**: assign to a teammate or set back to unassigned.
- **Tags**: see [Tags](tags.md).

**Trial and locked workspaces** (hosted cloud only)
During the trial a banner under the workspace name shows the days left. When the trial or the
subscription ends, the inbox is replaced by a page that says so, with the ways forward for admins.
See [Plan and billing](billing.md).
