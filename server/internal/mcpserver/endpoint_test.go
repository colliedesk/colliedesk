package mcpserver_test

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

// toolNames is the list a client sees, written out by hand: the SDK lists tools by name.
var toolNames = []string{
	"colliedesk_agents_list",
	"colliedesk_ai_automatic_limits_set",
	"colliedesk_ai_chat_preview",
	"colliedesk_ai_chat_session_delete",
	"colliedesk_ai_chat_session_get",
	"colliedesk_ai_chat_sessions_list",
	"colliedesk_ai_chat_usage_get",
	"colliedesk_ai_draft_create",
	"colliedesk_ai_draft_discard",
	"colliedesk_ai_draft_get",
	"colliedesk_ai_drafts_list",
	"colliedesk_ai_models_list",
	"colliedesk_ai_provider_add",
	"colliedesk_ai_provider_check",
	"colliedesk_ai_provider_key_set",
	"colliedesk_ai_provider_remove",
	"colliedesk_ai_provider_update",
	"colliedesk_ai_settings_get",
	"colliedesk_ai_slot_set",
	"colliedesk_ai_source_consent_remove",
	"colliedesk_ai_source_consent_set",
	"colliedesk_ai_triage_accept",
	"colliedesk_ai_triage_create",
	"colliedesk_ai_triage_dismiss",
	"colliedesk_ai_triage_get",
	"colliedesk_ai_triage_undo",
	"colliedesk_ai_usage_get",
	"colliedesk_attachment_get",
	"colliedesk_autofix_repo_connect",
	"colliedesk_autofix_repo_disconnect",
	"colliedesk_autofix_repo_notes_exclude",
	"colliedesk_autofix_repo_notes_include_set",
	"colliedesk_autofix_repo_update",
	"colliedesk_autofix_settings_get",
	"colliedesk_autofix_status",
	"colliedesk_billing_action",
	"colliedesk_billing_get",
	"colliedesk_channel_ai_get",
	"colliedesk_channel_ai_intake_evaluation_record",
	"colliedesk_channel_ai_intake_evaluations_list",
	"colliedesk_channel_ai_intake_readiness_get",
	"colliedesk_channel_ai_set",
	"colliedesk_channel_senders",
	"colliedesk_channel_thread_items",
	"colliedesk_channel_threads_list",
	"colliedesk_channel_unresolved_items",
	"colliedesk_channels_check",
	"colliedesk_channels_create",
	"colliedesk_channels_credentials_set",
	"colliedesk_channels_delete",
	"colliedesk_channels_events",
	"colliedesk_channels_get",
	"colliedesk_channels_held_discard",
	"colliedesk_channels_held_replay",
	"colliedesk_channels_list",
	"colliedesk_channels_update",
	"colliedesk_coding_agent_check",
	"colliedesk_coding_agent_disable",
	"colliedesk_coding_agent_enable",
	"colliedesk_contact_create",
	"colliedesk_contact_get",
	"colliedesk_contact_merge",
	"colliedesk_contact_unmerge",
	"colliedesk_contact_update",
	"colliedesk_contacts_find",
	"colliedesk_conversation_chat_get",
	"colliedesk_conversation_create",
	"colliedesk_conversation_fixes_list",
	"colliedesk_conversation_get",
	"colliedesk_conversation_intake_get",
	"colliedesk_conversation_presence",
	"colliedesk_conversation_start",
	"colliedesk_conversation_update",
	"colliedesk_conversations_bulk_update",
	"colliedesk_cursor_key_remove",
	"colliedesk_cursor_key_set",
	"colliedesk_docs_search",
	"colliedesk_email_channel_disconnect",
	"colliedesk_email_channel_get",
	"colliedesk_email_channel_update",
	"colliedesk_email_domain_verify",
	"colliedesk_email_forwarding_confirmation_get",
	"colliedesk_email_secret_rotate",
	"colliedesk_email_test_message_send",
	"colliedesk_fix_cancel",
	"colliedesk_fix_get",
	"colliedesk_fix_mark_deployed",
	"colliedesk_fix_pr_link",
	"colliedesk_fix_pr_unlink",
	"colliedesk_fix_preview",
	"colliedesk_fix_retry",
	"colliedesk_fix_send",
	"colliedesk_fix_unmark_deployed",
	"colliedesk_fixes_list",
	"colliedesk_github_connect",
	"colliedesk_github_disconnect",
	"colliedesk_github_repositories_list",
	"colliedesk_gmail_connect",
	"colliedesk_gmail_disconnect",
	"colliedesk_help_center_domain_check",
	"colliedesk_help_center_domain_get",
	"colliedesk_help_center_domain_remove",
	"colliedesk_help_center_domain_set",
	"colliedesk_help_center_freshdesk_redirect_get",
	"colliedesk_help_center_get",
	"colliedesk_help_center_languages_get",
	"colliedesk_help_center_languages_set",
	"colliedesk_help_center_search_console_get",
	"colliedesk_help_center_search_console_update",
	"colliedesk_help_center_theme_check",
	"colliedesk_help_center_theme_get",
	"colliedesk_help_center_theme_update",
	"colliedesk_help_center_update",
	"colliedesk_import_cancel",
	"colliedesk_import_get",
	"colliedesk_import_key_set",
	"colliedesk_import_people_list",
	"colliedesk_import_person_link",
	"colliedesk_import_person_unlink",
	"colliedesk_import_preview",
	"colliedesk_import_report",
	"colliedesk_import_start",
	"colliedesk_import_undo",
	"colliedesk_imports_list",
	"colliedesk_inbox_activity",
	"colliedesk_inbox_search",
	"colliedesk_intake_decide",
	"colliedesk_intake_get",
	"colliedesk_intake_list",
	"colliedesk_intake_requeue",
	"colliedesk_intake_stats",
	"colliedesk_intake_summary",
	"colliedesk_invite_create",
	"colliedesk_invite_revoke",
	"colliedesk_invites_list",
	"colliedesk_kb_article_ai_exclude",
	"colliedesk_kb_article_ai_include",
	"colliedesk_kb_article_create",
	"colliedesk_kb_article_delete",
	"colliedesk_kb_article_get",
	"colliedesk_kb_article_history",
	"colliedesk_kb_article_undelete",
	"colliedesk_kb_article_update",
	"colliedesk_kb_article_version_restore",
	"colliedesk_kb_articles_search",
	"colliedesk_kb_articles_trash",
	"colliedesk_kb_collection_create",
	"colliedesk_kb_collection_delete",
	"colliedesk_kb_collection_translation_delete",
	"colliedesk_kb_collection_translation_set",
	"colliedesk_kb_collection_undelete",
	"colliedesk_kb_collection_update",
	"colliedesk_kb_collections_list",
	"colliedesk_kb_collections_trash",
	"colliedesk_kb_image_get",
	"colliedesk_kb_image_upload",
	"colliedesk_kb_media_get",
	"colliedesk_kb_media_list",
	"colliedesk_kb_media_purge",
	"colliedesk_kb_media_replace",
	"colliedesk_kb_media_replace_undo",
	"colliedesk_kb_media_trash",
	"colliedesk_kb_media_trash_list",
	"colliedesk_kb_media_undelete",
	"colliedesk_kb_media_update",
	"colliedesk_kb_order_set",
	"colliedesk_kb_translation_create",
	"colliedesk_kb_translation_delete",
	"colliedesk_kb_translation_get",
	"colliedesk_kb_translation_history",
	"colliedesk_kb_translation_undelete",
	"colliedesk_kb_translation_update",
	"colliedesk_kb_translation_version_restore",
	"colliedesk_kb_translations_list",
	"colliedesk_kb_translations_trash",
	"colliedesk_kb_video_poster_set",
	"colliedesk_kb_video_upload_cancel",
	"colliedesk_kb_video_upload_complete",
	"colliedesk_kb_video_upload_list",
	"colliedesk_kb_video_upload_part",
	"colliedesk_kb_video_upload_start",
	"colliedesk_kb_video_upload_status",
	"colliedesk_member_roles_set",
	"colliedesk_message_move_undo",
	"colliedesk_message_moves_list",
	"colliedesk_messages_move",
	"colliedesk_microsoft_connect",
	"colliedesk_microsoft_disconnect",
	"colliedesk_my_display_name_set",
	"colliedesk_my_sla_alerts_set",
	"colliedesk_my_timezone_set",
	"colliedesk_note_add",
	"colliedesk_number_series_list",
	"colliedesk_number_series_set",
	"colliedesk_problem_report",
	"colliedesk_reply_draft",
	"colliedesk_reply_draft_restore",
	"colliedesk_reply_retry",
	"colliedesk_reply_send",
	"colliedesk_response_targets_get",
	"colliedesk_response_targets_set",
	"colliedesk_results_email_set",
	"colliedesk_results_get",
	"colliedesk_results_share_link_create",
	"colliedesk_results_share_link_get",
	"colliedesk_results_share_link_revoke",
	"colliedesk_role_create",
	"colliedesk_role_delete",
	"colliedesk_role_reset",
	"colliedesk_role_restore",
	"colliedesk_role_update",
	"colliedesk_roles_list",
	"colliedesk_saved_replies_list",
	"colliedesk_saved_replies_trash",
	"colliedesk_saved_reply_create",
	"colliedesk_saved_reply_delete",
	"colliedesk_saved_reply_get",
	"colliedesk_saved_reply_history",
	"colliedesk_saved_reply_undelete",
	"colliedesk_saved_reply_update",
	"colliedesk_saved_reply_version_restore",
	"colliedesk_saved_view_create",
	"colliedesk_saved_view_delete",
	"colliedesk_saved_view_restore",
	"colliedesk_saved_view_update",
	"colliedesk_saved_views_list",
	"colliedesk_session_revoke",
	"colliedesk_sessions_list",
	"colliedesk_setup_guide_set",
	"colliedesk_setup_progress_get",
	"colliedesk_tag_delete",
	"colliedesk_tag_rename",
	"colliedesk_tags_list",
	"colliedesk_team_member_update",
	"colliedesk_time_entries_list",
	"colliedesk_token_revoke",
	"colliedesk_web_form_get",
	"colliedesk_web_form_update",
	"colliedesk_whoami",
	"colliedesk_workspace_export",
	"colliedesk_workspace_rename",
	"colliedesk_workspace_settings_get",
	"colliedesk_workspace_settings_update",
	"colliedesk_workspace_slug_change",
}

