# Response targets

Response targets measure how fast conversations are answered and resolved, in business hours: an admin
sets them under Settings → Response targets; every teammate sees them.

1. **Business hours**: a timezone (the workspace's unless you pick another), the hours each weekday is open
   (a day not open is closed; 00:00 to 24:00 is the whole day), and holidays. Time outside them does not
   count. Turned on, they need an open day that is no holiday within the next two years. A day that
   closes at 00:00 on the page closes at midnight (`24:00` in the API). **Copy Monday to weekdays** fills
   Tuesday to Friday with Monday's hours.
2. **Targets by priority**: business time to the first reply and to resolving, for urgent, high, normal
   and low, in hours on the page (in half hours; minutes in the API); leave one empty for no target.
3. **Due soon**: how long before a target a conversation is due soon, when its reminder goes.
4. **Applies to**: the channels whose conversations have targets (every channel unless you pick some).
5. **Alerts**: whether reminder and overdue emails go out (an admin), and whether they reach you (each
   person, while the workspace sends them).
6. Each part saves on its own; agents read them. **Applies to** shows only with two channels or more.

Nothing is measured until you turn them on, and only conversations opened from then on have targets;
imported conversations have none. Turning them off hides every target and keeps what was measured.

For AI agents and the API: `GET /api/v1/response-targets` and `PUT /api/v1/response-targets` (one
document; an agent token asks a person first), MCP `colliedesk_response_targets_get` and
`colliedesk_response_targets_set_request`. A wrong part answers 400 with its path, such as `hours.2.to`.
Send `updated_at` as you read it to save only if nobody saved since: two people saving from the same
read, the second is told (409 `changed_elsewhere`, with what is stored) and changes nothing.

## On each conversation

A conversation opened while the targets are on, on a channel they cover, carries its clocks: the first
reply (met by the team's first reply; a note or the channel's own notice is none) and, when its priority
has one, resolving. A clock runs only while the conversation is Open: Pending pauses it and keeps the
business time used, Resolved or Closed stops it, and opening it again goes on from what was used.
Changing the priority adds or removes the resolution clock at once.

The inbox shows a conversation due soon or overdue: its row says "Due 1:15 PM" or "Overdue" after the
priority (which clock, in business hours, in its tooltip), and counts its tags to stay one line; the
header says the clock in full ("Reply by 1:15 PM", "Resolve overdue"); the details list both clocks under
**Targets**, with the business time left or late, met or paused. Nothing shows while a clock is far from
its target. Times are in your own time zone, named ("6:15 AM UTC") when it is not your browser's; an
inbox left open reads again when a mark is due to change. Filter them with Targets (Overdue, Due soon)
and sort by Due first (both offered once a listed conversation has targets), and save that as a view
(`filters.sla` and `sort: due` of `POST /api/v1/saved-views`, MCP `colliedesk_saved_view_create`). For AI
agents and the API: `sla` on each conversation (`state` overdue, due_soon, ok,
paused, stopped or met; `first_response` and `resolution` with `due_at`, `warn_at`, `met_at`,
`breached_at`, `due_soon`, `overdue` and `business_minutes_left`), `GET /api/v1/conversations?sla=overdue,due_soon`
and `sort=due`, and the same `sla` and `sort` of MCP `colliedesk_inbox_search`. A conversation changed in
the last seconds may reach the filter a few seconds later; it always shows its own clocks right.

## Reminder and overdue emails

When a running clock is due soon, and again when it is overdue, Colliedesk emails the person the
conversation is assigned to, or the admins while nobody is: each email once for each person, even with
several servers and even after the conversation was Pending and opened again (which moves its due time).
Turn yours off under Settings → Response targets (`PATCH /api/v1/agents/me/sla-alerts`, MCP
`colliedesk_my_sla_alerts_set`); an admin turns them off for everyone (`alert_email`). A conversation shows
the emails its clocks sent (`sla.alerts`). They go from the platform address of a server that sends mail
(SMTP and `MAIL_FROM_DOMAIN`, docs/self-host); without one nothing is emailed, and the inbox still shows
what is due. A conversation that came in already past a threshold (an import) is not emailed for it, nor
is a threshold reached while the emails or the targets were off: turning them back on sends nothing late,
and the inbox still shows it overdue.
