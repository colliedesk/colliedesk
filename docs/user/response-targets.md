# Response targets

Response targets measure how fast conversations are answered and resolved, in business hours: someone with
`workspace:update` (an admin) sets them under Settings → Response targets; every teammate sees them. A new
team can also pick one of three presets in the [setup flow](setup-guide.md#setup-flow) (**Set reply
targets**): it saves the preset's targets, turns them on with business hours Monday to Friday, 09:00 to
18:00 (or the hours already set here, which it keeps), and sets due soon to 30 minutes.

1. **Business hours**: a timezone (the workspace's unless you pick another), the hours each weekday is open
   (a day not open is closed; 00:00 to 24:00 is the whole day), and holidays. Time outside them does not
   count. Turned on, they need an open day that is no holiday within the next two years. A day that
   closes at 00:00 on the page closes at midnight (`24:00` in the API). **Copy Monday to weekdays** fills
   Tuesday to Friday with Monday's hours. A new workspace's timezone is the browser zone of the person
   who created it, from their first sign-in ([timezones](timezones.md)), so business hours count in the
   team's clock, not in `UTC`. Hours that use the workspace's timezone follow it when an admin changes it
   in Settings → General. Dates in Reports and the inbox show in your own timezone, which can differ
   from the one the hours count in: the page then says "Hours count in `UTC`; your dates show
   `Asia/Saigon`", and the first choice of the list is your timezone.
2. **Targets by priority**: business time to the first reply and to resolving, for urgent, high, normal
   and low. Each box is a number and a unit, `min` or `h`: any whole number of minutes from 1 up to a
   year (525600 minutes, 8760 hours), such as 15 min or 1.5 h (90 minutes); leave one empty for no
   target. The page shows whole and half hours in hours and anything else in minutes (60 is `1 h`, 90 is
   `1.5 h`, 510 is `8.5 h`, 15 is `15 min`, 45 is `45 min`). The API takes minutes.
3. **Due soon**: how long before a target a conversation is due soon, when its reminder goes: minutes
   or hours the same way, up to a week (10080 minutes, 168 hours), and 0 for no reminder before the
   target.
4. **Applies to**: the channels whose conversations have targets (every channel unless you pick some).
5. **Alerts**: whether reminder and overdue emails go out (an admin), and whether they reach you (each
   person, while the workspace sends them).
6. Each part saves on its own; agents read them. **Applies to** shows only with two channels or more.

Nothing is measured until you turn them on, and only conversations opened from then on have targets;
imported conversations have none. Turning them off hides every target and keeps what was measured.

While they are off, Settings → Response targets is one card, **Response targets are off**, with no hours,
holidays or targets to edit, so nothing looks live that is not. Under it, one line says what turning them
on starts with: business hours Monday to Friday, 09:00 to 18:00 in the workspace's timezone, and the
first-reply targets of the preset the [setup flow](setup-guide.md#setup-flow) would pick (**Same day**:
Urgent 1 h, High 2 h, Normal 8 h, Low 1 day), with **Due soon** at 30 minutes. It is the same hours and
the same targets the setup flow saves. **Turn on response targets** saves them and starts measuring in
one step, so the clock never runs against a week with no open day. Business hours or targets saved
earlier are never replaced: the line then names them ("Starts with your saved hours: …") and turning on
keeps them, and a reminder saved as none stays none. If the server refuses the turn-on because something saved is no longer valid (a channel in **Applies to** that was deleted, holidays that close every open day), the page opens the editor under the card, still off, with the server's words beside the field: fix it, then press the button again. A deleted channel is listed ticked as "Deleted channel"; nothing is dropped from the scope unless you untick it. Late conversations show in the inbox (the row marks and the Targets filter). Once they are
on, the page is the editor below, and the **Measure response targets** switch turns them off again with
every saved value kept.

For AI agents and the API: `GET /api/v1/response-targets` and `PUT /api/v1/response-targets` (one
document; an agent token asks a person first), MCP `colliedesk_response_targets_get` and
`colliedesk_response_targets_set`. A wrong part answers 400 with its path, such as `hours.2.to`.
Send `updated_at` as you read it to save only if nobody saved since: two people saving from the same
read, the second is told (409 `changed_elsewhere`, with what is stored) and changes nothing.

## On each conversation

A conversation opened while the targets are on, on a channel they cover, carries its clocks: the first
reply (met by the team's first reply; a note or the channel's own notice is none) and, when its priority
has one, resolving. A clock runs only while the conversation is Open: Pending pauses it and keeps the
business time used, Resolved, Closed or [Spam](inbox.md#spam) stops it, and opening it again goes on from what was used.
Changing the priority adds or removes the resolution clock at once.

The inbox shows when a reply is due while a clock runs: its row says "Due 1:15 PM", and once the target
passed, how late it is in red ("12 min late"; which clock, in business hours, in its tooltip). The header
says it in full ("Reply by 1:15 PM", "Reply was due 11:10 AM"); the details have one **Due** row with the
same words, and its menu lists both clocks under **Targets**, with the business time left or late, met or
paused. Times are in your own time zone, named ("6:15 AM UTC") when it is not your browser's; an
inbox left open reads again when a mark is due to change. Filter them with Targets (Overdue, Due soon)
(offered once a listed conversation has targets); the inbox is ordered Due first by default, and you can save that as a view
(`filters.sla` and `sort: due` of `POST /api/v1/saved-views`, MCP `colliedesk_saved_view_create`). For AI
agents and the API: `sla` on each conversation (`state` overdue, due_soon, ok,
paused, stopped or met; `first_response` and `resolution` with `due_at`, `warn_at`, `met_at`,
`breached_at`, `due_soon`, `overdue` and `business_minutes_left`), `GET /api/v1/conversations?sla=overdue,due_soon`
and `sort=due`, and the same `sla` and `sort` of MCP `colliedesk_inbox_search`. A conversation changed in
the last seconds may reach the filter a few seconds later; it always shows its own clocks right.

## Reminder and overdue emails

When a running clock is due soon, and again when it is overdue, Colliedesk emails the person the
conversation is assigned to, or, while nobody is, everyone who can set response targets (`workspace:update`, the admins): each email once for each person, even with
several servers and even after the conversation was Pending and opened again (which moves its due time).
Turn yours off under Settings → Response targets (`PATCH /api/v1/agents/me/sla-alerts`, MCP
`colliedesk_my_sla_alerts_set`); an admin turns them off for everyone (`alert_email`). A conversation shows
the emails its clocks sent (`sla.alerts`). They go from the platform address of a server that sends mail
(SMTP and `MAIL_FROM_DOMAIN`, [self-host email](../self-host/email.md)); without one nothing is emailed, and the inbox still shows
what is due. A conversation that came in already past a threshold (an import) is not emailed for it, nor
is a threshold reached while the emails or the targets were off: turning them back on sends nothing late,
and the inbox still shows it overdue.
