import { expect, test } from 'vitest';
import type { z } from 'zod';
import type { importJobSchema } from '../../api/imports';
import { freshdeskAgentCount } from './freshdesk-agents';

type Job = z.infer<typeof importJobSchema>;
const agent = (id: number, active: boolean) => ({ id, name: `Agent ${id}`, email: `a${id}@example.com`, active });
const job = (patch: Partial<Job>): Job => ({
  id: '11111111-1111-4111-8111-111111111111', kind: 'import', source: 'freshdesk', domain: 'acme.freshdesk.com', account_source: null, undo_of: null, state: 'previewed', phase: 'preview',
  options: {}, preview: null, progress: null, status: null, error_code: null, error_message: null, imported_conversations: null, has_key: false, has_interim_report: false,
  has_report: false, cancel_requested: false, next_run_at: null, created_by: null, created_at: '2026-10-20T09:00:00Z', started_at: null, finished_at: null, updated_at: '2026-10-20T09:00:00Z', ...patch,
});
const preview = (agents: ReturnType<typeof agent>[]): Job['preview'] => ({ domain: 'acme.freshdesk.com', account_id: 1, source: 'freshdesk:account:1', agents, companies: 0, contacts: 0, tickets_outside_archive: 0,
  highest_listed_ticket_id: 0, archive: 'readable', canned_responses: 'readable', knowledge_base: 'readable', rate_limit_per_minute: 100, api_calls: 1, checked_at: '2026-10-20T09:00:00Z' });

test('the count is the active agents of the account the newest import read', () => {
  const older = job({ id: '22222222-2222-4222-8222-222222222222', created_at: '2026-10-10T09:00:00Z', preview: preview([agent(1, true)]) });
  const newer = job({ preview: preview([agent(1, true), agent(2, true), agent(3, false), agent(4, true)]) });
  expect(freshdeskAgentCount([older, newer])).toBe(3);
});

test('no import, an import that read nothing, a discarded one or an undo gives no count', () => {
  expect(freshdeskAgentCount([])).toBeUndefined();
  expect(freshdeskAgentCount([job({ preview: null })])).toBeUndefined();
  expect(freshdeskAgentCount([job({ state: 'discarded', preview: preview([agent(1, true)]) })])).toBeUndefined();
  expect(freshdeskAgentCount([job({ kind: 'undo', preview: preview([agent(1, true)]) })])).toBeUndefined();
});

test('an account whose agents are all inactive gives no count, so the workspace seats decide', () => {
  expect(freshdeskAgentCount([job({ preview: preview([agent(1, false)]) })])).toBeUndefined();
});
