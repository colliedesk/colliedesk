-- Autofix: the first workspace connected a GitHub installation and two repositories (one paused when
-- it turned public), turned Claude and Cursor on (Cursor with a sealed key), and sent conversation 1
-- twice: a cancelled attempt, then a retry by a person that was merged and released. The second
-- workspace has an installation that reaches every repository (blocked) and an attempt an agent token
-- asked for and a person approved, still waiting for the person's GitHub authorization. One delivery
-- was applied.
INSERT INTO autofix_github_installations (id, workspace_id, installation_id, account_login, account_type, repository_selection, status, connected_by_agent_id, connected_by_name, connected_at, updated_at) VALUES
    (md5('afi-acme-1')::uuid, md5('ws-acme')::uuid, 41001, 'acme-co', 'Organization', 'selected', 'active', md5('agent-acme-admin')::uuid, 'Acme Admin', now() - interval '20 days', now() - interval '20 days'),
    (md5('afi-beta-1')::uuid, md5('ws-beta')::uuid, 41002, 'beta-dev', 'User', 'all', 'blocked_all_repositories', md5('agent-beta-admin')::uuid, 'Beta Admin', now() - interval '2 days', now() - interval '2 days');

INSERT INTO autofix_repos (id, workspace_id, installation_id, github_repo_id, full_name, visibility, visibility_checked_at, default_branch, include_notes, default_connector, paused_reason, paused_at, connected_by_agent_id, connected_by_name, connected_at) VALUES
    (md5('afr-acme-1')::uuid, md5('ws-acme')::uuid, 41001, 51001, 'acme-co/shop-web', 'private', now() - interval '1 day', 'main', true, 'claude_issue', NULL, NULL, md5('agent-acme-admin')::uuid, 'Acme Admin', now() - interval '20 days'),
    (md5('afr-acme-2')::uuid, md5('ws-acme')::uuid, 41001, 51002, 'acme-co/docs', 'public', now() - interval '3 days', 'trunk', false, NULL, 'became_public', now() - interval '3 days', md5('agent-acme-admin')::uuid, 'Acme Admin', now() - interval '19 days'),
    (md5('afr-beta-1')::uuid, md5('ws-beta')::uuid, NULL, 52001, 'beta-dev/app', 'private', now() - interval '2 days', 'main', false, 'copilot_issue', 'removed_from_app', now() - interval '1 day', md5('agent-beta-admin')::uuid, 'Beta Admin', now() - interval '2 days');

INSERT INTO autofix_connectors (workspace_id, kind, enabled, mode, consent_agent_id, consent_name, consent_at, consent_version, credential_key_id, credential_sealed, credential_set_by_agent_id, credential_set_by_name, credential_set_at, last_check_at, last_check_result, last_check_message, updated_at) VALUES
    (md5('ws-acme')::uuid, 'claude_issue', true, NULL, md5('agent-acme-admin')::uuid, 'Acme Admin', now() - interval '19 days', '2026-10-27', NULL, NULL, NULL, NULL, NULL, now() - interval '1 day', 'ok', NULL, now() - interval '1 day'),
    (md5('ws-acme')::uuid, 'cursor_api', true, NULL, md5('agent-acme-admin')::uuid, 'Acme Admin', now() - interval '18 days', '2026-10-27', 'k1', decode('00112233445566778899aabbccddeeff', 'hex'), md5('agent-acme-admin')::uuid, 'Acme Admin', now() - interval '18 days', now() - interval '2 hours', 'key_invalid', 'Cursor answered 401: the key was revoked.', now() - interval '2 hours'),
    (md5('ws-beta')::uuid, 'copilot_issue', false, 'assign_on_github', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, now() - interval '2 days');

