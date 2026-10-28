import type { AutofixSettings, ConnectorState, Fix, FixDetail, FixEvent, FixPreview, Repo } from './autofix-api';

// Shared test data for the Autofix screens: a fix in any state, a workspace's settings and a preview.

export const now = Date.parse('2026-10-27T12:00:00Z');
export const conversationId = '11111111-1111-4111-8111-111111111111';
export const repoId = '22222222-2222-4222-8222-222222222222';
export const fixId = '3f9c0000-0000-4000-8000-000000000001';

export function fixOf(overrides: Partial<Fix> = {}): Fix {
  return {
    id: fixId, conversation_id: conversationId, attempt_no: 1, previous_attempt_id: null, repo_id: repoId, repo_full_name: 'northwind/web',
    connector: 'claude_issue', state: 'sent', outcome_reason: null, state_changed_at: '2026-10-27T11:50:00Z', row_version: 3, stale: false, group: 'running',
    sent_title: 'CSV export stops at 500 rows', sent_message_ids: [], sent_notes: false, created_by_kind: 'person', created_by_agent_id: null,
    created_by_name: 'Mai Tran', approved_request_id: null, github_login: 'mai-tran', issue_number: 87,
    issue_url: 'https://github.com/northwind/web/issues/87', vendor_url: 'https://github.com/northwind/web/issues/87', branch: null, pr_number: null, pr_url: null,
    pr_linked_by: null, merge_sha: null, merged_at: null, merged_by_login: null, deployed_at: null, deployed_by_name: null, created_at: '2026-10-27T11:02:00Z',
    conversation_subject: 'CSV export stops at 500 rows', contact_name: 'Tomás Vidal', copilot_assignment: null,
    ...overrides,
  };
}

export const withPR = { pr_number: 412, pr_url: 'https://github.com/northwind/web/pull/412', pr_linked_by: 'auto' as const };

export function eventOf(overrides: Partial<FixEvent> = {}): FixEvent {
  return { id: 1, kind: 'state_changed', from_state: 'sending', to_state: 'sent', actor_kind: 'github', actor_name: null, data: {}, created_at: '2026-10-27T11:02:00Z', ...overrides };
}

export function detailOf(overrides: Partial<Fix> = {}, events: FixEvent[] = [], sentBody = 'body'): FixDetail {
  return { ...fixOf(overrides), sent_body: sentBody, events };
}

export function repoOf(overrides: Partial<Repo> = {}): Repo {
  return {
    id: repoId, installation_id: 41, github_repo_id: 901, full_name: 'northwind/web', visibility: 'private', visibility_checked_at: '2026-10-27T10:00:00Z',
    default_branch: 'main', include_notes: true, default_connector: 'claude_issue', paused_reason: null, paused_at: null, connected_by_name: 'Mai Tran',
    connected_at: '2026-10-12T10:00:00Z', ...overrides,
  };
}

export function connectorOf(overrides: Partial<ConnectorState> = {}): ConnectorState {
  return {
    kind: 'claude_issue', enabled: true, consent: { version: 'v1', name: 'Mai Tran', at: '2026-10-12T10:00:00Z' }, credential: null,
    last_check: { at: '2026-10-27T11:40:00Z', result: 'ok', message: null }, mode: null, ...overrides,
  };
}

export function settingsOf(overrides: Partial<AutofixSettings> = {}): AutofixSettings {
  return {
    available: true, consent_version: 'v1', app_slug: 'colliedesk',
    installations: [{ id: '44444444-4444-4444-8444-444444444444', installation_id: 41, account_login: 'northwind', account_type: 'Organization',
      repository_selection: 'selected', status: 'active', connected_by_name: 'Mai Tran', connected_at: '2026-10-12T10:00:00Z',
      settings_url: 'https://github.com/organizations/northwind/settings/installations/41' }],
    repos: [repoOf()],
    connectors: [connectorOf(), connectorOf({ kind: 'copilot_issue', mode: 'assign_api' }),
      connectorOf({ kind: 'cursor_api', credential: { set_by_name: 'Mai Tran', set_at: '2026-10-14T10:00:00Z' } })],
    ...overrides,
  };
}

export function previewOf(overrides: Partial<FixPreview> = {}): FixPreview {
  return {
    title: 'CSV export stops at 500 rows',
    body: '## Task\nFind the cause.\n\n<untrusted>\nThe file ends at 500 rows. Call me on [phone removed].\n</untrusted>',
    message_ids: ['55555555-5555-4555-8555-555555555555'],
    notes: [{ message_id: '66666666-6666-4666-8666-666666666666', author_name: 'Mai Tran', created_at: '2026-10-27T11:00:00Z', excerpt: 'Reproduced on our account too: 500 rows exactly.', included: false }],
    notes_allowed: true, attachments_left_out: 1,
    destination: { repo_full_name: 'northwind/web', connector: 'claude_issue', sends_as: 'person', vendor_retention: 'The text stays in the GitHub issue until someone deletes it.' },
    removed: { emails: 1, phones: 1, cards: 0, secrets: 1, mentions: 1, references: 1 },
    ...overrides,
  };
}