// wantAnnotations: read only, idempotent, destructive (cannot be undone: it reaches a customer, deletes, or
// revokes).
var wantAnnotations = map[string]struct {
	readOnly, idempotent, destructive bool
}{
	"colliedesk_agents_list":                         {readOnly: true},
	"colliedesk_attachment_get":                      {readOnly: true},
	"colliedesk_contact_create":                      {},
	"colliedesk_contact_get":                         {readOnly: true},
	"colliedesk_contacts_find":                       {readOnly: true},
	"colliedesk_conversation_create":                 {},
	"colliedesk_conversation_start":                  {destructive: true},
	"colliedesk_contact_update":                      {},
	"colliedesk_contact_merge":                       {},
	"colliedesk_contact_unmerge":                     {},
	"colliedesk_conversation_get":                    {readOnly: true},
	"colliedesk_conversation_presence":               {idempotent: true},
	"colliedesk_conversation_update":                 {idempotent: true},
	"colliedesk_conversations_bulk_update":           {idempotent: true},
	"colliedesk_docs_search":                         {readOnly: true},
	"colliedesk_inbox_activity":                      {readOnly: true},
	"colliedesk_inbox_search":                        {readOnly: true},
	"colliedesk_note_add":                            {},
	"colliedesk_reply_retry":                         {destructive: true},
	"colliedesk_reply_send":                          {destructive: true},
	"colliedesk_saved_replies_list":                  {readOnly: true},
	"colliedesk_saved_replies_trash":                 {readOnly: true},
	"colliedesk_saved_reply_get":                     {readOnly: true},
	"colliedesk_saved_reply_history":                 {readOnly: true},
	"colliedesk_saved_reply_create":                  {},
	"colliedesk_saved_reply_update":                  {},
	"colliedesk_saved_reply_delete":                  {},
	"colliedesk_saved_reply_undelete":                {},
	"colliedesk_saved_reply_version_restore":         {},
	"colliedesk_help_center_get":                     {readOnly: true},
	"colliedesk_help_center_languages_get":           {readOnly: true},
	"colliedesk_help_center_languages_set":           {idempotent: true},
	"colliedesk_kb_translations_list":                {readOnly: true},
	"colliedesk_kb_translations_trash":               {readOnly: true},
	"colliedesk_kb_translation_create":               {},
	"colliedesk_kb_translation_get":                  {readOnly: true},
	"colliedesk_kb_translation_update":               {},
	"colliedesk_kb_translation_delete":               {},
	"colliedesk_kb_translation_undelete":             {},
	"colliedesk_kb_translation_history":              {readOnly: true},
	"colliedesk_kb_translation_version_restore":      {},
	"colliedesk_kb_collection_translation_set":       {idempotent: true},
	"colliedesk_kb_collection_translation_delete":    {},
	"colliedesk_help_center_update":                  {idempotent: true},
	"colliedesk_help_center_freshdesk_redirect_get":  {readOnly: true},
	"colliedesk_help_center_search_console_get":      {readOnly: true},
	"colliedesk_help_center_search_console_update":   {idempotent: true},
	"colliedesk_help_center_theme_get":               {readOnly: true},
	"colliedesk_help_center_theme_check":             {readOnly: true},
	"colliedesk_help_center_theme_update":            {idempotent: true},
	"colliedesk_help_center_domain_get":              {readOnly: true},
	"colliedesk_help_center_domain_set":              {idempotent: true},
	"colliedesk_help_center_domain_check":            {idempotent: true},
	"colliedesk_help_center_domain_remove":           {idempotent: true},
	"colliedesk_imports_list":                        {readOnly: true},
	"colliedesk_import_get":                          {readOnly: true},
	"colliedesk_import_preview":                      {},
	"colliedesk_import_start":                        {},
	"colliedesk_import_cancel":                       {idempotent: true},
	"colliedesk_import_key_set":                      {},
	"colliedesk_import_report":                       {readOnly: true},
	"colliedesk_import_undo":                         {destructive: true},
	"colliedesk_import_people_list":                  {readOnly: true},
	"colliedesk_import_person_link":                  {idempotent: true},
	"colliedesk_import_person_unlink":                {idempotent: true},
	"colliedesk_kb_article_ai_exclude":               {idempotent: true},
	"colliedesk_kb_article_ai_include":               {idempotent: true},
	"colliedesk_kb_article_create":                   {},
	"colliedesk_kb_article_delete":                   {},
	"colliedesk_kb_article_get":                      {readOnly: true},
	"colliedesk_kb_article_history":                  {readOnly: true},
	"colliedesk_kb_article_undelete":                 {},
	"colliedesk_kb_article_update":                   {},
	"colliedesk_kb_article_version_restore":          {},
	"colliedesk_kb_articles_search":                  {readOnly: true},
	"colliedesk_kb_articles_trash":                   {readOnly: true},
	"colliedesk_kb_collection_create":                {},
	"colliedesk_kb_collection_delete":                {},
	"colliedesk_kb_collection_undelete":              {},
	"colliedesk_kb_collection_update":                {},
	"colliedesk_kb_collections_list":                 {readOnly: true},
	"colliedesk_kb_collections_trash":                {readOnly: true},
	"colliedesk_kb_image_get":                        {readOnly: true},
	"colliedesk_kb_image_upload":                     {idempotent: true},
	"colliedesk_kb_order_set":                        {idempotent: true},
	"colliedesk_kb_media_list":                       {readOnly: true},
	"colliedesk_kb_media_get":                        {readOnly: true},
	"colliedesk_kb_media_update":                     {},
	"colliedesk_kb_media_trash":                      {idempotent: true},
	"colliedesk_kb_media_trash_list":                 {readOnly: true},
	"colliedesk_kb_media_undelete":                   {idempotent: true},
	"colliedesk_kb_media_purge":                      {destructive: true, idempotent: true},
	"colliedesk_kb_media_replace":                    {},
	"colliedesk_kb_media_replace_undo":               {idempotent: true},
	"colliedesk_kb_video_upload_start":               {},
	"colliedesk_kb_video_upload_part":                {},
	"colliedesk_kb_video_upload_status":              {readOnly: true},
	"colliedesk_kb_video_upload_list":                {readOnly: true},
	"colliedesk_kb_video_upload_cancel":              {idempotent: true},
	"colliedesk_kb_video_upload_complete":            {idempotent: true},
	"colliedesk_kb_video_poster_set":                 {idempotent: true},
	"colliedesk_saved_views_list":                    {readOnly: true},
	"colliedesk_saved_view_create":                   {},
	"colliedesk_saved_view_update":                   {idempotent: true},
	"colliedesk_saved_view_delete":                   {idempotent: true},
	"colliedesk_saved_view_restore":                  {idempotent: true},
	"colliedesk_tag_delete":                          {destructive: true},
	"colliedesk_tag_rename":                          {},
	"colliedesk_tags_list":                           {readOnly: true},
	"colliedesk_time_entries_list":                   {readOnly: true},
	"colliedesk_token_revoke":                        {destructive: true},
	"colliedesk_whoami":                              {readOnly: true},
	"colliedesk_billing_action":                      {},
	"colliedesk_email_channel_disconnect":            {destructive: true},
	"colliedesk_channels_list":                       {readOnly: true},
	"colliedesk_channels_get":                        {readOnly: true},
	"colliedesk_channels_events":                     {readOnly: true},
	"colliedesk_channels_create":                     {},
	"colliedesk_channels_update":                     {idempotent: true},
	"colliedesk_channels_check":                      {idempotent: true},
	"colliedesk_channels_held_replay":                {},
	"colliedesk_channels_credentials_set":            {},
	"colliedesk_channels_delete":                     {destructive: true},
	"colliedesk_channels_held_discard":               {destructive: true},
	"colliedesk_email_secret_rotate":                 {destructive: true},
	"colliedesk_gmail_connect":                       {},
	"colliedesk_gmail_disconnect":                    {destructive: true},
	"colliedesk_microsoft_connect":                   {},
	"colliedesk_microsoft_disconnect":                {destructive: true},
	"colliedesk_problem_report":                      {},
	"colliedesk_reply_draft":                         {},
	"colliedesk_reply_draft_restore":                 {idempotent: true},
	"colliedesk_workspace_export":                    {},
	"colliedesk_workspace_slug_change":               {idempotent: true},
	"colliedesk_number_series_list":                  {readOnly: true},
	"colliedesk_number_series_set":                   {idempotent: true},
	"colliedesk_response_targets_get":                {readOnly: true},
	"colliedesk_response_targets_set":                {idempotent: true},
	"colliedesk_workspace_rename":                    {idempotent: true},
	"colliedesk_workspace_settings_get":              {readOnly: true},
	"colliedesk_workspace_settings_update":           {idempotent: true},
	"colliedesk_my_timezone_set":                     {idempotent: true},
	"colliedesk_my_sla_alerts_set":                   {idempotent: true},
	"colliedesk_my_display_name_set":                 {idempotent: true},
	"colliedesk_team_member_update":                  {},
	"colliedesk_invites_list":                        {readOnly: true},
	"colliedesk_invite_create":                       {},
	"colliedesk_invite_revoke":                       {destructive: true},
	"colliedesk_sessions_list":                       {readOnly: true},
	"colliedesk_session_revoke":                      {destructive: true},
	"colliedesk_web_form_get":                        {readOnly: true},
	"colliedesk_web_form_update":                     {idempotent: true},
	"colliedesk_email_channel_get":                   {readOnly: true},
	"colliedesk_email_channel_update":                {idempotent: true},
	"colliedesk_email_domain_verify":                 {idempotent: true},
	"colliedesk_email_test_message_send":             {},
	"colliedesk_email_forwarding_confirmation_get":   {readOnly: true},
	"colliedesk_billing_get":                         {readOnly: true},
	"colliedesk_ai_settings_get":                     {readOnly: true},
	"colliedesk_ai_models_list":                      {readOnly: true},
	"colliedesk_ai_provider_add":                     {},
	"colliedesk_ai_provider_update":                  {idempotent: true},
	"colliedesk_ai_provider_key_set":                 {},
	"colliedesk_ai_provider_check":                   {},
	"colliedesk_ai_provider_remove":                  {destructive: true},
	"colliedesk_ai_slot_set":                         {idempotent: true},
	"colliedesk_ai_source_consent_set":               {idempotent: true},
	"colliedesk_ai_source_consent_remove":            {idempotent: true},
	"colliedesk_channel_ai_get":                      {readOnly: true},
	"colliedesk_conversation_chat_get":               {readOnly: true},
	"colliedesk_channel_threads_list":                {readOnly: true},
	"colliedesk_channel_thread_items":                {readOnly: true},
	"colliedesk_channel_senders":                     {readOnly: true},
	"colliedesk_channel_unresolved_items":            {readOnly: true},
	"colliedesk_intake_list":                         {readOnly: true},
	"colliedesk_intake_get":                          {readOnly: true},
	"colliedesk_intake_decide":                       {},
	"colliedesk_intake_requeue":                      {idempotent: true},
	"colliedesk_intake_stats":                        {readOnly: true},
	"colliedesk_intake_summary":                      {readOnly: true},
	"colliedesk_conversation_intake_get":             {readOnly: true},
	"colliedesk_messages_move":                       {},
	"colliedesk_message_moves_list":                  {readOnly: true},
	"colliedesk_message_move_undo":                   {idempotent: true},
	"colliedesk_channel_ai_set":                      {idempotent: true},
	"colliedesk_channel_ai_intake_readiness_get":     {readOnly: true},
	"colliedesk_channel_ai_intake_evaluations_list":  {readOnly: true},
	"colliedesk_channel_ai_intake_evaluation_record": {},
	"colliedesk_ai_usage_get":                        {readOnly: true},
	"colliedesk_ai_automatic_limits_set":             {idempotent: true},
	"colliedesk_ai_chat_usage_get":                   {readOnly: true},
	"colliedesk_ai_chat_sessions_list":               {readOnly: true},
	"colliedesk_ai_chat_session_get":                 {readOnly: true},
	"colliedesk_ai_chat_session_delete":              {destructive: true},
	"colliedesk_ai_chat_preview":                     {},
	"colliedesk_ai_draft_create":                     {},
	"colliedesk_ai_draft_get":                        {readOnly: true},
	"colliedesk_ai_drafts_list":                      {readOnly: true},
	"colliedesk_ai_draft_discard":                    {idempotent: true},
	"colliedesk_ai_triage_get":                       {readOnly: true},
	"colliedesk_ai_triage_create":                    {},
	"colliedesk_ai_triage_accept":                    {idempotent: true},
	"colliedesk_ai_triage_dismiss":                   {idempotent: true},
	"colliedesk_ai_triage_undo":                      {idempotent: true},
	"colliedesk_results_get":                         {readOnly: true},
	"colliedesk_results_share_link_get":              {readOnly: true},
	"colliedesk_results_share_link_create":           {},
	"colliedesk_results_share_link_revoke":           {idempotent: true},
	"colliedesk_results_email_set":                   {idempotent: true},
	"colliedesk_setup_progress_get":                  {readOnly: true},
	"colliedesk_setup_guide_set":                     {idempotent: true},
	"colliedesk_autofix_status":                      {readOnly: true},
	"colliedesk_fixes_list":                          {readOnly: true},
	"colliedesk_fix_get":                             {readOnly: true},
	"colliedesk_conversation_fixes_list":             {readOnly: true},
	"colliedesk_autofix_settings_get":                {readOnly: true},
	"colliedesk_github_repositories_list":            {readOnly: true},
	"colliedesk_github_connect":                      {},
	"colliedesk_github_disconnect":                   {destructive: true},
	"colliedesk_autofix_repo_connect":                {idempotent: true},
	"colliedesk_autofix_repo_update":                 {idempotent: true},
	"colliedesk_autofix_repo_disconnect":             {destructive: true},
	"colliedesk_autofix_repo_notes_include_set":      {idempotent: true},
	"colliedesk_autofix_repo_notes_exclude":          {idempotent: true},
	"colliedesk_fix_preview":                         {readOnly: true},
	"colliedesk_fix_send":                            {},
	"colliedesk_fix_retry":                           {},
	"colliedesk_fix_cancel":                          {},
	"colliedesk_fix_mark_deployed":                   {idempotent: true},
	"colliedesk_fix_unmark_deployed":                 {idempotent: true},
	"colliedesk_fix_pr_link":                         {},
	"colliedesk_fix_pr_unlink":                       {},
	"colliedesk_coding_agent_enable":                 {idempotent: true},
	"colliedesk_roles_list":                          {readOnly: true},
	"colliedesk_role_create":                         {},
	"colliedesk_role_update":                         {idempotent: true},
	"colliedesk_role_delete":                         {},
	"colliedesk_role_reset":                          {idempotent: true},
	"colliedesk_role_restore":                        {},
	"colliedesk_member_roles_set":                    {idempotent: true},
}

