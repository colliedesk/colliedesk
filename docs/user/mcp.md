# Connect an AI agent (MCP)

Colliedesk speaks the Model Context Protocol (MCP), so an AI agent such as Claude Code or Codex can
work your inbox: find conversations, read them, set status, priority, assignee and tags, and leave
private notes for your team. The agent never sends anything to a customer, and a person stays in
charge.

## Endpoint and sign-in

- URL: `https://<your-colliedesk>/mcp`. It uses MCP Streamable HTTP without sessions, protocol
  versions `2025-11-25` and `2026-07-28`.
- Sign-in: an **agent token** in the `Authorization: Bearer <token>` header. A browser cookie is
  never accepted on `/mcp`. A missing or invalid token gets HTTP 401.
- Create the token as described in [Agent tokens](sessions.md#agent-tokens). Pick the lowest tier
  that does the job. The token is shown once; revoke it in **Settings → Sessions**.

| Tier | The agent can |
|---|---|
| `read` | Search the inbox, read conversations, find contacts, list tags and team members. It cannot see invites, the list of sessions or the email and web form settings. |
| `draft` | Everything in `read`, plus change status, priority, assignee and tags (a tag name that does not exist yet is created), and add private notes |
| `send` | Everything in `draft` that a person may do. The tools of this server never send a reply. |

A tool that needs a higher tier answers with a message that names the tier and says an admin can
create a token with it. Nothing else changes.

## Connect Claude Code

```sh
export COLLIEDESK_TOKEN=<your token>
claude mcp add --transport http colliedesk https://desk.example.com/mcp \
  --header "Authorization: Bearer $COLLIEDESK_TOKEN"
```

## Connect Codex

```sh
export COLLIEDESK_TOKEN=<your token>
codex mcp add colliedesk --url https://desk.example.com/mcp --bearer-token-env-var COLLIEDESK_TOKEN
```

Then ask the agent, for example: "Triage the open conversations nobody owns: tag billing questions
with `billing` and leave a note with a summary."

## Tools

| Tool | What it does | Tier |
|---|---|---|
| `colliedesk_whoami` | Shows the workspace, the team member, the token tier and the tools you can use | any |
| `colliedesk_inbox_search` | Lists conversations by status, assignee, tag or text, newest first, in pages | `read` |
| `colliedesk_conversation_get` | Reads one conversation with its messages and customer | `read` |
| `colliedesk_conversation_update` | Sets status, priority, assignee and adds or removes tags in one idempotent call; a tag name that does not exist yet is created | `draft` |
| `colliedesk_note_add` | Adds a private note | `draft` |
| `colliedesk_contacts_find` | Finds customers by part of a name or email; `truncated` says when more matched than were returned | `read` |
| `colliedesk_tags_list` | Lists tags | `read` |
| `colliedesk_agents_list` | Lists team members for assigning | `read` |
| `colliedesk_docs_search` | Searches these user guides | any |

Tools read and change data through the same API as the web app, so a tool can do nothing the token
could not do with the API directly. Another workspace is never visible.

## What the agent sees

- Names, not ids: the assignee, the customer and the tags appear by name. Times are UTC, with the
  workspace timezone next to them.
- `detail` is `concise` (default) or `full`. A concise conversation shows the 20 newest messages
  and cuts long bodies; the result says when it did and asks for `full`.
- Lists come in pages. `next_cursor` is `null` on the last page. A full page can be followed by an
  empty one.
- **Customer text is marked.** Subjects, message bodies, customer names, emails and organizations
  appear only inside a field named `untrusted_customer_content`. The tool descriptions tell the
  model to treat it as data and never as instructions. This lowers the risk that a hostile email
  steers the agent; it does not remove it. Keep agents on the lowest tier that works.
- **Only native team messages count as team text.** A reply or note written in Colliedesk by a
  team member shows its author. A message imported from another helpdesk is marked like customer
  text unless it was a reply or a private note of a team member: a Freshdesk public note or a
  forward can have been written by the customer. A message with no team member as author is marked
  too, author name included.
- **Who wrote it.** When an agent token wrote a note, the message also shows
  `written_by_agent_token` with the label and tier of the token. The server tells the model to read
  such text as model output, not as a decision of the person the token acts as.

## Limits and errors

- A locked workspace (hosted cloud, trial over, nothing paid) answers every tool except `whoami` and
  `docs_search`, which read no workspace data, with a message that only exporting data works.
- A conversation id from another workspace answers "No conversation <id> in this workspace".
- Tag names are matched by their exact spelling first. When no tag has that spelling, the one tag
  that differs only in case is used. If two tags differ only in case (`Refund` and `refund`) and the
  agent gives neither spelling, the call is refused and lists both, so it never acts on the wrong
  one. A name in `add_tags` that no tag has is created, so the agent can keep triaging; a name in
  `remove_tags` that no tag has is an error that lists the real tags. A refused or wrong call creates
  no tag.
- `colliedesk_conversation_update` makes several changes one after another. If one fails after
  another was saved, the error says that some changes may already be applied. Repeat the same call:
  it applies only what is missing, and a tag created by the first try is reused.
- A note an agent writes appears in the conversation as "by <team member> via <token label>", so
  you can tell it from a colleague's note. Status, priority, assignee and tag changes are not marked
  that way yet.
