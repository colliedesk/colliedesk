import type { AgentRequest } from '../../api/schemas';

// agentRequest is a pending request of the Claude triage token, with overrides.
export function agentRequest(overrides: Partial<AgentRequest> = {}): AgentRequest {
  return {
    id: '11111111-1111-4111-8111-111111111111', kind: 'action', status: 'pending', method: 'PATCH', route: '/api/v1/conversations/{id}',
    path: '/api/v1/conversations/22222222-2222-4222-8222-222222222222', body: { status: 'pending' }, summary: 'Change a conversation: status pending',
    conversation_id: '22222222-2222-4222-8222-222222222222',
    requested_by: { token_id: '33333333-3333-4333-8333-333333333333', token_label: 'Claude triage', token_scope: 'read', token_revoked: false,
      agent_id: '44444444-4444-4444-8444-444444444444', agent_name: 'Ada Admin' },
    decided_by: null, decided_at: null, reason: null, result_status: null, result: null, result_note: null,
    created_at: '2026-10-17T09:00:00Z', expires_at: '2026-10-24T09:00:00Z',
    ...overrides,
  };
}
