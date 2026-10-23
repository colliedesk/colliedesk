# Results

**Results** (in the rail) shows how your team did in Freshdesk next to how it does in Colliedesk,
for the last 30 days (the default) or the last 7. Every number is a link: it opens the inbox on the
conversations it counts, in any status, so you can check it. Every role can read the page.

The numbers are worked out in the background, never while you wait: the page says when they were
last updated (usually within the hour). Nothing on the page changes your conversations.

## Where the "before" numbers come from

When a Freshdesk import finishes, Colliedesk works out the same numbers from the history it
imported. That import is the **baseline**:

- The first import whose full run finishes becomes the baseline, and the moment it finished is the
  moment the baseline describes. Running the import again later never moves it: it only fills in
  what was missing, so the before numbers stay the same while you keep Freshdesk running.
- The baseline windows end at the newest ticket a customer opened before that moment. An account
  whose history ends long before the import (a quiet Freshdesk) is measured over its last 30 or 7
  days of tickets.
- Undoing that import removes the baseline. The next import to finish becomes the new one.
- An import that ended **FAILED** shows no before numbers: some tickets did not arrive, so the
  numbers would be wrong. An **UNVERIFIED** import shows its numbers with a label saying so. Fix
  what its report names and run the import again.
- Help Scout imports give a baseline too, without resolve times (Help Scout does not share them).
- Freshdesk shares no reopen history, so there is no "before" reopen rate.

A workspace that imported before this page existed gets its earliest import as the baseline. That
import did not read Freshdesk's resolve times: run it again to fill them in (it changes nothing
else).

## How each number is counted

The same rules apply to both sides, so the two columns can be compared.

**Which conversations.** Only conversations a customer started: the first message that is not a
note came from the customer. Conversations your team started, problem reports and, on the
Colliedesk side, conversations brought in by an import are left out. A conversation counts in a
window by the day it was created. Spam that reached the inbox counts too: there is no way to tell it
apart.

**Measured at a moment.** Each side counts only what had happened by its moment: the Colliedesk
side now, the baseline when its import finished. A reply that came later does not count.

**First response time.** Time from the customer's first message to the first reply your team sent
after it. Notes, automatic replies and forwards are not replies. The page shows the median (half
got a reply faster) and the 90th percentile (90% got one within that time), with how many were
answered and how many still wait for a first reply. Time is calendar time, nights and weekends
included.

**Time to resolve.** Time from when the conversation came in to when it was resolved, counting
only a resolution that stayed resolved (or closed) for 7 days. If it was reopened and resolved
again, the later resolution counts; each conversation counts at most once. Waiting on the customer
(pending) and a customer who went quiet are never resolved. Because a resolution needs those 7 days,
this number uses conversations created a week earlier: for "Last 30 days", conversations created 37
to 7 days ago. The page shows how many were resolved and how many are not resolved or still
settling. On the Freshdesk side the resolve time is the one Freshdesk reports for tickets that were
still resolved or closed when they were read; a ticket without one is counted separately.

**Reopened within 7 days.** Of those resolutions, the share that came back within 7 days: the
customer wrote again (even just "thanks!") or a teammate reopened it.

**Conversations per week.** Conversations in the window divided by its weeks, or by the weeks since
your first conversation if that came later.

**Handle time.** The time your team logged on a conversation in Freshdesk, added up, for the
resolved conversations; the median. Colliedesk does not record time yet
([issue #119](https://github.com/colliedesk/colliedesk/issues/119)).

**Small samples.** A median, a 90th percentile or a reopen rate needs at least 5 conversations; with
fewer the page says "Too few" and how many there are.

**Status history.** Colliedesk records every status change since your server was updated to a
version with this page (or since the workspace was created). A window that needs resolutions from
before that says on which day it will be available.

## AI drafts

For the same window, by the day a person decided: AI drafts sent nearly unchanged (at least 90% the
same as the draft), sent after edits, and discarded. A draft replaced by a newer one, or left until
it expired, is not counted.

**Hours saved** appears only when the baseline has a handle time from at least 5 conversations in
that window. It is an upper bound and the page prints the formula:

> conversations with at least one draft sent nearly unchanged × the median handle time in Freshdesk

It assumes each of those conversations would have taken as long as it did in Freshdesk.

## Open the conversations behind a number

Click a number (or a count under it, such as "8 waiting for a first reply"). The inbox opens with
the filter **Counted in: …** and every status; **Back to Results** returns to the page. The numbers
are worked out again every hour or so and the list belongs to the count you clicked; a count
replaced more than a day ago says "These numbers were refreshed. Open Results again."

## For AI agents

`GET /api/v1/results` returns the numbers (token tier `read`), and `GET /api/v1/conversations?result=…`
the conversations behind one. The MCP tool `colliedesk_results_get` and the `result` argument of
`colliedesk_inbox_search` do the same ([MCP](mcp.md)).