func TestToolsListIsTheCatalogInFixedOrderWithAnnotations(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("List Desk", "admin@list.example.com")
	for _, version := range []string{"", "2025-11-25"} {
		session := e.connect(w.read, version)
		if info := session.InitializeResult().ServerInfo; info.Name != "colliedesk" || info.Version != testVersion {
			t.Errorf("protocol %q: serverInfo=%+v, want colliedesk %s", version, info, testVersion)
		}
		for round := 0; round < 2; round++ {
			listed, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatalf("protocol %q: list tools: %v", version, err)
			}
			var names []string
			for _, tool := range listed.Tools {
				names = append(names, tool.Name)
			}
			if !slices.Equal(names, toolNames) {
				t.Fatalf("protocol %q round %d: tools=%v, want %v", version, round, names, toolNames)
			}
			for _, tool := range listed.Tools {
				want := wantAnnotations[tool.Name]
				a := tool.Annotations
				if a == nil || a.ReadOnlyHint != want.readOnly || a.IdempotentHint != want.idempotent || a.Title == "" || tool.Title == "" {
					t.Errorf("%s: annotations=%+v title=%q, want readOnly=%v idempotent=%v and a title", tool.Name, a, tool.Title, want.readOnly, want.idempotent)
				}
				if !want.readOnly && (a == nil || a.DestructiveHint == nil || *a.DestructiveHint != want.destructive) {
					t.Errorf("%s: a writing tool must state destructiveHint=%v", tool.Name, want.destructive)
				}
				if want.destructive && !strings.Contains(tool.Description, "cannot be undone") {
					t.Errorf("%s: a destructive tool must say it cannot be undone\n%s", tool.Name, tool.Description)
				}
				if !strings.Contains(tool.Description, "\n\nNeeds: ") {
					t.Errorf("%s: description does not end with what it needs\n%s", tool.Name, tool.Description)
				}
				if tool.InputSchema == nil || tool.OutputSchema == nil {
					t.Errorf("%s: missing input or output schema", tool.Name)
				}
			}
		}
	}
}

