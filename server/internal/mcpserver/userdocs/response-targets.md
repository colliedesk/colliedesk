# Response targets

Response targets measure how fast conversations are answered and resolved, in business hours: an admin
sets them under Settings → Response targets; every teammate sees them.

1. **Business hours**: a timezone (the workspace's unless you pick another), the hours each weekday is open
   (a day not open is closed; 00:00 to 24:00 is the whole day), and holidays. Time outside them does not
   count. Turned on, they need an open day that is no holiday within the next two years.
2. **Targets by priority**: business minutes to the first reply and to resolving, for urgent, high, normal
   and low; leave one empty for no target.
3. **Due soon**: how long before a target a conversation is due soon, when its reminder goes.
4. **Applies to**: the channels whose conversations have targets (every channel unless you pick some).
5. **Emails**: whether reminder and overdue emails go out.

Nothing is measured until you turn them on, and only conversations opened from then on have targets;
imported conversations have none. Turning them off hides every target and keeps what was measured.

For AI agents and the API: `GET /api/v1/response-targets` and `PUT /api/v1/response-targets` (one
document; an agent token asks a person first), MCP `colliedesk_response_targets_get` and
`colliedesk_response_targets_set_request`. A wrong part answers 400 with its path, such as `hours.2.to`.
