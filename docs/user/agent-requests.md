# Agent requests (removed)

Colliedesk no longer has agent requests or the **Requests** page. An AI agent acts for the person
who created its token: when the token holds the permission a call needs, the call runs at once; when
it does not, the call is refused with `permission_missing`, which names the permission and how to
get it. See [Roles and permissions](roles-and-permissions.md).

A reply an agent may not send is left as a draft above the reply box, where a person sends, edits or
discards it. Requests made before the change, including the ones still waiting, which were closed as
expired, stay in [the export](export.md) as `agent-requests.ndjson`.
