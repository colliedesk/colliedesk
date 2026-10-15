# Work the inbox

The inbox has three panes: the **conversation list** on the left, the **timeline** in the middle
with the composer below it, and **conversation details** on the right. On a narrower screen the
details pane is hidden; open it with **Show details** above the timeline.

A new workspace has no conversations yet. The list then links to the next steps: **Connect your
support email**, **Import from Freshdesk** and **Invite your team** (admins see all three). If the list
cannot be loaded, it says so with **Try again** instead.

**Find conversations**
- Filter by **Status** (Open, Pending, Resolved, Closed), **Assignee** (Any assignee, Assigned to me,
  Unassigned) and **Tag**.
- **Search conversations** matches subject and message text. Characters such as `%` and `_`
  are matched literally.
- When nothing matches, **Clear filters** below the message resets search, status, assignee and
  tag in one step.
- The list is ordered by latest activity and grouped under **Today** and **Earlier**. Use **Previous page** and **Next page** below the list to move; they appear only when there is more than one page.
- Dates from an earlier year show the year, for example imported history.
- The list loads again each time you open the inbox, so mail that arrived meanwhile is there.
  You do not need to reload to see new mail.

**Know when something new arrives**
- Any page of Colliedesk you have open (including Settings) checks every 20 seconds; the inbox reloads the list by itself when a conversation is created or a teammate assigns one
  to you. Conversations you assign to yourself do not count.
- When the tab is in the background, its title shows the number of new items, for example
  `(2) Colliedesk`. The count clears when you return to the tab.
- To also get a desktop notification, select the bell in the left rail: **Enable desktop
  notifications**. Your browser then asks for permission; it never asks before you select the bell.
  Select the bell again (**Desktop notifications are on. Turn off**) to stop them. The choice is
  stored in this browser only.
- A notification shows the subject and contact name, never message text. More than three new items at
  once become one summary notification.
- If your browser blocked notifications for this site, the bell says so; change it in the browser's
  site settings. The list refresh and the title count keep working.
- Colliedesk does not email agents about new or assigned conversations.

**Reply or take a note**
- **Reply** sends an outgoing message on the timeline. With the [email channel](email-channel.md)
  connected, it is also emailed to the contact and shows **Queued**, **Sent** or
  **Delivery failed** (with **Retry delivery**).
- **Private note** is visible only to your team.
- Messages support Markdown.
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
