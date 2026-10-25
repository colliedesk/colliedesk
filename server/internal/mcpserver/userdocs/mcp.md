# Connect an AI agent (MCP)

Colliedesk speaks the Model Context Protocol (MCP), so an AI agent such as Claude Code or Codex can
work your inbox: find conversations, read them, set status, priority, assignee and tags, and leave
private notes for your team. With a `send` token it can also reply to customers. Give an agent the
`send` tier only when you want it to write to customers; a person stays in charge of the rest.

## Endpoint and sign-in

<!-- generated:versions -->
- URL: `https://<your-colliedesk>/mcp`. It uses MCP Streamable HTTP without sessions, protocol
  versions `2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05`.
<!-- /generated -->
- Sign-in: an **agent token** in the `Authorization: Bearer <token>` header. A browser cookie or a
  person's own session token is never accepted on `/mcp`. A missing or invalid token gets HTTP 401.
- Create the token in **Settings → External AI agents**, as described in [Agent tokens](sessions.md#agent-tokens). Pick the lowest tier
  that does the job. The token is shown once; revoke it in **Settings → Sessions**.

| Tier | The agent can |
|---|---|
| `read` | Search the inbox, follow what changed, read conversations, attachments, logged time and customers, list tags and team members, read saved replies with their history and trash, and see the workspace settings, invites, sessions, and the email and web form settings. No answer holds an invite link, a token or the inbound email secret. |
| `draft` | Everything in `read`, plus change status, priority, assignee and tags of one or many conversations (a tag name that does not exist yet is created), add private notes, and set your own timezone. Nothing reaches a customer. |
| `send` | Everything in `draft`, plus reply to customers, retry a failed reply, open conversations, add customers, rename or delete tags, and create, edit, delete and restore saved replies. With `read` or `draft`, a saved reply change is a proposal a person approves. |
| `admin` | Everything in `send`, plus change the workspace name and timezone, deactivate and reactivate agents, revoke invites, revoke agent tokens and its owner's own sign-ins, set up the web form and the email channel, verify the email domain, and read the plan. It acts with the role of the admin who created it. |

A tool used above its tier does not fail: Colliedesk files the call as an
[agent request](agent-requests.md) that a person approves in **Requests**, and the tool answers
with `agent_request` (its id, summary and expiry) and `next`, which tells the model that nothing
changed yet and how to follow it. A reply from a token below `send` becomes a reply draft. Some
actions wait for a person with every tier, `admin` included: new tokens, invites, making someone an
admin or changing an admin, signing out another person, export, billing checkout and portal, disconnecting the email channel, a new
inbound secret and the workspace address. No tool approves or rejects; only a person does.

## Connect your AI host

Each guide gives the shortest way for one host, keeps the token out of plain configuration where
the host allows it, and says how to remove it again:

<!-- generated:hosts -->
- [Claude Code](connect-claude-code.md)
- [Codex](connect-codex.md)
- [Cursor](connect-cursor.md)
- [VS Code](connect-vscode.md)
- [Claude Desktop](connect-claude-desktop.md)
<!-- /generated -->

Then ask the agent, for example: "Triage the open conversations nobody owns: tag billing questions
with `billing` and leave a note with a summary."

## Tools

<!-- generated:tools -->
| Tool | What it does | Tier |
|---|---|---|
| `colliedesk_whoami` | Shows the workspace, the team member, the token tier and the tools you can use | any |
| `colliedesk_inbox_search` | Lists conversations in pages by any [inbox filter](inbox.md#filter-and-sort) or a saved view, in any of the inbox orders | `read` |
| `colliedesk_inbox_activity` | Lists what changed since the last call: new conversations, conversations assigned to you, conversations that changed | `read` |
| `colliedesk_conversation_get` | Reads one conversation with its messages, attachment ids and customer, and who else has it open | `read` |
| `colliedesk_conversation_presence` | Shows the team that the agent is working on a conversation (as "your name via the token label", for 2 minutes), lists who else has it open and warns when someone has a reply not sent yet; `leave` takes it off the list | `read` |
| `colliedesk_time_entries_list` | Lists the time logged on a conversation, with the total | `read` |
| `colliedesk_attachment_get` | Reads an attachment: name, size, type, the text of a plain text file, and a PNG, JPEG, GIF or WebP image up to 2 MiB | `read` |
| `colliedesk_conversation_update` | Sets status, priority, assignee and adds or removes tags in one idempotent call; a tag name that does not exist yet is created | `draft` |
| `colliedesk_conversations_bulk_update` | Sets status, assignee and tags of up to 50 conversations at once, all or nothing | `draft` |
| `colliedesk_note_add` | Adds a private note, with files if needed (up to 10, 5 MiB a call) | `draft` |
| `colliedesk_reply_send` | Sends a reply to the customer by email. Takes `seen_reply_number` (from `colliedesk_conversation_get`): when someone replied since, nothing is sent and their reply is shown; warns when a teammate has a reply open. Takes files (up to 10, 5 MiB a call) and the `ai_draft_id` it was written from. Cannot be undone | `send` |
| `colliedesk_reply_retry` | Sends again a reply whose delivery failed. Cannot be undone | `send` |
| `colliedesk_conversation_create` | Opens a conversation with a customer; sends nothing | `send` |
| `colliedesk_conversation_start` | Opens a conversation and emails its first message to the customer in one step, for a customer found or one added with it (`new_contact`); nothing is stored if the email is refused. Takes files (up to 10, 5 MiB a call). Cannot be undone | `send` |
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
| `colliedesk_kb_order_set` | Sets the order of a collection's collections and articles, or of the top-level collections | `send` |
| `colliedesk_kb_articles_search` | Lists or searches the articles, without their text, a page at a time, each with its public address (`url`) to link in a reply | `read` |
| `colliedesk_kb_article_get` | Reads one article with its whole Markdown text and its public address (`url`), also from the trash | `read` |
| `colliedesk_kb_article_create` | Writes an article, as a draft or published | `send` |
| `colliedesk_kb_article_update` | Edits, moves, publishes or unpublishes an article; every change is a version | `send` |
| `colliedesk_kb_article_delete` | Moves an article to the trash and off the help center. Restorable for 30 days, so not marked destructive | `send` |
| `colliedesk_kb_articles_trash` | Lists the articles in the trash a page at a time | `read` |
| `colliedesk_kb_article_undelete` | Takes an article out of the trash | `send` |
| `colliedesk_kb_article_history` | Lists the versions of an article a page at a time (text cut after 2,000 characters; one version by number comes whole) | `read` |
| `colliedesk_kb_article_version_restore` | Makes the text of an earlier version current again, as a new version | `send` |
| `colliedesk_kb_article_ai_exclude` | Keeps an article away from reply drafts and triage suggestions, in every language; its text does not change | `send` |
| `colliedesk_kb_article_ai_include` | Lets AI read an excluded article again | `send` |
| `colliedesk_kb_image_upload` | Stores an image for articles (base64, at most 5 MiB) and gives the link to put in one | `send` |
| `colliedesk_kb_image_get` | Shows a knowledge base image to the model | `read` |
| `colliedesk_kb_media_list` | Lists the media library: what no article uses (with the space it takes) and images missing alt text | `read` |
| `colliedesk_kb_media_get` | Shows one library item, its thumbnail and the articles that use it | `read` |
| `colliedesk_kb_media_update` | Names an item, writes its alt text, or keeps it from the purge of unused images | `send` |
| `colliedesk_kb_media_trash` | Moves library items to the trash by id, or all unused ones; a lower tier's request names the ids | `send` |
| `colliedesk_kb_media_trash_list` | Lists the library's trash, with when each item is deleted for good | `read` |
| `colliedesk_kb_media_undelete` | Takes items out of the trash, by id or a whole "move to trash" batch | `send` |
| `colliedesk_kb_media_purge` | Deletes items in the trash for good, freeing their storage. Cannot be undone | `admin` |
| `colliedesk_kb_media_replace` | Puts another image or video in place of one in every article that shows it | `send` |
| `colliedesk_kb_media_replace_undo` | Undoes a replacement | `send` |
| `colliedesk_kb_video_upload_start` | Starts uploading a video (MP4 or WebM, up to 200 MiB) in parts | `send` |
| `colliedesk_kb_video_upload_part` | Sends the next part of an upload (up to 5 MiB, base64) | `send` |
| `colliedesk_kb_video_upload_status` | Says where an upload stands, to go on after an interruption | `read` |
| `colliedesk_kb_video_upload_list` | Lists the workspace's uploads in progress, who started each and when it expires | `read` |
| `colliedesk_kb_video_upload_cancel` | Cancels an upload and throws its parts away (one you started; an admin's token, any) | `send` |
| `colliedesk_kb_video_upload_complete` | Checks and stores the uploaded video | `send` |
| `colliedesk_kb_video_poster_set` | Sets a video's poster, the still picture shown before it plays | `send` |
| `colliedesk_help_center_get` | Says whether the public help center is on, its title and language | `read` |
| `colliedesk_help_center_theme_get` | Reads the public help center's logo, colors, fonts, custom CSS and links | `read` |
| `colliedesk_help_center_theme_check` | Checks a candidate brand, custom CSS and color contrast without saving it; can render a page preview | `read` |
| `colliedesk_help_center_search_console_get` | Reads the Google and Bing Search Console verification tokens of the help center | `read` |
| `colliedesk_help_center_freshdesk_redirect_get` | Says how the old Freshdesk help center addresses move and, for a Freshdesk plan that allows it, returns the snippet that redirects them | `read` |
| `colliedesk_help_center_domain_get` | Reads the custom domain, its DNS records and the last check | `read` |
| `colliedesk_token_revoke` | Revokes the token the agent is using. Cannot be undone | any |
| `colliedesk_docs_search` | Searches these user guides | any |
| `colliedesk_reply_draft` | Leaves a reply draft that a person edits, sends or discards; never emails by itself and carries no files | `read` |
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
| `colliedesk_microsoft_connect_request` | Asks an admin to connect Microsoft 365 for sending replies; the approver gets Microsoft's sign-in page | `read` |
| `colliedesk_microsoft_disconnect_request` | Asks to disconnect Microsoft 365 | `read` |
| `colliedesk_workspace_slug_change_request` | Asks to change the workspace address | `read` |
| `colliedesk_problem_report` | Sends a problem report to the team that runs the server, with the request ids of failed calls | `read` |
| `colliedesk_workspace_settings_get` | Shows the workspace name, address and timezone, and your own timezone | `read` |
| `colliedesk_workspace_settings_update` | Sets the workspace timezone | `admin` |
| `colliedesk_workspace_rename` | Renames the workspace (replies never carry this name; it is the help center title while the help center has none) | `admin` |
| `colliedesk_my_timezone_set` | Sets or clears your own timezone | `draft` |
| `colliedesk_my_display_name_set` | Sets or clears your own display name | `draft` |
| `colliedesk_team_member_update` | Renames, deactivates or reactivates an agent; making someone an admin or changing an admin always waits for approval | `admin` |
| `colliedesk_invites_list` | Lists invites and their state, never their links | `read` |
| `colliedesk_invite_create` | Asks to invite a person; the approver sees the link once, the agent never does | `read` |
| `colliedesk_invite_revoke` | Revokes a pending invite. Cannot be undone | `admin` |
| `colliedesk_sessions_list` | Lists sign-ins and agent tokens (the whole workspace with `admin`, otherwise your own) | `read` |
| `colliedesk_session_revoke` | Revokes a token or signs out one of your own devices; signing out another person waits for approval. Cannot be undone | `admin` |
| `colliedesk_help_center_update` | Turns the public help center on or off and sets its title and language | `admin` |
| `colliedesk_help_center_theme_update` | Replaces the public help center's brand after checking its color contrast and custom CSS | `admin` |
| `colliedesk_help_center_search_console_update` | Sets or removes the Google and Bing verification tokens served as meta tags on the help center's pages | `admin` |
| `colliedesk_help_center_domain_set` | Sets or replaces the custom domain, pending DNS ownership proof | `admin` |
| `colliedesk_help_center_domain_check` | Checks DNS ownership and activates the proven custom domain | `admin` |
| `colliedesk_help_center_domain_remove` | Removes the custom domain and restores the slug address | `admin` |
| `colliedesk_web_form_get` | Shows the web form settings | `read` |
| `colliedesk_web_form_update` | Turns the web form on or off and sets the allowed origins | `admin` |
| `colliedesk_email_channel_get` | Shows the email channel, never the inbound secret | `read` |
| `colliedesk_email_channel_update` | Connects the email channel or changes the support address or the sender name | `admin` |
| `colliedesk_email_domain_verify` | Checks the DNS record that verifies the sending domain | `admin` |
| `colliedesk_email_test_message_send` | Sends your own address a test email that opens a conversation you can answer | `draft` |
| `colliedesk_email_forwarding_confirmation_get` | Reads the code and link Gmail sent to confirm forwarding to an email channel | `read` |
| `colliedesk_billing_get` | Shows the plan, trial and seats on the hosted cloud | `admin` |
| `colliedesk_imports_list` | Lists imports | `admin` |
| `colliedesk_import_get` | Shows one import with its progress and why it failed or waits | `admin` |
| `colliedesk_import_preview` | Reads a Freshdesk account before importing it, without writing anything. Takes a Freshdesk admin's API key, which passes through the conversation: refused below `admin` so it never waits in a request | `admin` |
| `colliedesk_import_start` | Starts a previewed import: recent tickets first, then the whole account | `admin` |
| `colliedesk_import_cancel` | Stops an import or discards a preview; what was imported stays | `admin` |
| `colliedesk_import_key_set` | Gives an import waiting for its Freshdesk key the key again | `admin` |
| `colliedesk_import_report` | Reads the reconcile report of an import (JSON or Markdown) | `admin` |
| `colliedesk_import_undo_request` | Asks to remove what an import brought in | `read` |
| `colliedesk_import_people_list` | Lists the agents of an import's Freshdesk account and who each is linked to | `admin` |
| `colliedesk_import_person_link` | Links a Freshdesk agent to a Colliedesk agent; their imported replies and tickets follow | `admin` |
| `colliedesk_import_person_unlink` | Takes back the link between a Freshdesk agent and a Colliedesk agent | `admin` |
| `colliedesk_ai_settings_get` | Shows the [AI settings](ai.md): providers (never a key), each feature's provider and model, and the change history | `read` |
| `colliedesk_ai_models_list` | Lists the models a key may use, before adding the provider; saves and bills nothing. Refused below `admin` so the key never waits in a request | `admin` |
| `colliedesk_ai_provider_add` | Adds an AI provider after a billed check of the first model. The key passes through the conversation: refused below `admin` so it never waits in a request | `admin` |
| `colliedesk_ai_provider_update` | Renames an AI provider or changes its models | `admin` |
| `colliedesk_ai_provider_key_set` | Replaces an AI provider's key after a billed check of the first model. The key passes through the conversation: refused below `admin` so it never waits in a request | `admin` |
| `colliedesk_ai_provider_check` | Runs a check of an AI provider | `admin` |
| `colliedesk_ai_provider_remove` | Deletes an AI provider and erases its key; its features turn off. Cannot be undone | `admin` |
| `colliedesk_ai_slot_set_request` | Asks to set an AI feature: its provider and model, on or off, and what it does by itself by default | `read` |
| `colliedesk_ai_usage_get` | Shows a month of AI usage per feature and provider, and what is left of the included AI | `admin` |
| `colliedesk_ai_automatic_limits_set` | Changes how many automatic AI runs a workspace allows a day and a minute | `admin` |
| `colliedesk_ai_chat_usage_get` | Shows a month of the help center's AI chat against its limits | `admin` |
| `colliedesk_ai_chat_sessions_list` | Lists end users' chats with the help center's AI | `read` |
| `colliedesk_ai_chat_session_get` | Reads one AI chat's transcript with the articles each answer cited | `read` |
| `colliedesk_ai_chat_session_delete_request` | Asks a person to delete an AI chat's transcript for good | `read` |
| `colliedesk_ai_chat_preview` | Asks the help center's AI chat a question as a customer would | `draft` |
| `colliedesk_channel_ai_get` | Shows what each AI feature does by itself on a channel and its scopes | `read` |
| `colliedesk_channel_ai_set` | Sets what an AI feature does by itself on a channel or a scope; a raise waits for a person (admin token) | `admin` |
| `colliedesk_ai_source_consent_request` | Asks to let AI intake send images, a contact's profile or an integration's data | `read` |
| `colliedesk_ai_source_consent_remove` | Stops AI intake sending a kind of extra data (admin token) | `admin` |
| `colliedesk_ai_draft_create` | Asks for an [AI reply draft](ai.md#reply-drafts) of a conversation, with an optional prompt; it is written in the background and never sent | `draft` |
| `colliedesk_ai_draft_get` | Shows an AI draft, waiting up to 20 seconds while it is written | `read` |
| `colliedesk_ai_drafts_list` | Lists a conversation's newest AI drafts | `read` |
| `colliedesk_ai_draft_discard` | Stops an AI draft being written or discards a ready one | `draft` |
| `colliedesk_ai_triage_get` | Shows a conversation's newest [AI triage suggestion](ai.md#triage-suggestions): tags, priority, assignee and a reason | `read` |
| `colliedesk_ai_triage_create` | Asks for a triage suggestion again | `draft` |
| `colliedesk_ai_triage_accept` | Applies a suggestion (all of it, or the parts named) to its conversation | `draft` |
| `colliedesk_ai_triage_dismiss` | Dismisses a suggestion | `draft` |
| `colliedesk_ai_triage_undo` | Takes back what AI applied of a suggestion by itself (all of it, or the parts named); keeps what a person changed since | `draft` |
| `colliedesk_results_get` | Shows the [results page](results.md): your numbers for the last 30 and 7 days next to the history you imported | `read` |
| `colliedesk_results_share_link_get` | Shows whether the results page has an active share link, who made it and when it was last opened; never the link itself | `admin` |
| `colliedesk_results_share_link_create` | Asks a person to create a share link of the results page; the old link stops working | `read` |
| `colliedesk_results_share_link_revoke` | Turns off the results page's share link | `admin` |
| `colliedesk_results_email_set` | Turns your weekly results email on or off (admins only) | `draft` |
| `colliedesk_setup_progress_get` | Shows the four getting-started steps (email, import, invite, first reply) and where each stands | `read` |
| `colliedesk_setup_guide_set` | Hides the [setup guide](setup-guide.md) from your inbox, or shows it again | `draft` |
| `colliedesk_channels_list` | Lists the channels with their state and the channel types the server runs | `read` |
| `colliedesk_channels_get` | Shows one channel and its configuration | `read` |
| `colliedesk_channels_events` | Shows the history of the channels | `read` |
| `colliedesk_channels_create` | Adds a channel (admin token) | `admin` |
| `colliedesk_channels_update` | Renames a channel, turns it on or off, or changes its configuration or rules (admin token) | `admin` |
| `colliedesk_channels_check` | Reads a channel's state again (admin token) | `admin` |
| `colliedesk_channels_held_replay` | Opens the messages a channel held while it was off (admin token) | `admin` |
| `colliedesk_channels_credentials_set` | Stores a channel's secret (admin token only) | `admin` |
| `colliedesk_channels_delete_request` | Asks a person to remove a channel | `read` |
| `colliedesk_channels_held_discard_request` | Asks a person to discard the messages a channel held | `read` |
| `colliedesk_autofix_status` | Shows whether Autofix is set up and how many fixes wait for a person or are running | `read` |
| `colliedesk_fixes_list` | Lists the fixes sent to coding agents, by group (needs a person, running, done) | `read` |
| `colliedesk_fix_get` | Shows one fix: where it is, its issue and pull request, what was sent and its timeline | `read` |
| `colliedesk_conversation_fixes_list` | Lists a conversation's fixes, the latest attempt first | `read` |
| `colliedesk_autofix_settings_get` | Shows Autofix's GitHub installations, connected repositories and coding agents (never a key) | `read` |
| `colliedesk_github_repositories_list` | Lists the repositories a GitHub installation reaches, read from GitHub now, and which are connected | `read` |
| `colliedesk_github_connect_request` | Asks a person to install the Colliedesk GitHub App | `read` |
| `colliedesk_github_disconnect_request` | Asks a person to uninstall the GitHub App from an account; its repositories stay, paused | `read` |
| `colliedesk_autofix_repo_connect` | Connects a private repository for fixes (admin token) | `admin` |
| `colliedesk_autofix_repo_update` | Sets the coding agent a repository suggests (admin token) | `admin` |
| `colliedesk_autofix_repo_disconnect` | Disconnects a repository; its fixes in progress are cancelled (admin token). Cannot be undone | `admin` |
| `colliedesk_autofix_repo_notes_include_request` | Asks a person to let fixes on a repository carry internal notes | `read` |
| `colliedesk_autofix_repo_notes_exclude` | Stops internal notes going with fixes on a repository (admin token) | `admin` |
| `colliedesk_fix_preview` | Builds what sending a conversation to a coding agent would send, masked, without sending | `read` |
| `colliedesk_fix_send_request` | Asks a person to send a conversation to a coding agent; they confirm on GitHub and the issue is theirs | `read` |
| `colliedesk_fix_retry_request` | Asks a person to send a failed or cancelled fix again | `read` |
| `colliedesk_fix_cancel` | Cancels a fix in progress and closes its issue | `draft` |
| `colliedesk_fix_mark_deployed` | Marks a merged fix released | `draft` |
| `colliedesk_fix_unmark_deployed` | Takes back a fix's release | `draft` |
| `colliedesk_fix_pr_link` | Links a pull request Colliedesk did not find to a fix | `draft` |
| `colliedesk_fix_pr_unlink` | Unlinks a pull request a person linked | `draft` |
| `colliedesk_coding_agent_enable_request` | Asks a person to turn on a coding agent for Autofix, with their consent to the data it receives | `read` |
| `colliedesk_coding_agent_disable` | Turns off a coding agent for Autofix; fixes not sent yet are cancelled, fixes already sent go on | `admin` |
| `colliedesk_coding_agent_check` | Checks a coding agent: the GitHub App and private repositories, or Cursor's key and its repositories | `read` |
| `colliedesk_cursor_key_set` | Sets the Cursor key Autofix sends fixes with, after Cursor accepts it. The key passes through the conversation: refused below `admin` so it never waits in a request | `admin` |
| `colliedesk_cursor_key_remove` | Erases the Cursor key; Cursor takes no new fix until one is set again | `admin` |
<!-- /generated -->

Tools marked "Cannot be undone" carry the MCP `destructiveHint`, and their descriptions tell the
model to show you what it will do and wait for your yes, unless you already asked for exactly that.
Many clients also ask you before such a call.

Tools read and change data through the same API as the web app, so a tool can do nothing the token
could not do with the API directly. Another workspace is never visible.

## Operator tools

The people who run a Colliedesk server (operators, see [operator console](../self-host/operator-console.md))
can give an AI client an operator token (`cdop_…`, made in the operator console). It connects to `/mcp/operator`, never to `/mcp`, and a workspace token
never reaches `/mcp/operator`. An operator token reads; every change it asks for waits until an operator
approves it in the console with their authenticator code.

<!-- generated:operator-tools -->
| Tool | What it does | Tier |
|---|---|---|
| `colliedesk_operator_whoami` | Shows the operator the token acts as, what it may ask for and where the console is | `operator` |
| `colliedesk_operator_sign_out` | Ends this operator token. Cannot be undone | `operator` |
| `colliedesk_operator_token_create` | Asks an operator for another operator token | `operator` |
| `colliedesk_operator_status` | Shows the server's release, the configuration each process runs and what needs attention | `operator` |
| `colliedesk_operator_config_get` | Reads every setting, without secrets | `operator` |
| `colliedesk_operator_config_check` | Checks a settings change without saving it and says what saving it would ask | `operator` |
| `colliedesk_operator_config_change` | Asks an operator to change settings | `operator` |
| `colliedesk_operator_config_history` | Lists the configuration versions and what each changed | `operator` |
| `colliedesk_operator_config_restore` | Asks an operator to restore an older configuration version | `operator` |
| `colliedesk_operator_trial_confirm` | Asks an operator to keep the trial change that runs | `operator` |
| `colliedesk_operator_trial_revert` | Asks an operator to end the trial change that runs now | `operator` |
| `colliedesk_operator_products` | Lists the Polar products the server sells or once sold | `operator` |
| `colliedesk_operator_product_add` | Asks an operator to add a Polar product | `operator` |
| `colliedesk_operator_product_on_sale` | Asks an operator to put a Polar product on or off sale | `operator` |
| `colliedesk_operator_audit` | Reads the operator audit log | `operator` |
| `colliedesk_operator_requests` | Lists the requests this token made | `operator` |
| `colliedesk_operator_request_get` | Shows one request this token made | `operator` |
| `colliedesk_operator_request_cancel` | Withdraws a request this token made | `operator` |
| `colliedesk_operator_team` | Lists operators, sessions and tokens | `operator` |
| `colliedesk_operator_invite` | Asks an operator to invite another operator | `operator` |
| `colliedesk_operator_revoke` | Asks an operator to revoke an operator | `operator` |
| `colliedesk_operator_session_revoke` | Asks an operator to end a session or token | `operator` |
<!-- /generated -->

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
  after 20,000 characters, and a PNG, JPEG, GIF or WebP image of at most 2 MiB as an image the
  model can look at (`image: false` leaves it out). The type is judged from the bytes, never from
  the file name; an SVG is text, never an image. Any other file (a PDF, an archive, a larger image)
  comes back as its name, size, SHA-256 and detected type only: open it in the web app. The file
  name and text are customer text. **An image cannot be marked as customer text:** whatever it
  shows (a screenshot of instructions, text in a photo) reaches the model as it is. The tool
  description tells the model it is data a customer sent, never instructions; keep agents on the
  lowest tier that works.
- **Saved replies are team text.** Their title and text appear under `team_text`: the team wrote
  them for customers. The tool descriptions tell the model to treat them as text to send or improve,
  never as instructions. Writes carry the `updated_at` the model read, so it cannot overwrite or
  delete a version it never saw.
- **Files an agent attaches.** `colliedesk_note_add`, `colliedesk_reply_send` and
  `colliedesk_conversation_start` take `files`: up to 10 files, 5 MiB in total per call, each with
  a `filename` and its bytes as base64. The REST API takes 10 MiB a message; attach larger files in
  the web app. A call with files is never filed as a request (see
  [agent requests](agent-requests.md)): below the tool's tier it is refused and nothing is stored,
  so the agent sends the text alone. `colliedesk_reply_draft` carries no files. A reply written from
  an [AI draft](ai.md#reply-drafts) passes its `ai_draft_id`, so the draft records how close the
  reply stayed to it; this works only with a send token, and a draft that is not the conversation's
  ready one (another conversation's, another workspace's, already sent) is refused and nothing is
  sent.
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