// whoami names the person a token acts as and their roles, how the token got its permissions, and the
// permissions it has now; tools_you_can_use lists exactly the tools those permissions open.
func TestWhoamiNamesTheRoleAndThePermissions(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Whoami Desk", "admin@whoami.example.com")
	for name, token := range map[string]string{"read": w.read, "draft": w.draft, "admin": w.adminTier} {
		out := e.ok(e.connect(token, ""), "colliedesk_whoami", nil)
		if got := at(t, out, "agent", "email"); got != "admin@whoami.example.com" {
			t.Errorf("%s: agent email=%v", name, got)
		}
		if roles, _ := at(t, out, "agent", "roles").([]any); !slices.Contains(roles, any("Admin")) {
			t.Errorf("%s: agent roles=%v, want Admin", name, roles)
		}
		if got := at(t, out, "workspace", "name"); got != "Whoami Desk" {
			t.Errorf("%s: workspace=%v", name, got)
		}
		if got := at(t, out, "workspace", "timezone"); got != "UTC" {
			t.Errorf("%s: timezone=%v", name, got)
		}
		if meaning, _ := out["permissions_meaning"].(string); !strings.Contains(meaning, "permission_missing") {
			t.Errorf("%s: permissions_meaning=%q, want it to say how a refusal reads", name, meaning)
		}
		held := []string{}
		for _, permission := range at(t, out, "permissions").([]any) {
			held = append(held, permission.(string))
		}
		tools, _ := at(t, out, "tools_you_can_use").([]any)
		for _, spec := range mcpserver.Catalog() {
			open := !spec.Operator && !slices.ContainsFunc(spec.Needs, func(need string) bool { return !slices.Contains(held, need) })
			if listed := slices.Contains(tools, any(spec.Name)); listed != open {
				t.Errorf("%s: %s listed=%v, but it needs %v and the token holds %v", name, spec.Name, listed, spec.Needs, held)
			}
		}
	}
	read := e.ok(e.connect(w.read, ""), "colliedesk_whoami", nil)
	if at(t, read, "token", "kind") != "custom" || at(t, read, "token", "role") != nil || slices.Contains(at(t, read, "permissions").([]any), any("conversations:update")) {
		t.Errorf("read token: %v, want a custom token without conversations:update", read)
	}
	admin := e.ok(e.connect(w.adminTier, ""), "colliedesk_whoami", nil)
	if at(t, admin, "token", "kind") != "role" || at(t, admin, "token", "role") != "Admin" || !slices.Contains(at(t, admin, "permissions").([]any), any("team:manage_roles")) {
		t.Errorf("admin token: %v, want a token following Admin with team:manage_roles", admin)
	}
}