INSERT INTO fix_attempts (id, workspace_id, conversation_id, attempt_no, previous_attempt_id, repo_id, repo_full_name, connector, state, outcome_reason, state_changed_at, row_version,
        sent_title, sent_body, sent_message_ids, sent_notes, client_key, created_by_kind, created_by_agent_id, created_by_name, created_by_token_id, approved_request_id,
        github_user_id, github_login, issue_number, issue_node_id, vendor_ref, vendor_run_id, vendor_url, branch, pr_number, pr_url, pr_linked_by,
        merge_sha, merged_at, merged_by_login, deployed_at, deployed_by_agent_id, deployed_by_name, dispatch_started_at, next_check_at, check_failures, created_at) VALUES
    (md5('fix-acme-1')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 1, NULL, md5('afr-acme-1')::uuid, 'acme-co/shop-web', 'cursor_api', 'cancelled', 'cancelled_by_person', now() - interval '6 days', 3,
        'Checkout button does nothing', 'The checkout button does nothing after the coupon step.', ARRAY[md5('msg-acme-1-1')::uuid], false, 'client-acme-1', 'person', md5('agent-acme-ben')::uuid, 'Ben', NULL, NULL,
        NULL, NULL, NULL, NULL, 'bc-' || md5('fix-acme-1')::uuid, 'run-acme-1', 'https://cursor.com/agents/bc-acme-1', NULL, NULL, NULL, NULL,
        NULL, NULL, NULL, NULL, NULL, NULL, now() - interval '7 days', NULL, 1, now() - interval '7 days'),
    (md5('fix-acme-2')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 2, md5('fix-acme-1')::uuid, md5('afr-acme-1')::uuid, 'acme-co/shop-web', 'claude_issue', 'deployed', NULL, now() - interval '1 day', 6,
        'Checkout button does nothing', 'The checkout button does nothing after the coupon step. Seen on Safari.', ARRAY[md5('msg-acme-1-1')::uuid], true, 'client-acme-2', 'person', md5('agent-acme-ben')::uuid, 'Ben', NULL, NULL,
        9001, 'ben-acme', 412, 'I_kwDOacme412', NULL, NULL, 'https://github.com/acme-co/shop-web/issues/412', 'claude/issue-412-20261020', 413, 'https://github.com/acme-co/shop-web/pull/413', 'auto',
        '0123456789abcdef0123456789abcdef01234567', now() - interval '2 days', 'acme-lead', now() - interval '1 day', md5('agent-acme-admin')::uuid, 'Acme Admin', now() - interval '6 days', now() + interval '10 minutes', 0, now() - interval '6 days'),
    (md5('fix-beta-1')::uuid, md5('ws-beta')::uuid, md5('conv-beta-1')::uuid, 1, NULL, md5('afr-beta-1')::uuid, 'beta-dev/app', 'copilot_issue', 'sending', NULL, now() - interval '5 minutes', 1,
        'Login link expired', 'The login link says it expired on the first click.', '{}', false, NULL, 'token', md5('agent-beta-cy')::uuid, 'Cy', md5('token-beta-draft')::uuid, md5('request-beta-approved')::uuid,
        NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL,
        NULL, NULL, NULL, NULL, NULL, NULL, NULL, now() + interval '10 minutes', 0, now() - interval '5 minutes');

INSERT INTO fix_attempt_events (workspace_id, fix_attempt_id, kind, from_state, to_state, actor_kind, actor_id, actor_name, data, delivery_id, created_at) VALUES
    (md5('ws-acme')::uuid, md5('fix-acme-1')::uuid, 'state_changed', 'sent', 'cancelled', 'person', md5('agent-acme-ben')::uuid::text, 'Ben', '{"reason": "cancelled_by_person"}', NULL, now() - interval '6 days'),
    (md5('ws-acme')::uuid, md5('fix-acme-2')::uuid, 'state_changed', 'pr_open', 'merged', 'github', 'acme-lead', 'acme-lead', '{"pr_number": 413}', 'delivery-acme-1', now() - interval '2 days'),
    (md5('ws-beta')::uuid, md5('fix-beta-1')::uuid, 'created', NULL, NULL, 'token', md5('token-beta-draft')::uuid::text, 'Claude (draft)', '{}', NULL, now() - interval '5 minutes');

INSERT INTO autofix_oauth_states (state_hash, workspace_id, agent_id, purpose, fix_attempt_id, code_verifier, expires_at, created_at) VALUES
    (decode(md5('oauth-beta-1') || md5('oauth-beta-1b'), 'hex'), md5('ws-beta')::uuid, md5('agent-beta-cy')::uuid, 'dispatch', md5('fix-beta-1')::uuid, repeat('v', 43), now() + interval '5 minutes', now() - interval '5 minutes');

INSERT INTO autofix_webhook_deliveries (source, delivery_id, received_at) VALUES
    ('github', 'delivery-acme-1', now() - interval '2 days');
UPDATE fix_attempts SET copilot_assignment = 'on_github' WHERE id = md5('fix-beta-1')::uuid;
