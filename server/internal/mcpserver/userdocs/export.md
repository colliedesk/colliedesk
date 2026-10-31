# Export all your data

Download everything in your workspace as one ZIP file. Use it as a backup, to move to another
helpdesk, or to keep a copy when a cloud plan ends. The format is open and documented here.

## Who can export

Admins only, because the file holds every contact. Agents get "403 Forbidden".

The export also works when the workspace is locked (a cloud trial or subscription ended) and on
self-hosted installs.

## How to export

In the app, open **Settings → Export all data** and select **Export all data**. On a locked cloud
workspace the same button is on the locked page. Your browser then downloads the file itself, with its
own progress bar, straight to disk: a large workspace is never held in memory. Keep the page open until
the download finishes. If the server is busy with other exports you are told how long to wait. If
another export of your workspace is still running you are told to wait for it. A file that stops early
has no `manifest.json`; download it again.

Without the app, send an authenticated `GET /api/v1/export` (the app uses `POST /api/v1/export/link`, which returns a one-time URL valid for 60 seconds that streams the same file without a token). With an API token:

```sh
curl -fSL -H "Authorization: Bearer $COLLIEDESK_TOKEN" \
  -o colliedesk-export.zip https://desk.example.com/api/v1/export
```

The server builds the ZIP while it sends it, so the download starts at once and large workspaces
do not need extra memory. The file is named `colliedesk-<workspace slug>-<UTC time>.zip`.

Only one export of a workspace runs at a time. A second request gets "409 Conflict" until the
first one has sent its last byte. A server also runs at most two exports at once; another one
gets "429 Too Many Requests" with a `Retry-After` header. Wait and try again.

Keep reading while the file downloads. If your client stops reading for more than 60 seconds, the
server cancels the export and frees its resources; start it again. One export may run for at most
2 hours in total, however fast the client reads.

## Check that the file is complete

A complete export contains `manifest.json`. If the server cannot read a stored file, or a file does
not match its recorded checksum, it aborts the download. You then get a broken ZIP without
`manifest.json`. Run the export again; if it fails again, report the attachment id in the server log.

## Archive layout (format version 1)