func TestMCPRequiresABearerTokenAndNeverACookie(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Gate Desk", "admin@gate.example.com")
	initialize := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

	post := func(headers map[string]string, cookie *http.Cookie) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, e.mcpURL(), strings.NewReader(initialize))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		return response
	}
	cases := []struct {
		name    string
		headers map[string]string
		cookie  *http.Cookie
	}{
		{"no credential", nil, nil},
		{"empty bearer", map[string]string{"Authorization": "Bearer "}, nil},
		{"unknown token", map[string]string{"Authorization": "Bearer not-a-real-token"}, nil},
		{"wrong scheme", map[string]string{"Authorization": "Basic " + w.read}, nil},
		{"person session as cookie", nil, &http.Cookie{Name: "colliedesk_session", Value: w.admin}},
		{"agent token as cookie", nil, &http.Cookie{Name: "colliedesk_session", Value: w.read}},
		{"valid cookie with a bad bearer", map[string]string{"Authorization": "Bearer nope"}, &http.Cookie{Name: "colliedesk_session", Value: w.admin}},
		{"person session as bearer", map[string]string{"Authorization": "Bearer " + w.admin}, nil},
	}
	for _, c := range cases {
		response := post(c.headers, c.cookie)
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(response.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s: status=%d WWW-Authenticate=%q body=%s, want 401 with a Bearer challenge", c.name, response.StatusCode, response.Header.Get("WWW-Authenticate"), body)
		}
	}
	if response := post(map[string]string{"Authorization": "Bearer " + w.read}, nil); response.StatusCode != http.StatusOK {
		t.Errorf("agent token as bearer: status=%d, want 200", response.StatusCode)
	}
	// Stateless: there is no stream to open and no session to end.
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, _ := http.NewRequest(method, e.mcpURL(), nil)
		req.Header.Set("Authorization", "Bearer "+w.read)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /mcp: status=%d, want 405", method, response.StatusCode)
		}
	}
}

