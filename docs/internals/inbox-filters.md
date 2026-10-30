# Inbox filters and saved views

Decisions and traps behind `GET /api/v1/conversations` filters, `conversations.channel` and
`saved_views` (migration `20261019120000-inbox-filters`).

## The list query

`listConversations` (`server/internal/store/conversations.go`) builds one `WHERE` from the filter:
every value is a bind parameter, only fixed SQL text is concatenated. Messages are read only through
`EXISTS` when a filter needs them (search, `delivery_failed`); there is no join over every message of
the workspace. The second query hydrates the page through `unnest($ids) WITH ORDINALITY`, so its order
is the order of the first query whatever the sort. Every sort ends with `id`, so pages never repeat or
skip a row while nothing changes. A value the server does not know is a 400, never an empty page: a
typo must not look like "no conversations". Budget: `TestInboxListLatencyBudget` runs the new filters
and sorts on 5,000 conversations at 100 ms p95 (measured 2 to 16 ms locally).

## Who replies next

`awaiting=team` means the newest message that is not a note is `incoming`. It is kept in two columns,
`conversations.last_public_kind` and `last_public_at`, by statement-level triggers on `messages`
(insert, delete, update of kind, time or conversation). The triggers recompute the newest message,
never increment: the Freshdesk import writes old messages in any order. They never touch
`updated_at`, which is the team's activity. A trigger, not the writers, keeps the columns, so code
older than the migration (a rollback) keeps them right too. Two writers on one conversation: the refresh locks
the conversation rows first (`FOR NO KEY UPDATE`, the lock the UPDATE takes anyway, so foreign key checks of
other inserts are not blocked), ordered by workspace and id like the bulk update, and reads the newest message
in the next statement, whose snapshot sees what the other writer committed. Reading and updating in one
statement kept that statement's older snapshot and overwrote a newer customer message (reproduced in the
migration test with two interleaved transactions). Reading the newest message per row at
query time was rejected: `sort=waiting_longest` would read it for every conversation of the workspace.

## Channel

`conversations.channel` is one of `email`, `web_form`, `imported`, `agent`, `problem_report`. Every
writer sets it (email inbound, web form, problem report, Freshdesk import, `POST /conversations`, which
is `agent`). The column default is `agent` only for an older image after a rollback, which does not
know the column; its new conversations read as started by the team. Gmail sending (issue #95) is a way
out of the email channel, not a channel.

Backfill, from what each row already holds, in this order: an `origin` is `imported`; otherwise the
first message decides: not incoming (or none) is `agent`; `email_meta.via` `web_form` or
`problem_report` is that channel; an incoming message without `email_meta` and without
`email_message_id` is `web_form`, because the web form stored its messages without metadata before
`via` existed while mail and problem reports always had metadata; any other incoming message is
`email`. Nothing is guessed from tags or subjects: a person can remove the web-form tag. The down
migration drops the column; it is derived, and the next upgrade derives it again the same way.
`TestInboxFiltersMigrationBackfillsChannelAndWaitingStateWithoutLosingRows` seeds each case.

## Saved views

`saved_views` rows are `personal` (an owner, removed with that agent) or `shared` (the workspace), with
a CHECK that ties `scope` to `owner_agent_id`, so a personal view can never turn shared by losing its
owner. `filters` is the `ConversationFilters` object of the contract, checked by the same parser as
the list query, so a saved view always opens. Another person's personal view does not exist for the
caller (404, never 403). Delete is a trash for 30 days with restore; a delete or a restore repeated
answers the same view. A change (rename, filters, scope) must send the `updated_at` it read: a view saved
since answers 409 `changed_elsewhere`, as saved replies do. There is no version history: a view is a few
filters, and a delete waits in the trash. The down migration refuses while any view exists, as for saved reply history.

Token tier: every write is `draft`. A view changes no conversation and nothing reaches a customer;
renaming or deleting a tag is `send` because it rewrites every conversation that carries the tag.

## Web app

The filters are in the address (`src/lib/inbox-query.ts`), with the API's names and comma lists. No
`status` is the Open view and `status=all` is every status, so the default address stays `/`. Values the
inbox does not know are dropped when the address is read, so a stale link never turns into a 400. The
search box keeps what is typed locally (`DraftInput`) and sends 100 ms after the last key; a newer search
cancels the request of the one it replaced.

The Filter menu offers fewer controls than the address takes. An agent narrows the inbox many times a day,
so the menu keeps the choices that triage needs and the rest stay link-only: `awaiting=customer`,
`has_pending_draft` (the Requests page owns drafts waiting for approval), `assignee=anyone`, `source`,
`organization`, `channel=problem_report` (it reaches only the operator's workspace) and the date ranges
(reporting, which Results and Export cover). A link that sets one still filters, counts on the Filter
button and shows a chip that removes it; a channel value or a sort the menu does not offer stays listed
while it is chosen, so the state is visible. `awaiting=team` is one checkbox, **Waiting on us**. The
signed-in agent is the `me` value, labelled Me, and is not listed again under their name. A channel type
(email, web form, help chat) is offered once the channel list has a channel of that type, removed ones
included, since their conversations remain. A date chip shows the days of a `*_after` / `*_before` pair in
the agent's timezone; the end day is the day before the exclusive `*_before`.

Bundle: the first bundle carries only the search box, the status tabs, reading the address and building
the API query. The Filter and Sort menus and the chips (`filter-panel.tsx`) and the Views menu with its
data (`saved-views-menu.tsx`) load as their own chunks through `ui/lazy-part.tsx`, which shows inert
controls of the same size meanwhile and a reload link if a chunk fails; views are fetched only when the
menu opens and the trash when its section opens.
