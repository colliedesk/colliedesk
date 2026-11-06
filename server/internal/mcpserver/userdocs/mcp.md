# Connect an AI agent (MCP)

Colliedesk speaks the Model Context Protocol (MCP), so an AI agent such as Claude Code or Codex can
work your inbox: find conversations, read them, set status, priority, assignee and tags, and leave
private notes for your team. With `messages:send` it can also reply to customers. Give an agent a
token that holds `messages:send` only when you want it to write to customers.

## Endpoint and sign-in

<!-- generated:versions -->
- URL: `https://<your-colliedesk>/mcp`. It uses MCP Streamable HTTP without sessions, protocol
  versions `2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05`.
<!-- /generated -->
- Sign-in: an **agent token** in the `Authorization: Bearer <token>` header. A browser cookie or a
  person's own session token is never accepted on `/mcp`. A missing or invalid token gets HTTP 401.
- Create the token in **Settings → External AI agents**, as described in
  [Agent tokens](sessions.md#agent-tokens): it follows a role (one whose permissions you all hold), or holds a fixed
  list of your permissions. Give it only what the job needs. The token is shown once; revoke it in
  **Settings → Sessions**.

The agent does what the token's [permissions](roles-and-permissions.md) allow, at once; nothing waits
for a person to approve it, and it never does more than the person who made the token can do right
now. The **Needs** column below names the permissions each tool needs. `colliedesk_whoami` answers
your name and roles, how the token was made and its permissions.

A tool the token has no permission for fails with the message, `missing_permissions` and
`missing_from` of [a refused call](roles-and-permissions.md#when-a-permission-is-missing): `token`
means you hold the permission and a token that includes it would work, `person` that you would need a
role that holds it. An invite, a new inbound email secret, an export link or a results share link is
returned to the agent so it can give it to you. Connecting Gmail, Microsoft 365 or GitHub, sending a
fix through GitHub, and billing checkout and portal answer an address only you can finish, in a
browser signed in to Colliedesk. Creating a token is for people only: no tool makes one.

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
| Tool | What it does | Needs |
|---|---|---|
| `colliedesk_whoami` | Shows the workspace, the team member and their roles, the token's permissions and the tools you can use | any token |
| `colliedesk_inbox_search` | Lists conversations in pages by any [inbox filter](inbox.md#filter-and-sort) or a saved view, in any of the inbox orders | `contacts:read`, `conversations:read`, `saved_views:read`, `tags:read`, `team:read` |
| `colliedesk_inbox_activity` | Lists what changed since the last call: new conversations, conversations assigned to you, conversations that changed | `conversations:read`, `team:read` |
| `colliedesk_conversation_get` | Reads one conversation with its messages, attachment ids and customer, and who else has it open | `contacts:read`, `conversations:read`, `messages:read`, `team:read` |
| `colliedesk_conversation_presence` | Shows the team that the agent is working on a conversation (as "your name via the token label", for 2 minutes), lists who else has it open and warns when someone has a reply not sent yet; `leave` takes it off the list | `conversations:read` |
| `colliedesk_time_entries_list` | Lists the time logged on a conversation, with the total | `conversations:read`, `team:read` |
| `colliedesk_attachment_get` | Reads an attachment, or a file of a reply draft (with `conversation_id` and `draft_id`): name, size, type, the text of a plain text file, and a PNG, JPEG, GIF or WebP image up to 2 MiB | `ai_assist:read`, `messages:read` |
| `colliedesk_conversation_update` | Sets status, priority, assignee and adds or removes tags in one idempotent call; a tag name that does not exist yet is created | `conversations:update`, `tags:create`, `tags:read`, `team:read` |
| `colliedesk_conversations_bulk_update` | Sets status, assignee and tags of up to 50 conversations at once, all or nothing | `conversations:update`, `tags:read`, `team:read` |
| `colliedesk_conversations_spam` | Moves up to 50 conversations into Spam with their senders (their later mail goes there too; no clock, not in Results), or back where they were with `spam` false; all or nothing | `conversations:update` |
| `colliedesk_note_add` | Adds a private note, with files if needed (up to 10, 5 MiB a call) | `messages:note` |
| `colliedesk_reply_send` | Sends a reply to the customer by email. Takes `seen_reply_number` (from `colliedesk_conversation_get`): when someone replied since, nothing is sent and their reply is shown; warns when a teammate has a reply open. Takes files (up to 10, 5 MiB a call) and the `ai_draft_id` it was written from; without `ai_draft_file_ids` every file of that draft is sent. Cannot be undone | `conversations:read`, `messages:note`, `messages:read`, `messages:send`, `team:read` |
| `colliedesk_reply_retry` | Sends again a reply whose delivery failed. Cannot be undone | `messages:send` |
| `colliedesk_conversation_create` | Opens a conversation with a customer; sends nothing | `messages:send` |
| `colliedesk_conversation_start` | Opens a conversation and emails its first message to the customer in one step, for a customer found or one added with it (`new_contact`); nothing is stored if the email is refused. Takes files (up to 10, 5 MiB a call). Cannot be undone | `messages:read`, `messages:send` |
| `colliedesk_contacts_find` | Finds customers by part of a name or email; `truncated` says when more matched than were returned | `contacts:read` |
| `colliedesk_contact_get` | Reads one customer with their conversations (any status, in pages of `limit`), the customer they were merged into and the merges into them | `contacts:read`, `conversations:read`, `team:read` |
| `colliedesk_contact_create` | Adds a customer | `contacts:create` |
| `colliedesk_contact_update` | Renames a customer | `contacts:update` |
| `colliedesk_contact_merge` | Merges a duplicate customer into the one to keep; undone with `colliedesk_contact_unmerge` | `contacts:update` |
| `colliedesk_contact_unmerge` | Undoes a merge: the conversations it moved go back | `contacts:update` |
| `colliedesk_tags_list` | Lists tags | `tags:read` |
| `colliedesk_tag_rename` | Renames a tag everywhere | `tags:read`, `tags:update` |
| `colliedesk_tag_delete` | Deletes a tag and takes it off every conversation. Cannot be undone | `tags:delete`, `tags:read` |
| `colliedesk_agents_list` | Lists team members for assigning | `team:read` |
| `colliedesk_saved_replies_list` | Lists the team's saved replies with who changed each one last | `saved_replies:read` |
| `colliedesk_saved_reply_get` | Reads one saved reply by id or title, also from the trash | `saved_replies:read` |
| `colliedesk_saved_reply_create` | Adds a saved reply | `saved_replies:create` |
| `colliedesk_saved_reply_update` | Edits the title or text of a saved reply; every edit is a version | `saved_replies:update` |
| `colliedesk_saved_reply_delete` | Moves a saved reply to the trash. Restorable for 30 days, so not marked destructive; after that it is gone with its history | `saved_replies:delete` |
| `colliedesk_saved_reply_history` | Lists the versions of a saved reply a page at a time: who, through which token, and the text (cut after 2,000 characters; one version by number comes whole) | `saved_replies:read` |
| `colliedesk_saved_reply_version_restore` | Makes an earlier version current again, as a new version | `saved_replies:update` |
| `colliedesk_saved_replies_trash` | Lists the saved replies in the trash a page at a time, with when each is deleted for good | `saved_replies:read` |
| `colliedesk_saved_reply_undelete` | Takes a saved reply out of the trash | `saved_replies:delete` |
| `colliedesk_saved_views_list` | Lists the [saved inbox views](inbox.md#saved-views) the token's person can use, or the ones in the trash | `saved_views:read` |
| `colliedesk_saved_view_create` | Saves inbox filters as a personal or shared view | `saved_views:create` |
| `colliedesk_saved_view_update` | Changes the name, the filters or who sees a view | `saved_views:update` |
| `colliedesk_saved_view_delete` | Moves a view to the trash for 30 days; changes no conversation | `saved_views:delete` |
| `colliedesk_saved_view_restore` | Takes a view out of the trash | `saved_views:delete` |
| `colliedesk_kb_collections_list` | Lists the knowledge base collections (at most three levels deep) with how many articles each holds | `kb_articles:read` |
| `colliedesk_kb_collection_create` | Adds a collection, at the top or inside another | `kb_articles:create` |
| `colliedesk_kb_collection_update` | Renames, reorders or moves a collection | `kb_articles:update` |
| `colliedesk_kb_collection_delete` | Moves an empty collection to the trash for 30 days | `kb_articles:delete` |
| `colliedesk_kb_collections_trash` | Lists the collections in the trash | `kb_articles:read` |
| `colliedesk_kb_collection_undelete` | Takes a collection out of the trash | `kb_articles:delete` |
| `colliedesk_kb_order_set` | Sets the order of a collection's collections and articles, or of the top-level collections | `kb_articles:update` |
| `colliedesk_kb_articles_search` | Lists or searches the articles, without their text, a page at a time, each with its public address (`url`) to link in a reply | `kb_articles:read` |
| `colliedesk_kb_article_get` | Reads one article with its whole Markdown text and its public address (`url`), also from the trash | `kb_articles:read` |
| `colliedesk_kb_article_create` | Writes an article, as a draft or published | `kb_articles:create` |
| `colliedesk_kb_article_update` | Edits, moves, publishes or unpublishes an article; every change is a version | `kb_articles:update` |
| `colliedesk_kb_article_delete` | Moves an article to the trash and off the help center. Restorable for 30 days, so not marked destructive | `kb_articles:delete` |
| `colliedesk_kb_articles_trash` | Lists the articles in the trash a page at a time | `kb_articles:read` |
| `colliedesk_kb_article_undelete` | Takes an article out of the trash | `kb_articles:delete` |
| `colliedesk_kb_article_history` | Lists the versions of an article a page at a time (text cut after 2,000 characters; one version by number comes whole) | `kb_articles:read` |
| `colliedesk_kb_article_version_restore` | Makes the text of an earlier version current again, as a new version | `kb_articles:update` |
| `colliedesk_kb_article_ai_exclude` | Keeps an article away from reply drafts and triage suggestions, in every language; its text does not change | `kb_articles:update` |
| `colliedesk_kb_article_ai_include` | Lets AI read an excluded article again | `kb_articles:update` |
| `colliedesk_kb_image_upload` | Stores an image for articles (base64, at most 5 MiB) and gives the link to put in one | `kb_media:create` |
| `colliedesk_kb_image_get` | Shows a knowledge base image to the model | `kb_media:read` |
| `colliedesk_kb_media_list` | Lists the media library: what no article uses (with the space it takes) and images missing alt text | `kb_media:read` |
| `colliedesk_kb_media_get` | Shows one library item, its thumbnail and the articles that use it | `kb_media:read` |
| `colliedesk_kb_media_update` | Names an item, writes its alt text, or keeps it from the purge of unused images | `kb_media:update` |
| `colliedesk_kb_media_trash` | Moves library items to the trash by id, or all unused ones | `kb_media:delete` |
| `colliedesk_kb_media_trash_list` | Lists the library's trash, with when each item is deleted for good | `kb_media:read` |
| `colliedesk_kb_media_undelete` | Takes items out of the trash, by id or a whole "move to trash" batch | `kb_media:delete` |
| `colliedesk_kb_media_purge` | Deletes items in the trash for good, freeing their storage. Cannot be undone | `kb_media:purge` |
| `colliedesk_kb_media_replace` | Puts another image or video in place of one in every article that shows it | `kb_media:update` |
| `colliedesk_kb_media_replace_undo` | Undoes a replacement | `kb_media:update` |
| `colliedesk_kb_video_upload_start` | Starts uploading a video (MP4 or WebM, up to 200 MiB) in parts | `kb_media:create` |
| `colliedesk_kb_video_upload_part` | Sends the next part of an upload (up to 5 MiB, base64) | `kb_media:create` |
| `colliedesk_kb_video_upload_status` | Says where an upload stands, to go on after an interruption | `kb_media:read` |
| `colliedesk_kb_video_upload_list` | Lists the workspace's uploads in progress, who started each and when it expires | `kb_media:read` |
| `colliedesk_kb_video_upload_cancel` | Cancels an upload and throws its parts away (one you started; with kb_media:purge, any) | `kb_media:create` |
| `colliedesk_kb_video_upload_complete` | Checks and stores the uploaded video | `kb_media:create` |
| `colliedesk_kb_video_poster_set` | Sets a video's poster, the still picture shown before it plays | `kb_media:update` |
| `colliedesk_help_center_get` | Says whether the public help center is on, its title and language | `help_center:read` |
| `colliedesk_help_center_languages_get` | Lists the help center's languages, which are shown to customers, and how many articles are translated, outdated and missing in each | `help_center:read` |
| `colliedesk_help_center_languages_set` | Sets the help center's languages besides the primary one, and which of them customers see | `help_center:update` |
| `colliedesk_kb_translations_list` | Lists the articles missing, outdated or translated in one language | `kb_articles:read` |
| `colliedesk_kb_translations_trash` | Lists the translations in the trash | `kb_articles:read` |
| `colliedesk_kb_translation_create` | Adds an article's version in another language of the help center | `kb_articles:create` |
| `colliedesk_kb_translation_get` | Reads a translation with its text and the article it translates | `kb_articles:read` |
| `colliedesk_kb_translation_update` | Changes, publishes or unpublishes a translation, or marks it up to date or outdated | `kb_articles:update` |
| `colliedesk_kb_translation_delete` | Moves a translation to the trash for 30 days | `kb_articles:delete` |
| `colliedesk_kb_translation_undelete` | Takes a translation out of the trash | `kb_articles:delete` |
| `colliedesk_kb_translation_history` | Lists the versions of a translation | `kb_articles:read` |
| `colliedesk_kb_translation_version_restore` | Makes an earlier version of a translation current again | `kb_articles:update` |
| `colliedesk_kb_collection_translation_set` | Sets a collection's name and description in another language | `kb_articles:update` |
| `colliedesk_kb_collection_translation_delete` | Removes a collection's name in one language | `kb_articles:update` |
| `colliedesk_help_center_theme_get` | Reads the public help center's logo, colors, fonts, custom CSS and links | `help_center:read` |
| `colliedesk_help_center_theme_check` | Checks a candidate brand, custom CSS and color contrast without saving it; can render a page preview | `help_center:read` |
| `colliedesk_help_center_search_console_get` | Reads the Google and Bing Search Console verification tokens of the help center | `help_center:read` |
| `colliedesk_help_center_freshdesk_redirect_get` | Says how the old Freshdesk help center addresses move and, for a Freshdesk plan that allows it, returns the snippet that redirects them | `help_center:read` |
| `colliedesk_help_center_domain_get` | Reads the custom domain, its DNS records and the last check | `help_center:read` |
| `colliedesk_token_revoke` | Revokes the token the agent is using. Cannot be undone | any token |
| `colliedesk_docs_search` | Searches these user guides | any token |
| `colliedesk_reply_draft` | Leaves a reply draft, with files if needed (up to 10, 5 MiB a call), that a person edits, sends or discards; never emails by itself. A new draft goes on top and the earlier one stays | `ai_assist:create` |
| `colliedesk_reply_draft_restore` | Brings an earlier written draft (replaced, expired or discarded) back above the reply box, with its files | `ai_assist:create` |
| `colliedesk_workspace_export` | Gives a one-time download link of a workspace export | `workspace:export` |
| `colliedesk_billing_action` | Starts a plan checkout or opens the billing portal for the person to pay, or requests a Freshdesk data migration | `billing:manage` |
| `colliedesk_email_channel_disconnect` | Disconnects the default email channel. Cannot be undone | `channels:manage_credentials` |
| `colliedesk_email_secret_rotate` | Gives an email channel a new inbound secret, shown once. Cannot be undone | `channels:manage_credentials` |
| `colliedesk_gmail_connect` | Starts connecting Gmail for sending replies; the person behind the token agrees on Google's page | `channels:manage_credentials` |
| `colliedesk_gmail_disconnect` | Disconnects Gmail. Cannot be undone | `channels:manage_credentials` |
| `colliedesk_microsoft_connect` | Starts connecting Microsoft 365 for sending replies; the person behind the token signs in on Microsoft's page | `channels:manage_credentials` |
| `colliedesk_microsoft_disconnect` | Disconnects Microsoft 365. Cannot be undone | `channels:manage_credentials` |
| `colliedesk_workspace_slug_change` | Changes the workspace address | `workspace:update` |
| `colliedesk_number_series_list` | Lists the series conversations are numbered in, with their code format and next number | `conversations:read` |
| `colliedesk_number_series_set` | Adds a number series or changes its format, next number or start | `workspace:update` |
| `colliedesk_response_targets_get` | Reads the business hours and the response targets by priority | `conversations:read` |
| `colliedesk_response_targets_set` | Changes the business hours or the response targets | `workspace:update` |
| `colliedesk_my_sla_alerts_set` | Turns your own reminder and overdue emails on or off | `profile:update` |
| `colliedesk_problem_report` | Sends a problem report to the team that runs the server, with the request ids of failed calls | any token |
| `colliedesk_workspace_settings_get` | Shows the workspace name, address and timezone, and your own timezone | any token |
| `colliedesk_workspace_settings_update` | Sets the workspace timezone | `workspace:update` |
| `colliedesk_workspace_rename` | Renames the workspace (replies never carry this name; it is the help center title while the help center has none) | `workspace:update` |
| `colliedesk_my_timezone_set` | Sets or clears your own timezone | `profile:update` |
| `colliedesk_my_display_name_set` | Sets or clears your own display name | `profile:update` |
| `colliedesk_team_member_update` | Renames, deactivates or reactivates a team member | `team:manage_roles`, `team:read` |
| `colliedesk_invites_list` | Lists invites and their state, never their links | `team:read` |
| `colliedesk_invite_create` | Invites a person with roles by name and returns the one-time link | `team:invite` |
| `colliedesk_invite_revoke` | Revokes a pending invite. Cannot be undone | `team:revoke` |
| `colliedesk_sessions_list` | Lists sign-ins and agent tokens (the whole workspace with `admin`, otherwise your own) | any token |
| `colliedesk_session_revoke` | Revokes a token or signs out a session; another person's needs team:revoke. Cannot be undone | any token |
| `colliedesk_roles_list` | Lists the roles with their permissions, who holds them and the tokens that follow them | `team:read` |
| `colliedesk_role_create` | Makes a role from permissions you hold, or from another role | `team:manage_roles`, `team:read` |
| `colliedesk_role_update` | Renames a role or changes its permissions; everyone holding it and the tokens following it change at once | `team:manage_roles`, `team:read` |
| `colliedesk_role_delete` | Deletes a role nobody uses; it can be restored | `team:manage_roles`, `team:read` |
| `colliedesk_role_reset` | Puts the Agent role back to its first permissions | `team:manage_roles` |
| `colliedesk_role_restore` | Brings back a deleted role | `team:manage_roles` |
| `colliedesk_member_roles_set` | Sets which roles a team member holds, by name | `team:manage_roles`, `team:read` |
| `colliedesk_help_center_update` | Turns the public help center on or off and sets its title and language | `help_center:update` |
| `colliedesk_help_center_theme_update` | Replaces the public help center's brand after checking its color contrast and custom CSS | `help_center:update` |
| `colliedesk_help_center_search_console_update` | Sets or removes the Google and Bing verification tokens served as meta tags on the help center's pages | `help_center:update` |
| `colliedesk_help_center_domain_set` | Sets or replaces the custom domain, pending DNS ownership proof | `help_center:update` |
| `colliedesk_help_center_domain_check` | Checks DNS ownership and activates the proven custom domain | `help_center:update` |
| `colliedesk_help_center_domain_remove` | Removes the custom domain and restores the slug address | `help_center:update` |
| `colliedesk_web_form_get` | Shows the web form settings | `channels:read` |
| `colliedesk_web_form_update` | Turns the web form on or off and sets the allowed origins | `channels:update` |
| `colliedesk_email_channel_get` | Shows the email channel, never the inbound secret | `channels:read` |
| `colliedesk_email_channel_update` | Connects the email channel or changes the support address or the sender name | `channels:update` |
| `colliedesk_email_domain_verify` | Checks the DNS record that verifies the sending domain | `channels:update` |
| `colliedesk_email_test_message_send` | Sends your own address a test email that opens a conversation you can answer | `conversations:update` |
| `colliedesk_email_forwarding_confirmation_get` | Reads the code and link Gmail sent to confirm forwarding to an email channel | `channels:read` |
| `colliedesk_billing_get` | Shows the plan, trial and seats on the hosted cloud | `billing:read` |
| `colliedesk_imports_list` | Lists imports | `imports:read` |
| `colliedesk_import_get` | Shows one import with its progress and why it failed or waits | `imports:read` |
| `colliedesk_import_preview` | Reads a Freshdesk account before importing it, without writing anything. Takes a Freshdesk admin's API key, which passes through the conversation | `imports:manage_credentials` |
| `colliedesk_import_start` | Starts a previewed import: recent tickets first, then the whole account | `imports:run` |
| `colliedesk_import_cancel` | Stops an import or discards a preview; what was imported stays | `imports:run` |
| `colliedesk_import_key_set` | Gives an import waiting for its Freshdesk key the key again | `imports:manage_credentials` |
| `colliedesk_import_report` | Reads the reconcile report of an import (JSON or Markdown) | `imports:read` |
| `colliedesk_import_undo` | Removes what an import brought in. Cannot be undone | `imports:undo` |
| `colliedesk_import_people_list` | Lists the agents of an import's Freshdesk account and who each is linked to | `imports:read` |
| `colliedesk_import_person_link` | Links a Freshdesk agent to a Colliedesk agent; their imported replies and tickets follow | `imports:run` |
| `colliedesk_import_person_unlink` | Takes back the link between a Freshdesk agent and a Colliedesk agent | `imports:run` |
| `colliedesk_ai_settings_get` | Shows the [AI settings](ai.md): providers (never a key), each feature's provider and model, and the change history | `ai_settings:read` |
| `colliedesk_ai_models_list` | Lists the models a key may use, before adding the provider; saves and bills nothing | `ai_settings:manage_credentials` |
| `colliedesk_ai_provider_add` | Adds an AI provider after a billed check of the first model. The key passes through the conversation | `ai_settings:manage_credentials` |
| `colliedesk_ai_provider_update` | Renames an AI provider, changes its models, or an OpenRouter provider's routing | `ai_settings:update` |
| `colliedesk_ai_provider_key_set` | Replaces an AI provider's key after a billed check of the first model. The key passes through the conversation | `ai_settings:manage_credentials` |
| `colliedesk_ai_provider_check` | Runs a check of an AI provider | `ai_settings:update` |
| `colliedesk_ai_provider_remove` | Deletes an AI provider and erases its key; its features turn off. Cannot be undone | `ai_settings:update` |
| `colliedesk_ai_slot_set` | Sets an AI feature: its provider and model, on or off, and what it does by itself by default | `ai_settings:update` |
| `colliedesk_ai_usage_get` | Shows a month of AI usage per feature and provider, and what is left of the included AI | `billing:read` |
| `colliedesk_ai_automatic_limits_set` | Changes how many automatic AI runs a workspace allows a day and a minute | `ai_settings:update` |
| `colliedesk_ai_chat_usage_get` | Shows a month of the help center's AI chat against its limits | `billing:read` |
| `colliedesk_ai_chat_sessions_list` | Lists end users' chats with the help center's AI | `messages:read` |
| `colliedesk_ai_chat_session_get` | Reads one AI chat's transcript with the articles each answer cited | `messages:read` |
| `colliedesk_ai_chat_session_delete` | Deletes an AI chat's transcript. Cannot be undone | `ai_settings:update` |
| `colliedesk_ai_chat_preview` | Asks the help center's AI chat a question as a customer would | `ai_assist:create` |
| `colliedesk_channel_ai_get` | Shows what each AI feature does by itself on a channel and its scopes | `channels:read` |
| `colliedesk_channel_ai_set` | Sets what an AI feature does by itself on a channel or a scope | `ai_settings:update` |
| `colliedesk_ai_source_consent_set` | Lets AI intake send images, a contact's profile or an integration's data | `ai_settings:update` |
| `colliedesk_ai_source_consent_remove` | Stops AI intake sending a kind of extra data | `ai_settings:update` |
| `colliedesk_channel_ai_intake_readiness_get` | Shows, per action, whether intake's AI may act on its own on a chat scope, and what keeps it a suggestion | `channels:read` |
| `colliedesk_channel_ai_intake_evaluations_list` | Lists the offline evaluations recorded for a chat channel's intake | `channels:read` |
| `colliedesk_channel_ai_intake_evaluation_record` | Records an offline evaluation of a chat scope's intake, which can open a gate | `ai_settings:update` |
| `colliedesk_conversation_chat_get` | Reads what was said in the chat places a conversation came from, and which messages are its own | `messages:read` |
| `colliedesk_channel_threads_list` | Lists a chat channel's one-to-one chats and groups | `messages:read` |
| `colliedesk_channel_thread_items` | Reads everything said in one chat place | `messages:read` |
| `colliedesk_channel_senders` | Lists everyone who wrote in a chat channel's places, each once, most recent first | `messages:read` |
| `colliedesk_channel_unresolved_items` | Lists chat messages nobody concluded on yet | `messages:read` |
| `colliedesk_intake_list` | Lists the cards of the intake queue of chat channels: waiting, being decided, set aside or decided | `messages:read` |
| `colliedesk_intake_get` | Reads an intake card with what its customer said and every decision made on it | `messages:read` |
| `colliedesk_intake_decide` | Opens a conversation from an intake card, adds it to one, or sets it aside | `conversations:update` |
| `colliedesk_intake_requeue` | Puts an intake card that was set aside back in the queue | `conversations:update` |
| `colliedesk_intake_summary` | Says how many intake cards wait for a person and for AI, and the chat channels they come from | `conversations:read` |
| `colliedesk_intake_stats` | Counts a period of intake: by state, by who decided, and AI's decisions a person changed | `conversations:read` |
| `colliedesk_conversation_intake_get` | Reads why each message from a channel is in a conversation | `messages:read` |
| `colliedesk_messages_move` | Moves customer messages to another conversation, a new one, or back to a chat's intake queue | `conversations:update` |
| `colliedesk_message_moves_list` | Lists the moves of messages out of or into a conversation | `conversations:read` |
| `colliedesk_message_move_undo` | Undoes a move of customer messages | `conversations:update` |
| `colliedesk_ai_draft_create` | Asks for an [AI reply draft](ai.md#reply-drafts) of a conversation, with an optional prompt; it is written in the background and never sent | `ai_assist:create` |
| `colliedesk_ai_draft_get` | Shows an AI draft, waiting up to 20 seconds while it is written | `ai_assist:read` |
| `colliedesk_ai_drafts_list` | Lists a conversation's drafts in pages (`before`, `limit`), the AI's and the written ones, with their files; earlier written ones can be restored | `ai_assist:read` |
| `colliedesk_ai_draft_discard` | Stops an AI draft being written or discards a ready one | `ai_assist:create` |
| `colliedesk_ai_triage_get` | Shows a conversation's newest [AI triage suggestion](ai.md#triage-suggestions): tags, priority, assignee and a reason | `ai_assist:read` |
| `colliedesk_ai_triage_create` | Asks for a triage suggestion again | `ai_assist:create` |
| `colliedesk_ai_triage_accept` | Applies a suggestion (all of it, or the parts named) to its conversation | `ai_assist:create` |
| `colliedesk_ai_triage_dismiss` | Dismisses a suggestion | `ai_assist:create` |
| `colliedesk_ai_triage_undo` | Takes back what AI applied of a suggestion by itself (all of it, or the parts named); keeps what a person changed since | `ai_assist:create` |
| `colliedesk_results_get` | Shows the [results page](results.md): your numbers for the last 30 and 7 days next to the history you imported | `results:read` |
| `colliedesk_results_share_link_get` | Shows whether the results page has an active share link, who made it and when it was last opened; never the link itself | `results:share` |
| `colliedesk_results_share_link_create` | Creates a share link of the results page; the old link stops working | `results:share` |
| `colliedesk_results_share_link_revoke` | Turns off the results page's share link | `results:share` |
| `colliedesk_results_email_set` | Turns your weekly results email on or off (admins only) | `profile:update` |
| `colliedesk_setup_progress_get` | Shows the four getting-started steps (email, import, invite, first reply) and where each stands | `conversations:read` |
| `colliedesk_setup_guide_set` | Hides the [setup guide](setup-guide.md) from your inbox, or shows it again | `profile:update` |
| `colliedesk_channels_list` | Lists the channels with their state and the channel types the server runs | `channels:read` |
| `colliedesk_channels_get` | Shows one channel and its configuration | `channels:read` |
| `colliedesk_channels_events` | Shows the history of the channels | `channels:read` |
| `colliedesk_channels_create` | Adds a channel | `channels:update` |
| `colliedesk_channels_update` | Renames a channel, turns it on or off, or changes its configuration or rules | `channels:update` |
| `colliedesk_channels_check` | Reads a channel's state again | `channels:update` |
| `colliedesk_channels_held_replay` | Opens the messages a channel held while it was off | `channels:update` |
| `colliedesk_channels_credentials_set` | Stores a channel's secret | `channels:manage_credentials` |
| `colliedesk_channels_delete` | Removes a channel; its conversations stay. Cannot be undone | `channels:manage_credentials` |
| `colliedesk_channels_held_discard` | Discards the messages a channel held while it was off. Cannot be undone | `channels:update` |
| `colliedesk_autofix_status` | Shows whether Autofix is set up and how many fixes wait for a person or are running | `autofix:read` |
| `colliedesk_fixes_list` | Lists the fixes sent to coding agents, by group (needs a person, running, done) | `autofix:read` |
| `colliedesk_fix_get` | Shows one fix: where it is, its issue and pull request, what was sent and its timeline | `autofix:read` |
| `colliedesk_conversation_fixes_list` | Lists a conversation's fixes, the latest attempt first | `autofix:read` |
| `colliedesk_autofix_settings_get` | Shows Autofix's GitHub installations, connected repositories and coding agents (never a key) | `autofix_settings:read` |
| `colliedesk_github_repositories_list` | Lists the repositories a GitHub installation reaches, read from GitHub now, and which are connected | `autofix_settings:read` |
| `colliedesk_github_connect` | Starts installing the Colliedesk GitHub App; the person behind the token installs it on GitHub | `autofix_settings:update` |
| `colliedesk_github_disconnect` | Uninstalls the GitHub App from an account; its repositories stay, paused. Cannot be undone | `autofix_settings:update` |
| `colliedesk_autofix_repo_connect` | Connects a private repository for fixes | `autofix_settings:update` |
| `colliedesk_autofix_repo_update` | Sets the coding agent a repository suggests | `autofix_settings:update` |
| `colliedesk_autofix_repo_disconnect` | Disconnects a repository; its fixes in progress are cancelled. Cannot be undone | `autofix_settings:update` |
| `colliedesk_autofix_repo_notes_include_set` | Lets fixes on a repository carry internal notes | `autofix_settings:update` |
| `colliedesk_autofix_repo_notes_exclude` | Stops internal notes going with fixes on a repository | `autofix_settings:update` |
| `colliedesk_fix_preview` | Builds what sending a conversation to a coding agent would send, masked, without sending | `autofix:read` |
| `colliedesk_fix_send` | Sends a conversation to a coding agent; for an issue the person behind the token confirms on GitHub | `autofix:send` |
| `colliedesk_fix_retry` | Sends a failed or cancelled fix again | `autofix:send` |
| `colliedesk_fix_cancel` | Cancels a fix in progress and closes its issue | `autofix:update` |
| `colliedesk_fix_mark_deployed` | Marks a merged fix released | `autofix:update` |
| `colliedesk_fix_unmark_deployed` | Takes back a fix's release | `autofix:update` |
| `colliedesk_fix_pr_link` | Links a pull request Colliedesk did not find to a fix | `autofix:update` |
| `colliedesk_fix_pr_unlink` | Unlinks a pull request a person linked | `autofix:update` |
| `colliedesk_coding_agent_enable` | Turns on a coding agent for Autofix, with consent to the data it receives | `autofix_settings:update` |
| `colliedesk_coding_agent_disable` | Turns off a coding agent for Autofix; fixes not sent yet are cancelled, fixes already sent go on | `autofix_settings:update` |
| `colliedesk_coding_agent_check` | Checks a coding agent: the GitHub App and private repositories, or Cursor's key and its repositories | `autofix_settings:read` |
| `colliedesk_cursor_key_set` | Sets the Cursor key Autofix sends fixes with, after Cursor accepts it. The key passes through the conversation | `autofix_settings:manage_credentials` |
| `colliedesk_cursor_key_remove` | Erases the Cursor key; Cursor takes no new fix until one is set again | `autofix_settings:manage_credentials` |
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
| Tool | What it does | Needs |
|---|---|---|
| `colliedesk_operator_whoami` | Shows the operator the token acts as, what it may ask for and where the console is | operator |
| `colliedesk_operator_sign_out` | Ends this operator token. Cannot be undone | operator |
| `colliedesk_operator_token_create` | Asks an operator for another operator token | operator |
| `colliedesk_operator_status` | Shows the server's release, the configuration each process runs and what needs attention | operator |
| `colliedesk_operator_config_get` | Reads every setting, without secrets | operator |
| `colliedesk_operator_config_check` | Checks a settings change without saving it and says what saving it would ask | operator |
| `colliedesk_operator_config_change` | Asks an operator to change settings | operator |
| `colliedesk_operator_config_history` | Lists the configuration versions and what each changed | operator |
| `colliedesk_operator_config_restore` | Asks an operator to restore an older configuration version | operator |
| `colliedesk_operator_trial_confirm` | Asks an operator to keep the trial change that runs | operator |
| `colliedesk_operator_trial_revert` | Asks an operator to end the trial change that runs now | operator |
| `colliedesk_operator_products` | Lists the Polar products the server sells or once sold | operator |
| `colliedesk_operator_product_add` | Asks an operator to add a Polar product | operator |
| `colliedesk_operator_product_on_sale` | Asks an operator to put a Polar product on or off sale | operator |
| `colliedesk_operator_audit` | Reads the operator audit log | operator |
| `colliedesk_operator_requests` | Lists the requests this token made | operator |
| `colliedesk_operator_request_get` | Shows one request this token made | operator |
| `colliedesk_operator_request_cancel` | Withdraws a request this token made | operator |
| `colliedesk_operator_team` | Lists operators, sessions and tokens | operator |
| `colliedesk_operator_invite` | Asks an operator to invite another operator | operator |
| `colliedesk_operator_revoke` | Asks an operator to revoke an operator | operator |
| `colliedesk_operator_session_revoke` | Asks an operator to end a session or token | operator |
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
  steers the agent; it does not remove it. Give agents only the permissions the job needs.
- **Only native team messages count as team text.** A reply or note written in Colliedesk by a
  team member shows its author. A message imported from another helpdesk is marked like customer
  text unless it was a reply or a private note of a team member: a Freshdesk public note or a
  forward can have been written by the customer. A message with no team member as author is marked
  too, author name included.
- **Attachments.** `colliedesk_attachment_get` returns the text of a plain UTF-8 text file, cut
  after 20,000 characters, and a PNG, JPEG, GIF or WebP image of at most 2 MiB as an image the
  model can look at (`image: false` leaves it out). A larger image comes back as a preview of at
  most 320 pixels, the thumbnail the web app shows, and the hint says so. The type is judged from the
  bytes, never from the file name; an SVG is text, never an image. Any other file (a PDF, an archive)
  comes back as its name, size, SHA-256 and detected type only: open it in the web app. The file
  name and text are customer text. **An image cannot be marked as customer text:** whatever it
  shows (a screenshot of instructions, text in a photo) reaches the model as it is. The tool
  description tells the model it is data a customer sent, never instructions; give agents only the
  permissions the job needs.
- **Saved replies are team text.** Their title and text appear under `team_text`: the team wrote
  them for customers. The tool descriptions tell the model to treat them as text to send or improve,
  never as instructions. Writes carry the `updated_at` the model read, so it cannot overwrite or
  delete a version it never saw.
- **Files an agent attaches.** `colliedesk_note_add`, `colliedesk_reply_send` and
  `colliedesk_conversation_start` take `files`: up to 10 files, 5 MiB in total per call, each with
  a `filename` and its bytes as base64. The REST API takes 10 MiB a message; attach larger files in
  the web app. A call with files the token may not make is refused
  and nothing is stored. `colliedesk_reply_draft` carries no files. A reply written from
  an [AI draft](ai.md#reply-drafts) passes its `ai_draft_id`, so the draft records how close the
  reply stayed to it; this needs `messages:send`, and a draft that is not the conversation's
  ready one (another conversation's, another workspace's, already sent) is refused and nothing is
  sent.
- **Who wrote it.** When an agent token wrote a note or a reply, the message also shows
  `written_by_agent_token` with the label of the token and the team member it acts as (`acts_as`). The server tells the model to read
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
- `colliedesk_conversations_spam` with `spam: true` files junk in [Spam](inbox.md#spam) and marks its
  senders; `spam: false` (Not spam) puts each conversation back in the status it had. A conversation in
  Spam keeps its status: `colliedesk_conversation_update` and `colliedesk_conversations_bulk_update`
  refuse to change it and say to take it out first. `colliedesk_inbox_search` with `status: "spam"` lists
  them.
- A status set with `colliedesk_conversation_update` or `colliedesk_conversations_bulk_update` on a
  conversation whose chat channel has words for it [tells the customer](inbox.md#telling-the-customer); that
  change needs `messages:send`, and without it the call is refused and names the channel.
  `notify: false` sets the status quietly with `conversations:update` alone.