| Path | Content |
|---|---|
| `manifest.json` | Format, version, export time (UTC), workspace, record counts, and every attachment and knowledge base image file with its size and SHA-256 |
| `agents.ndjson` | One agent per line: `id`, `email`, `display_name`, `role`, `timezone_override`, `created_at`, `deactivated_at` (`null` for an active agent) |
| `contacts.ndjson` | One contact per line: `id`, `name`, `email` (`null` for a customer known only on a channel), `organization`, `merged_into_id` (the contact it was [merged](contacts.md#merge-two-contacts-of-the-same-person) into, or null), `created_at`, `updated_at`, `identities` (who the customer is on a channel: `channel_id`, `kind`, `value`, `display_name`) |
| `contact-merges.ndjson` | Every merge of two contacts, undone or not, oldest first: `id`, `contact_id` (kept), `merged_contact_id`, `conversation_ids` (the conversations it moved), `merged_by_agent_id`, `token_id`, `created_at`, `undone_at`, `undone_by_agent_id` |
| `tags.ndjson` | One tag per line: `id`, `name`, `color`, `created_at` |
| `saved-replies.ndjson` | One saved reply per line, including the ones in the trash: `id`, `title`, `body`, `created_by_agent_id` (an agent id or `null`), `created_at`, `updated_at`, `deleted_at` (when it went to the trash, or `null`), `deleted_by_agent_id`. The body keeps the variables `{{customer.name}}` and `{{agent.name}}` unfilled |
| `saved-reply-versions.ndjson` | Every [version](saved-replies.md#history-and-restore) of every saved reply, one per line, ordered by reply and version: `saved_reply_id`, `version`, `kind` (`baseline`, `create`, `edit`, `delete`, `restore` or `undelete`; `baseline` is the text a reply had when history started), `restored_from_version`, `title`, `body` (the reply after the change), `changed_by_agent_id`, `token_id`, `token_label` (the agent token that made or asked for it), `agent_request_id` (the approved agent request that made it), `created_at` |
| `saved-views.ndjson` | One [saved inbox view](inbox.md#saved-views) per line, every person's and the ones in the trash: `id`, `name`, `scope` (`personal` or `shared`), `owner_agent_id` (the person a personal view belongs to, `null` when shared), `filters` (the filters of `GET /api/v1/conversations` as an object), `created_by_agent_id`, `created_at`, `updated_at`, `deleted_at` |
| `kb-collections.ndjson` | One knowledge base collection per line, including the ones in the trash: `id`, `parent_id` (the collection it sits in, `null` at the top), `name`, `slug`, `description`, `position`, `source_meta` (what an import read that has no field of its own, or `null`), `created_at`, `updated_at`, `deleted_at`, `deleted_by_agent_id` |
| `kb-articles.ndjson` | One knowledge base article per line, ordered by number, including the ones in the trash: `id`, `collection_id`, `position` (its place in its collection, set by hand; `0` until ordered, then ties go by title), `number` (never reused; the help center address is `/a/<number>-<slug>`), `title`, `slug`, `body_md` (the text in Markdown), `seo_title` and `seo_description` (what search engines show; empty uses the title and the start of the text), `status` (`draft` or `published`), `published_at`, `author_agent_id`, `source_meta`, `source_html` (the original HTML an import read, or `null`), `image_ids` (the images its versions link), `created_at`, `updated_at`, `deleted_at`, `deleted_by_agent_id`, `ai_excluded_at` and `ai_excluded_by_agent_id` (set while AI may not read the article, [AI and the knowledge base](knowledge-base.md#keep-an-article-away-from-ai)) |
| `kb-article-versions.ndjson` | Every version of every article, ordered by article and version: `article_id`, `version`, `kind` (`create`, `edit`, `delete`, `restore`, `undelete`, `import`, `media_replace`, `ai_exclude` or `ai_include`), `restored_from_version`, `title`, `slug`, `body_md`, `seo_title`, `seo_description`, `status`, `collection_id` (as they were after the change), `changed_by_agent_id`, `token_id`, `token_label`, `agent_request_id`, `created_at` |
| `kb-article-translations.ndjson` | One translation of an article per line (an import brings them in): `article_id`, `locale`, `title`, `body_md`, `source_meta`, `source_html`, `created_at`, `updated_at` |
| `kb-images.ndjson` | One image of the knowledge base media library per line: `id`, `content_type`, `size`, `sha256`, `path` (the file in the ZIP), `name`, `alt`, `width` and `height` (as shown, `null` until the server read them), `kept` (never deleted as unused), `created_by_agent_id`, `created_at`, `updated_at`, `updated_by_agent_id`, `deleted_at` and `deleted_by_agent_id` (set while it is in the library's trash). Thumbnails are not exported; the server makes them again |
| `help-center.ndjson` | The help center settings, one line once the knowledge base or the help center was first written: `enabled`, `title`, `locale`, `content_version`, `last_article_number`, `updated_at`, and its [brand](knowledge-base.md#brand): `logo_media_id` and `favicon_media_id` (ids in `kb-images.ndjson`), `logo_alt`, `linkback_url`, `contact_url`, `contact_phone`, `color_primary`, `color_header`, `color_header_text`, `color_background`, `color_text`, `color_link`, `color_footer`, `body_font`, `heading_font` (`null` is the default look), `custom_css` (exactly as saved; `""` is none), its [Search Console](knowledge-base.md#search-console-and-bing-webmaster-tools) tokens `google_site_verification` and `bing_site_verification` (`null` when none), and its [custom domain](knowledge-base.md#custom-domain): `custom_domain` (the host name, `null` when none), `custom_domain_status` (`pending` or `active`) and `custom_domain_verified_at`. The domain's proof token is not exported; after a restore, set the domain again and check it |
| `agent-requests.ndjson` | One [agent request](agent-requests.md) per line: `id`, `kind`, `status`, `method`, `route`, `path`, `body` (the call as the token sent it), `edited_body` (a reply draft as a person edited it, or `null`), `summary`, `conversation_id`, `token_id`, `token_label`, `token_scope` (`custom` for a token with its own permissions), `token_permissions` (what the token held when it asked, `resource:action`), `requested_by_agent_id`, `decided_by_agent_id`, `decided_at`, `reason`, `result_status`, `result_note`, `created_at`, `expires_at`, `saved_reply_id` and `saved_reply_before` (for a request on a saved reply: the reply as the token saw it, and the version a restore applies). The answer of an approved call is not exported. `body`, `edited_body` and `saved_reply_before` are `null` 90 days after the decision |
| `import-jobs.ndjson` | One [import from Freshdesk](import-freshdesk.md) run in the app, or its undo, per line: `id`, `kind` (`import` or `undo`), `source`, `domain`, `account_source`, `undo_of`, `state`, `phase`, `options`, `preview`, `interim_report`, `report` (the reconcile report), `status`, `error_code`, `error_message`, `created_by`, `created_at`, `started_at`, `finished_at`. The Freshdesk API key is never exported |
| `import-people.ndjson` | One agent of an imported Freshdesk account per line: `source` (the account, `freshdesk:account:<id>`), `external_id` (the Freshdesk agent id, which imported messages carry as `origin.author_external_id`), `name`, `email`, `active`, `linked_agent_id` (the agent who takes over their imported replies), `linked_by_email`, `chosen_by_person`, `created_at`, `updated_at` |
| `ai-providers.ndjson` | One [AI provider](ai.md) the workspace added per line: `id`, `kind` (`anthropic` or `openai_compatible`), `label`, `base_url`, `auth_kind` (`api_key` or `none`), `key_hint` (the key's last 4 characters), `models`, `check_status`, `check_error_code`, `checked_at`, `created_by`, `created_at`, `updated_at`. The key is never exported: after a restore, enter it again |
| `ai-slots.ndjson` | One AI feature setting per line: `slot` (`triage`, `draft`, `chat` or `intake`), `uses_server`, `provider_id`, `model`, `effort`, `enabled`, `default_level` (`off`, `suggest` or `auto`: what it does by itself where no channel sets a level), `automatic` (the older switch, read from `default_level`) and `auto_parts` (triage applies those parts by itself), `consented_by`, `consented_at` (who agreed that its data goes to that provider), `updated_by`, `updated_at`, `monthly_cap_micro_usd` (AI chat's own monthly limit) |
| `ai-settings-events.ndjson` | One change to the AI settings per line, oldest first: `id`, `kind`, `actor_agent_id`, `actor_token_id`, `provider_id`, `provider_label`, `slot`, `detail` (what changed; keys only by their last 4 characters), `created_at` |
| `ai-source-consents.ndjson` | One kind of extra context AI intake may send per line: `source_class` (`images`, `contact_profile`, `external:<name>`), `consented_by`, `consented_at` |
| `ai-usage.ndjson` | One call to an AI provider per line: `id`, `feature`, `billed_to` (`server` or `workspace_key`), `provider_id`, `provider_kind`, `provider_label`, `model`, `status` (`ok`, `error`, or `started` for a call that never finished), `error_code`, `stop_reason`, `served_model`, `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens` (`null` when the provider reported none), `hops` (per model, when a refused request was answered by another model), `provider_request_id`, `actor_agent_id`, `actor_token_id`, `started_at`, `finished_at`, `latency_ms`, `task_id` (the background task it worked for), `automatic` (no person asked for it), `cost_micro_usd` (what the server's provider spent, in millionths of a dollar, at that day's prices; `null` for your own key), `reserved_micro_usd` (the most the call could cost, set aside against the allowance while it ran; `null` for your own key). No prompt or answer is stored. Background tasks themselves are not exported |
| `ai-triage.ndjson` | One [AI triage suggestion](ai.md#triage-suggestions) per line: `id`, `conversation_id`, `task_id`, `automatic`, `requested_by_agent_id`, `requested_by_token_id`, `status` (`pending`, `ready` or `cancelled`), `tag_ids`, `priority`, `assignee_agent_id`, `reason` (erased 90 days after it was decided), `sources` (the articles and solved conversations it read: `kind`, `id`, `title`), `base` (the conversation's `priority` and `assignee_agent_id` when it was asked for), `stale` (what a person set before it came, so not suggested), `model`, `outcome` (`accepted`, `partly_accepted`, `dismissed`, `superseded`, `expired`; empty while open), `applied` (`tag_ids`, `priority`, `assignee_agent_id` applied so far), `auto_applied_at` and `auto_applied` (what AI applied by itself), `skipped` (what it left, and why), `undone_parts`, `kept`, `undone_at`, `undone_by_agent_id` (an undo of what AI applied), `created_at`, `ready_at`, `decided_at`, `decided_by_agent_id` |
| `ai-drafts.ndjson` | One [AI reply draft](ai.md#reply-drafts) per line: `id`, `conversation_id`, `task_id`, `requested_by_agent_id`, `requested_by_token_id`, `prompt` (what the person asked for), `status` (`pending`, `ready` or `cancelled`), `text`, `missing_info`, `sources` (`kind`, `id`, `title` of each article or solved conversation it used), `warnings` (an address, a number or a name the conversation did not hold), `model`, `outcome` (`sent_unchanged`, `sent_edited`, `discarded`, `superseded`, `expired`), `similarity` (0 to 1, how close the reply sent stayed to it), `sent_message_id`, `automatic` (written by itself for a customer message), `answers_message_id`, `reply_needed`, `created_at`, `ready_at`, `decided_at`, `decided_by_agent_id`, `text_cleared_at` (when the text and prompt were erased, 90 days after the draft ended) |
| `ai-chat-sessions.ndjson` | One end user's chat with the [help center's AI](ai-chat.md) per line, never its cookie: `id`, `channel_id`, `surface` (`help_center`, `article`, `widget`), `status` (`open`, `ended`, `handed_off`), `conversation_id` (opened by its hand-off), `history_cut_at`, `created_at`, `last_turn_at`, `ended_at`, and `turns`: `id`, `seq`, `question`, `answer` (both erased 90 days after the chat's last turn), `viewing_kb_article_id`, `status` (`streaming`, `answered`, `no_source`, `over_limit`, `failed`), `error_code`, `model`, `ttft_ms`, the four token counts, `created_at`, `finished_at`, `sources` (`ordinal`, `kb_article_id`, `article_number`, `article_version`, `title`, `folder`, as they were) and `feedback` (`rating`, `created_at`, `updated_at`) |
| `results-baselines.ndjson` | The [results](results.md) baseline, when there is one: `source` (the import it comes from), `anchor_at` (when that import's first full run finished), `import_status` (`OK`, `FAILED`, `UNVERIFIED`, or `null` for an import made before results existed), `computed_at` and `numbers` (the baseline's windows, as `GET /api/v1/results` returns them) |
| `autofix-installations.ndjson` | One GitHub App installation connected for Autofix per line: `id`, `installation_id` (GitHub's), `account_login`, `account_type`, `repository_selection` (`selected` or `all`), `status` (`active`, `blocked_all_repositories`, `suspended`), `connected_by_agent_id`, `connected_by_name`, `connected_at` |
| `autofix-repos.ndjson` | One repository connected for fixes per line: `id`, `installation_id`, `github_repo_id`, `full_name`, `visibility`, `visibility_checked_at`, `default_branch`, `include_notes`, `default_connector`, `paused_reason` (`became_public`, `removed_from_app`, `installation_suspended`), `paused_at`, `connected_by_agent_id`, `connected_by_name`, `connected_at` |
| `autofix-connectors.ndjson` | One coding agent setting per line: `kind` (`claude_issue`, `copilot_issue`, `cursor_api`), `enabled`, `mode` (Copilot: `assign_api` or `assign_on_github`), the data consent (`consent_agent_id`, `consent_name`, `consent_at`, `consent_version`), `credential_set` (whether a Cursor key is set; never the key), `credential_set_by_agent_id`, `credential_set_by_name`, `credential_set_at`, `last_check_at`, `last_check_result`, `last_check_message` |
| `fixes.ndjson` | One fix (a conversation sent to a coding agent) per line: `id`, `conversation_id`, `attempt_no`, `previous_attempt_id`, `repo_id`, `repo_full_name`, `connector`, `state` (`sending`, `sent`, `working`, `pr_open`, `merged`, `deployed`, `failed`, `cancelled`), `outcome_reason`, `state_changed_at`, `sent_title` and `sent_body` (exactly what left Colliedesk), `sent_message_ids`, `sent_notes`, `created_by_kind`, `created_by_agent_id`, `created_by_name`, `created_by_token_id`, `approved_request_id`, `github_login`, `copilot_assignment` (Copilot: `assigned` or `on_github`), `issue_number`, `vendor_ref`, `vendor_url`, `branch`, `pr_number`, `pr_url`, `pr_linked_by`, `merge_sha`, `merged_at`, `merged_by_login`, `deployed_at`, `deployed_by_agent_id`, `deployed_by_name`, `created_at` |
| `fix-events.ndjson` | Every event of every fix's timeline, oldest first: `id`, `fix_id`, `kind`, `from_state` and `to_state` (both set when the state moved), `actor_kind` (`person`, `token`, `github`, `cursor`, `system`), `actor_id`, `actor_name`, `data`, `created_at` |
| `channels.ndjson` | One [channel](channels.md) per line, removed ones included: `id`, `type` (`email`, `web_form`), `name`, `enabled`, `is_default` (the default channel of its type), `settings` (its rules over its type's defaults), `config` (its configuration as Settings shows it; `null` once removed), `external_account`, `created_at`, `updated_at`, `disabled_at`, `deleted_at`. A channel's secrets are never exported |
| `channel-events.ndjson` | One change to a channel per line, oldest first: `id`, `channel_id`, `channel_type`, `channel_name`, `kind` (`added`, `renamed`, `enabled`, `disabled`, `default_changed`, `config_changed`, `credential_changed`, `removed`, `held_replayed`, `held_discarded`, `ai_changed`), `actor_agent_id`, `actor_token_id`, `detail` (what changed; a secret only by its last 4 characters), `created_at` |
| `channel-ai-levels.ndjson` | One [AI level](ai.md#what-ai-does-by-itself-per-channel) a channel sets per line: `channel_id`, `scope` (empty for the whole channel), `slot`, `level`, `options` (intake's settings), `updated_by`, `updated_at` |
| `channel-inbound-events.ndjson` | What a channel received that is not a message, one per line: messages it holds because it was off, ones being collected or that failed to be processed, ones it dropped (and why) and chatter it kept without opening a conversation. `id`, `channel_id`, `external_event_id`, `state` (`received`, `pending`, `held`, `failed`, `dropped`, `processed`), `raw` (the payload as the channel sent it, or a summary for mail), `raw_path` (the raw message in the ZIP, for held mail), `hold_reason`, `drop_reason`, `error`, `decision_basis` and `decision_rule` (what the channel's rules decided: `match`, `default` or `person`, and the rule, such as `scope_archive` or `review_hold`; empty while it is collected or when the channel was off), `received_at`, `processed_at`, `files` (`name`, `content_type`, `status`, `size`, `sha256`, `error`, `path`) |
| `channel-events/<event id>/...` | The raw message and the files of each event in `channel-inbound-events.ndjson` |
| `conversations.ndjson` | One conversation per line, with its messages, time entries and status changes |
| `attachments/<attachment id>/<file name>` | The original bytes of each attachment |
| `kb-videos.ndjson` | One video of the knowledge base media library per line: `id`, `content_type` (`video/mp4` or `video/webm`), `size`, `sha256`, `path` (the file in the ZIP), `poster_path` (the poster's file, or `null`), `name`, `alt` (its title), `width`, `height`, `duration_ms` (`null` when the file does not say), `codecs`, `kept`, `created_by_agent_id`, `created_at`, `updated_at`, `updated_by_agent_id`, `deleted_at`, `deleted_by_agent_id` |
| `kb-media-replacements.ndjson` | One replacement of a library item per line: `id`, `from_media_id`, `to_media_id`, `article_ids` (the articles that got a new version) and `article_versions` (that version of each), `translations` (`article_id`, `locale`, and the text `before` and `after` of each translation rewritten), `skipped_article_ids` (what an undo left because it changed since), `replaced_by_agent_id`, `created_at`, `undone_at`, `undone_by_agent_id` |
| `kb-images/<image id>.<extension>` | The bytes of each knowledge base image |
| `kb-videos/<video id>.<extension>`, `kb-videos/<video id>.poster.<extension>` | The bytes of each knowledge base video and its poster |

NDJSON means one JSON object per line. Read it line by line. All times are UTC (RFC 3339).

A conversation line has `id`, `contact_id`, `subject`, `status`, `priority`, `assignee_id`
(an agent id or `null`), `channel` (how it started: `email`, `web_form`, `imported`, `agent` or
`problem_report`; see [Filter and sort](inbox.md#filter-and-sort)), `channel_id` (the channel it came
through, or `null`), `channel_scope` (the scope of its channel's type it started in, such as a chat's
`group`, or `null`), `reply_channel_id` (the mailbox its replies keep leaving from, or `null` until the
first reply), `origin` (where an imported ticket came from, or `null`; `origin.mailboxes` lists the
support addresses it was sent to), `tag_ids`,
`created_at`, `updated_at`, `messages`, `time_entries` and `status_changes`.

Each message has `id`, `conversation_id`, `author_agent_id`, `author_name` (as stored: the author
name of an imported message, `null` for a message written in Colliedesk, whose author is
`author_agent_id`, and for received mail, whose sender is in `email_meta`), `kind`
(`incoming`, `outgoing` or `note`), `body`, `body_html`, `origin`, `email_message_id`,
`email_meta` (the sender, authentication result, `via`, Cc addresses and attachment names of a
received message; `via` is `platform` or `workspace` for email and `web_form` for text typed into
the web form, whose sender address is unverified and whose `authentication` is `none`), `cc` (the
addresses a reply was copied to, or `null`), `agent_request_id` (the agent request whose approval sent
this reply, or `null`), `ai_draft_id` (the AI draft it was written from, or `null`), `channel_id` (the
channel that carried it, or `null` for a note and for messages stored before channels were recorded),
`external_id` (its id on that channel, such as `email:<Message-ID>`), `channel_meta` (what the channel
knows of it beyond `email_meta`, or `null`), `decision_basis` and `decision_rule` (why a message from a
channel is in this conversation: what the channel's rules decided, such as `default` and `new_thread`, or
`match` and `thread_key`; `null` for the others), `created_at` and `attachments`. Each attachment entry has `id`, `filename`, `content_type`,
`size`, `sha256`, `created_at` and `path`, the file's location in the ZIP.

Each time entry has `id`, `conversation_id`, `agent_id`, `agent_name`, `minutes`, `note`,
`billable` and `executed_at`.

Each status change has `id`, `from_status`, `to_status`, `changed_at`, `actor_agent_id` (the person
who changed it, or `null` for a customer reply that reopened the conversation) and `source`
(`colliedesk`, or `import` when an import set the status it read from the old helpdesk). Changes are
recorded from the upgrade that added them; a conversation created by an import has no change for
its first status.

Attachment files are stored without compression, under a directory named by the attachment id, so
two files with the same name never collide. File names are cleaned: directories and control
characters are removed, and an empty name becomes `file`. To verify a file, compute its SHA-256
and compare it with `sha256` in `manifest.json`.

## What is not exported

Sessions and API tokens, pairing and invite links, the email channel's inbound secret, a channel's
secrets (`channel_credentials`), queued email delivery state, billing records, and who has a conversation open right now (it is gone
within minutes). They are credentials or server internals, not your data. The reply numbers the API
shows (`reply_number`, `latest_reply_number`) are not exported either: they count a conversation's
`outgoing` messages in the order they were stored, which the file keeps.

## Limits

The rows are read in one snapshot, so a conversation and its messages always agree. Changes made
after the export starts are not in the file. The snapshot is open only while the records are sent;
the attachment files follow without it. Messages are sent one at a time, so a very long thread
does not need extra memory. The list of attachment files is kept in a temporary file on the
server, so the server needs a writable temp directory.
