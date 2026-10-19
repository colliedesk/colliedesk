# Agent requests and reply drafts

An AI agent works with an agent token, and the token's scope limits what it may do alone (see
[Agent tokens](sessions.md#agent-tokens)). When the agent tries something above its scope, Colliedesk
does not refuse it. It saves the call as an **agent request**, and nothing changes until a person
approves it. A reply from an agent that may not send becomes a **reply draft**: a person reads it,
edits it if needed, and sends it or throws it away.

## What becomes a request

- **Anything above the token's scope.** A `read` token that changes a status, a `draft` token that
  replies to a customer, renames a tag or edits a saved reply. The agent gets an answer at once that
  says the request is waiting; it is never a silent change.
- **Actions that always need a person**, whatever the scope, `admin` included: creating an agent
  token, creating an invite, making a teammate an admin or changing an admin (demoting,
  deactivating, reactivating), signing out another person's device, exporting all workspace data
  (or a download link for it), starting a plan checkout or opening the billing portal, disconnecting
  the email channel, rotating the inbound email secret, connecting or disconnecting Gmail (the
  approver gets Google's consent page), and changing the workspace address.

A settings change shows what changes, from what to what, for example "Change the workspace
timezone: UTC → Asia/Tokyo" or "Change ada@example.com: role agent → admin". A settings call with a
field the setting does not take is refused at once and nothing is saved, so a password pasted next
to the real fields never sits in a request.

A call that carries files cannot wait: the files would sit in storage for days. The agent is asked
to send the text alone (it becomes a request) and attach the files after approval.

A token may file 100 requests in any hour and have 50 waiting at once; beyond that it is told to
wait. In a locked workspace (hosted cloud, trial over) nothing is filed.

## Approve or reject

Open **Requests** in the left rail. The number on it counts the requests that wait. Each request
shows what it does, who asked and through which token (for example "Asked by Ada via Claude
Code (read)"), the exact call it will make and the data it sends.

- **Approve** runs the call once, **as you**: your role and permissions apply. A request only an
  admin may run (billing, export, new tokens, the workspace address, team, invites and channel
  settings) shows "Only an admin can
  approve this" to everyone else, and keeps waiting for an admin. A request that changes the token
  owner's own settings (their timezone) can be approved only by that person, because it runs as the
  person who approves. If the call is refused anyway
  (for example the trial's sending limits), the request is marked **Failed**. Two clicks, or two
  people at once, still run it once. Closing the tab after you clicked does not stop it.
- **Reject** refuses it, with a reason the agent can read if you write one. A rejected request
  never runs.
- When the approved call answers with a new token, a secret, a download link or a payment page,
  you see it once, on the request. Copy what you need: it is not stored, and the agent never sees it.
  Approving an export gives you the download link.

A request waits seven days, then expires and can no longer be approved. 90 days after a request is
decided, Colliedesk deletes the call it carried (and an edited draft); the record of who asked,
what for, and who decided stays. The agent can withdraw its
own request before anyone decides. A request of a token that was revoked can only be rejected.

A proposed change to a [saved reply](saved-replies.md#ai-agents) shows the reply before and after
instead of the raw call, and says when the reply changed after the agent asked.

## Reply drafts

When an agent drafts a reply, the conversation is marked **Draft waiting** in the inbox. Open it:
the draft sits above the reply box with the name of the token that wrote it and every recipient:
the customer and anyone it copies (Cc). The same recipients are in the request's summary.

- **Send** emails it to the customer, as you.
- **Edit** lets you change the text and untick copies first; Send then sends your version. You can
  remove a copy, never add one: to write to someone new, send your own reply.
- **Discard** throws it away. The customer never sees it.

An agent whose token is below `send` may copy only the customer and people already on the
conversation (who wrote in or were copied before); any other address is refused when the draft is
filed. A `send` token may copy a new address, and you see it before you press Send.

The sent reply shows "requested by <token label>, approved by <you>" in the conversation. If you
changed the words, it shows "from an agent draft, edited and sent by <you>" instead: the text is
yours. The request keeps both the agent's draft and your edit. A note
from a `draft` token is still added at once, as before: notes never reach a customer.

An agent with a `send` token can also ask for a draft on purpose (MCP tool `colliedesk_reply_draft`)
when a person should read the words first.

## For agents and scripts

- The API answers `202 Accepted` with `request_id`, `status`, `summary` and `expires_at` instead of
  the usual result. `GET /api/v1/agent-requests/{id}` shows what became of it, and
  `POST /api/v1/agent-requests/{id}/cancel` withdraws it. A token sees only its own requests.
- MCP tools return the request as their result, with what to do next. `colliedesk_requests_list`,
  `colliedesk_request_get` and `colliedesk_request_cancel` follow it. See [MCP](mcp.md).
- Approving and rejecting are for people only. No token can do either, so an agent can never
  approve its own request.
- A token may have 50 requests waiting at once.
