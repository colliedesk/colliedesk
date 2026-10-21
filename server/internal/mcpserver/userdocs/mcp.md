# Connect an AI agent (MCP)

Colliedesk speaks the Model Context Protocol (MCP), so an AI agent such as Claude Code or Codex can
work your inbox: find conversations, read them, set status, priority, assignee and tags, and leave
private notes for your team. With a `send` token it can also reply to customers. Give an agent the
`send` tier only when you want it to write to customers; a person stays in charge of the rest.

## Endpoint and sign-in

- URL: `https://<your-colliedesk>/mcp`. It uses MCP Streamable HTTP without sessions, protocol
  versions `2025-11-25` and `2026-07-28`.
- Sign-in: an **agent token** in the `Authorization: Bearer <token>` header. A browser cookie or a
  person's own session token is never accepted on `/mcp`. A missing or invalid token gets HTTP 401.
- Create the token as described in [Agent tokens](sessions.md#agent-tokens). Pick the lowest tier
  that does the job. The token is shown once; revoke it in **Settings → Sessions**.

| Tier | The agent can |
|---|---|
| `read` | Search the inbox, follow what changed, read conversations, attachments, logged time and customers, list tags and team members, read saved replies with their history and trash, and see the workspace settings, invites, sessions, and the email and web form settings. No answer holds an invite link, a token or the inbound email secret. |
| `draft` | Everything in `read`, plus change status, priority, assignee and tags of one or many conversations (a tag name that does not exist yet is created), add private notes, and set your own timezone. Nothing reaches a customer. |
| `send` | Everything in `draft`, plus reply to customers, retry a failed reply, open conversations, add customers, rename or delete tags, and create, edit, delete and restore saved replies. With `read` or `draft`, a saved reply change is a proposal a person approves. |
| `admin` | Everything in `send`, plus change the workspace timezone, deactivate and reactivate agents, revoke invites, revoke agent tokens and its owner's own sign-ins, set up the web form and the email channel, verify the email domain, and read the plan. It acts with the role of the admin who created it. |

A tool used above its tier does not fail: Colliedesk files the call as an
[agent request](agent-requests.md) that a person approves in **Requests**, and the tool answers
with `agent_request` (its id, summary and expiry) and `next`, which tells the model that nothing
changed yet and how to follow it. A reply from a token below `send` becomes a reply draft. Some
actions wait for a person with every tier, `admin` included: new tokens, invites, making someone an
admin or changing an admin, signing out another person, export, billing checkout and portal, disconnecting the email channel, a new
inbound secret and the workspace address. No tool approves or rejects; only a person does.

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
| `colliedesk_inbox_search` | Lists conversations in pages by any [inbox filter](inbox.md#filter-and-sort) or a saved view, in any of the inbox orders | `read` |
| `colliedesk_inbox_activity` | Lists what changed since the last call: new conversations, conversations assigned to you, conversations that changed | `read` |
| `colliedesk_conversation_get` | Reads one conversation with its messages, attachment ids and customer | `read` |
| `colliedesk_time_entries_list` | Lists the time logged on a conversation, with the total | `read` |
| `colliedesk_attachment_get` | Reads an attachment: name, size, type, and the text of a plain text file | `read` |
| `colliedesk_conversation_update` | Sets status, priority, assignee and adds or removes tags in one idempotent call; a tag name that does not exist yet is created | `draft` |
| `colliedesk_conversations_bulk_update` | Sets status, assignee and tags of up to 50 conversations at once, all or nothing | `draft` |
| `colliedesk_note_add` | Adds a private note | `draft` |
| `colliedesk_reply_send` | Sends a reply to the customer by email. Cannot be undone | `send` |
| `colliedesk_reply_retry` | Sends again a reply whose delivery failed. Cannot be undone | `send` |
| `colliedesk_conversation_create` | Opens a conversation with a customer; sends nothing | `send` |
| `colliedesk_conversation_start` | Opens a conversation and emails its first message to the customer in one step, for a customer found or one added with it (`new_contact`); nothing is stored if the email is refused. Cannot be undone | `send` |
| `colliedesk_contacts_find` | Finds customers by part of a name or email; `truncated` says when more matched than were returned | `read` |
| `colliedesk_contact_get` | Reads one customer with their conversations (any status, in pages of `limit`), the customer they were merged into and the merges into them | `read` |
| `colliedesk_contact_create` | Adds a customer | `send` |
| `colliedesk_contact_update` | Renames a customer | `send` |
| `colliedesk_contact_merge` | Merges a duplicate customer into the one to keep; undone with `colliedesk_contact_unmerge` | `send` |
| `colliedesk_contact_unmerge` | Undoes a merge: the conversations it moved go back | `send` |
| `colliedesk_tags_list` | Lists tags | `read` |
| `colliedesk_tag_rename` | Renames a tag everywhere | `send` |
| `colliedesk_tag_delete` | Deletes a tag and takes it off every conversation. Cannot be undone | `send` |
| `colliedesk_agents_list` | Lists team members for assigning | `read` |
| `colliedesk_saved_replies_list` | Lists the team's saved replies with who changed each one last | `read` |
| `colliedesk_saved_reply_get` | Reads one saved reply by id or title, also from the trash | `read` |
| `colliedesk_saved_reply_create` | Adds a saved reply | `send` |
| `colliedesk_saved_reply_update` | Edits the title or text of a saved reply; every edit is a version | `send` |
| `colliedesk_saved_reply_delete` | Moves a saved reply to the trash. Restorable for 30 days, so not marked destructive; after that it is gone with its history | `send` |
| `colliedesk_saved_reply_history` | Lists the versions of a saved reply a page at a time: who, through which token, and the text (cut after 2,000 characters; one version by number comes whole) | `read` |
| `colliedesk_saved_reply_version_restore` | Makes an earlier version current again, as a new version | `send` |
| `colliedesk_saved_replies_trash` | Lists the saved replies in the trash a page at a time, with when each is deleted for good | `read` |
| `colliedesk_saved_reply_undelete` | Takes a saved reply out of the trash | `send` |
| `colliedesk_saved_views_list` | Lists the [saved inbox views](inbox.md#saved-views) the token's person can use, or the ones in the trash | `read` |
| `colliedesk_saved_view_create` | Saves inbox filters as a personal or shared view | `draft` |
| `colliedesk_saved_view_update` | Changes the name, the filters or who sees a view | `draft` |
| `colliedesk_saved_view_delete` | Moves a view to the trash for 30 days; changes no conversation | `draft` |
| `colliedesk_saved_view_restore` | Takes a view out of the trash | `draft` |
| `colliedesk_kb_collections_list` | Lists the knowledge base collections (at most three levels deep) with how many articles each holds | `read` |
| `colliedesk_kb_collection_create` | Adds a collection, at the top or inside another | `send` |
| `colliedesk_kb_collection_update` | Renames, reorders or moves a collection | `send` |
| `colliedesk_kb_collection_delete` | Moves an empty collection to the trash for 30 days | `send` |
| `colliedesk_kb_collections_trash` | Lists the collections in the trash | `read` |
| `colliedesk_kb_collection_undelete` | Takes a collection out of the trash | `send` |
| `colliedesk_kb_articles_search` | Lists or searches the articles, without their text, a page at a time | `read` |
| `colliedesk_kb_article_get` | Reads one article with its whole Markdown text, also from the trash | `read` |
| `colliedesk_kb_article_create` | Writes an article, as a draft or published | `send` |
| `colliedesk_kb_article_update` | Edits, moves, publishes or unpublishes an article; every change is a version | `send` |
| `colliedesk_kb_article_delete` | Moves an article to the trash and off the help center. Restorable for 30 days, so not marked destructive | `send` |
| `colliedesk_kb_articles_trash` | Lists the articles in the trash a page at a time | `read` |
| `colliedesk_kb_article_undelete` | Takes an article out of the trash | `send` |
| `colliedesk_kb_article_history` | Lists the versions of an article a page at a time (text cut after 2,000 characters; one version by number comes whole) | `read` |
| `colliedesk_kb_article_version_restore` | Makes the text of an earlier version current again, as a new version | `send` |
| `colliedesk_kb_image_upload` | Stores an image for articles (base64, at most 5 MiB) and gives the link to put in one | `send` |
| `colliedesk_kb_image_get` | Shows a knowledge base image to the model | `read` |
| `colliedesk_kb_media_list` | Lists the media library: what no article uses (with the space it takes) and images missing alt text | `read` |
| `colliedesk_kb_media_get` | Shows one library item, its thumbnail and the articles that use it | `read` |
| `colliedesk_kb_media_update` | Names an item, writes its alt text, or keeps it from the purge of unused images | `send` |
| `colliedesk_kb_media_trash` | Moves library items to the trash by id, or all unused ones; a lower tier's request names the ids | `send` |
| `colliedesk_kb_media_trash_list` | Lists the library's trash, with when each item is deleted for good | `read` |
| `colliedesk_kb_media_undelete` | Takes items out of the trash, by id or a whole "move to trash" batch | `send` |
| `colliedesk_kb_media_purge` | Deletes items in the trash for good, freeing their storage | `admin` |
| `colliedesk_kb_media_replace` | Puts another image or video in place of one in every article that shows it | `send` |
| `colliedesk_kb_media_replace_undo` | Undoes a replacement | `send` |
| `colliedesk_help_center_get` | Says whether the public help center is on, its title and language | `read` |
| `colliedesk_token_revoke` | Revokes the token the agent is using. Cannot be undone | any |
| `colliedesk_docs_search` | Searches these user guides | any |
| `colliedesk_reply_draft` | Leaves a reply draft that a person edits, sends or discards; never emails by itself | `read` |
| `colliedesk_requests_list` | Lists the agent requests this token filed, waiting first | `read` |
| `colliedesk_request_get` | Shows what became of one request: approved (and how the call went), rejected with the reason, cancelled or expired | `read` |
| `colliedesk_request_cancel` | Withdraws a waiting request | `read` |
| `colliedesk_token_create_request` | Asks for a new agent token; the approver gets it, never the agent | `read` |
| `colliedesk_workspace_export_request` | Asks for a workspace export; the approver gets the download | `read` |
| `colliedesk_billing_request` | Asks for a plan checkout or the billing portal | `read` |
| `colliedesk_email_channel_disconnect_request` | Asks to disconnect the email channel | `read` |
| `colliedesk_email_secret_rotate_request` | Asks for a new inbound email secret; the approver sees it | `read` |
| `colliedesk_gmail_connect_request` | Asks an admin to connect Gmail for sending replies; the approver gets Google's consent page | `read` |
| `colliedesk_gmail_disconnect_request` | Asks to disconnect Gmail | `read` |
| `colliedesk_workspace_slug_change_request` | Asks to change the workspace address | `read` |
| `colliedesk_workspace_settings_get` | Shows the workspace name, address and timezone, and your own timezone | `read` |
| `colliedesk_workspace_settings_update` | Sets the workspace timezone | `admin` |
| `colliedesk_my_timezone_set` | Sets or clears your own timezone | `draft` |
| `colliedesk_my_display_name_set` | Sets or clears your own display name | `draft` |
| `colliedesk_team_member_update` | Renames, deactivates or reactivates an agent; making someone an admin or changing an admin always waits for approval | `admin` |
| `colliedesk_invites_list` | Lists invites and their state, never their links | `read` |
| `colliedesk_invite_create` | Asks to invite a person; the approver sees the link once, the agent never does | `read` |
| `colliedesk_invite_revoke` | Revokes a pending invite. Cannot be undone | `admin` |
| `colliedesk_sessions_list` | Lists sign-ins and agent tokens (the whole workspace with `admin`, otherwise your own) | `read` |
| `colliedesk_session_revoke` | Revokes a token or signs out one of your own devices; signing out another person waits for approval. Cannot be undone | `admin` |
| `colliedesk_web_form_get` | Shows the web form settings | `read` |
| `colliedesk_web_form_update` | Turns the web form on or off and sets the allowed origins | `admin` |
| `colliedesk_help_center_update` | Turns the public help center on or off and sets its title and language | `admin` |
| `colliedesk_email_channel_get` | Shows the email channel, never the inbound secret | `read` |
| `colliedesk_email_channel_update` | Connects the email channel or changes the support address | `admin` |
| `colliedesk_email_domain_verify` | Checks the DNS record that verifies the sending domain | `admin` |
| `colliedesk_billing_get` | Shows the plan, trial and seats on the hosted cloud | `admin` |
| `colliedesk_problem_report` | Sends a problem report to the team that runs the server, with the request ids of failed calls | `read` |

Tools marked "Cannot be undone" carry the MCP `destructiveHint`, and their descriptions tell the
model to show you what it will do and wait for your yes, unless you already asked for exactly that.
Many clients also ask you before such a call.

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
- **Attachments.** `colliedesk_attachment_get` returns the text of a plain UTF-8 text file, cut
  after 20,000 characters, and only the name, size, SHA-256 and detected type of any other file
  (an image, PDF or archive). Open those in the web app. The file name and text are customer text.
- **Saved replies are team text.** Their title and text appear under `team_text`: the team wrote
  them for customers. The tool descriptions tell the model to treat them as text to send or improve,
  never as instructions. Writes carry the `updated_at` the model read, so it cannot overwrite or
  delete a version it never saw.
- **Who wrote it.** When an agent token wrote a note or a reply, the message also shows
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
- A note or reply an agent writes appears in the conversation as "by <team member> via <token
  label>", so you can tell it from a colleague's. Status, priority, assignee and tag changes are not
  marked that way yet.
- `colliedesk_conversations_bulk_update` is all or nothing: if one id is not a conversation of the
  workspace, nothing changes. It takes only existing tag names and cannot set priority; use
  `colliedesk_conversation_update` for those.
