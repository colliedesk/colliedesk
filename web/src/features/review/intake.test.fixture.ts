import type { ChatItem, IntakeCandidate, IntakeGathering, IntakeGatheringDetail } from '../../api/intake';

export const CHANNEL = '11111111-1111-4111-8111-111111111111';
export const CARD = '22222222-2222-4222-8222-222222222222';
export const CONVERSATION = '33333333-3333-4333-8333-333333333333';
export const OTHER_CONVERSATION = '44444444-4444-4444-8444-444444444444';
export const AGENT = '55555555-5555-4555-8555-555555555555';

export const gathering = (overrides: Partial<IntakeGathering> = {}): IntakeGathering => ({
  id: CARD, channel_id: CHANNEL, scope: 'group', thread_id: '66666666-6666-4666-8666-666666666666', lane_key: 'thread:6:lan',
  contact_id: null, state: 'review', review_reason: 'no_command',
  rules: { action: 'review', rule: 'group_without_command', basis: 'default', target_conversation_id: null },
  conversation_id: null, decided_by: null, decided_at: null, decided_by_agent_id: null, ignore_reason: null, fallback: false,
  opened_at: '2026-10-26T02:00:00Z', closed_at: '2026-10-26T02:01:00Z', decide_by: null, requeued_at: null, requeued_by_agent_id: null,
  item_count: 2, sender: { kind: 'zalo_uid', value: 'u1', name: 'Lan Nguyen' }, place: { kind: 'group', name: 'Customer A support' },
  preview: 'The scale stopped printing labels', suggestion: null, ...overrides,
});

export const chatItem = (overrides: Partial<ChatItem> = {}): ChatItem => ({
  id: '77777777-7777-4777-8777-777777777777', thread_id: '66666666-6666-4666-8666-666666666666', direction: 'in', role: 'customer',
  sender: { kind: 'zalo_uid', value: 'u1', name: 'Lan Nguyen' }, contact_id: null, agent_id: null, is_other_customer: false,
  body: 'The scale stopped printing labels', external_id: 'm1', quote_external_id: null, message_id: null, disposition: 'pending_review',
  disposition_reason: null, occurred_at: '2026-10-26T02:00:00Z', files: [], linked: false, link_reason: null, ...overrides,
});

export const candidate = (overrides: Partial<IntakeCandidate> = {}): IntakeCandidate => ({
  conversation_id: CONVERSATION, subject: 'Scale prints blank labels', status: 'open', contact_id: '88888888-8888-4888-8888-888888888888',
  contact_name: 'Lan Nguyen', channel_id: CHANNEL, updated_at: '2026-10-25T09:00:00Z', resolved: false, ...overrides,
});

export const detail = (overrides: Partial<IntakeGatheringDetail> = {}): IntakeGatheringDetail => ({
  ...gathering(),
  items: [chatItem(), chatItem({ id: '99999999-9999-4999-8999-999999999999', body: 'Since this morning', external_id: 'm2', occurred_at: '2026-10-26T02:00:30Z' })],
  decisions: [], candidates: [candidate()], commands: ['bug', 'question'], ...overrides,
});