func TestMCPServesTheHostNameOfAReverseProxyOnALoopbackListener(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Proxy Desk", "admin@proxy.example.com")
	req, _ := http.NewRequest(http.MethodPost, e.mcpURL(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Host = "desk.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+w.read)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status=%d body=%s, want 200: a proxy on the same host forwards the public Host header", response.StatusCode, body)
	}
}

// Only a failing database is a reason for 503. Every way a token can be refused is a 401, so a client
// knows to ask for a new token instead of retrying.
func TestMCPAnswers401ForEveryRefusedToken(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Refused Desk", "admin@refused.example.com")
	issue := func(label string) string {
		status, issued := e.rest(http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": label, "permissions": readPermissions}, w.admin)
		if status != http.StatusCreated {
			t.Fatalf("create token %q status=%d body=%v", label, status, issued)
		}
		return issued["token"].(string)
	}
	age := func(label, set string) {
		if _, err := e.pool.Exec(context.Background(), `UPDATE sessions SET `+set+` WHERE workspace_id=$1 AND label=$2`, w.id, label); err != nil {
			t.Fatal(err)
		}
	}
	pastTheCap, expired, revoked := issue("past the cap"), issue("expired"), issue("revoked")
	age("past the cap", "created_at=now()-interval '366 days'")
	age("expired", "expires_at=now()-interval '1 minute'")
	age("revoked", "revoked_at=now()")

	for name, token := range map[string]string{"past the one-year cap": pastTheCap, "expired": expired, "revoked": revoked, "unknown": "cdk_agent_" + strings.Repeat("A", 43)} {
		req, err := http.NewRequest(http.MethodPost, e.mcpURL(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(response.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s token: status=%d WWW-Authenticate=%q body=%s, want 401 with a Bearer challenge", name, response.StatusCode, response.Header.Get("WWW-Authenticate"), body)
		}
	}
}
