import { z } from 'zod';
import { aiSourceSchema } from './ai-sources';
import { request } from '../../api/client';
import type { Generated, JsonResponse } from '../../api/schemas';

// AI triage suggestions load with their part of the details panel, so their schemas and calls are not in the first bundle.

const nullableDate = z.string().datetime().nullable();
const priority = z.enum(['low', 'normal', 'high', 'urgent']);
const triageSet = z.object({ tag_ids: z.array(z.string().uuid()), priority: z.string().nullable(), assignee_agent_id: z.string().uuid().nullable() });
export const aiTriageSchema = z.object({
  id: z.string().uuid(), conversation_id: z.string().uuid(), status: z.enum(['queued', 'running', 'ready', 'failed', 'cancelled']),
  error_code: z.string().nullable(), automatic: z.boolean(), tag_ids: z.array(z.string().uuid()), priority: priority.nullable(),
  assignee_agent_id: z.string().uuid().nullable(), reason: z.string().nullable(), sources: z.array(aiSourceSchema),
  stale: z.object({ priority: priority.nullable(), assignee_agent_id: z.string().uuid().nullable() }), model: z.string().nullable(),
  outcome: z.enum(['accepted', 'partly_accepted', 'dismissed', 'superseded', 'expired', 'auto_applied', 'undone']).nullable(),
  applied: triageSet,
  created_at: z.string().datetime(), ready_at: nullableDate, decided_at: nullableDate, decided_by_agent_id: z.string().uuid().nullable(),
  auto_applied_at: nullableDate, auto_applied: triageSet,
  skipped: z.record(z.string(), z.object({ reason: z.enum(['set_by_person', 'agent_conversation', 'already_assigned']), value: z.string().nullable() })),
  undone_parts: triageSet, kept: z.object({ priority: priority.nullable(), assignee_agent_id: z.string().uuid().nullable() }),
  undone_at: nullableDate, undone_by_agent_id: z.string().uuid().nullable(),
}) satisfies z.ZodType<Generated<'AITriage'>>;
const latestSchema = z.object({ suggestion: aiTriageSchema.nullable(), auto_applied: aiTriageSchema.nullable() }) satisfies z.ZodType<JsonResponse<'getAITriage', 200>>;

export type AITriage = z.infer<typeof aiTriageSchema>;
export type TriagePart = 'tags' | 'priority' | 'assignee';
// One part of a suggestion: its tags, its priority or its assignee.
export type TriagePick = { tag_ids?: string[]; priority?: string; assignee_agent_id?: string };

const base = (conversation: string) => `/api/v1/conversations/${conversation}/ai-triage`;

export const aiTriageApi = {
  latest: (conversation: string) => request(base(conversation), latestSchema),
  create: (conversation: string) => request(base(conversation), aiTriageSchema, { method: 'POST' }),
  // Without pick, the rest of the suggestion.
  accept: (conversation: string, id: string, pick?: TriagePick) =>
    request(`${base(conversation)}/${id}/accept`, aiTriageSchema, { method: 'POST', ...(pick ? { body: JSON.stringify(pick) } : {}) }),
  dismiss: (conversation: string, id: string) => request(`${base(conversation)}/${id}/dismiss`, aiTriageSchema, { method: 'POST' }),
  // Without parts, all of what AI applied.
  undo: (conversation: string, id: string, parts?: TriagePart[]) =>
    request(`${base(conversation)}/${id}/undo`, aiTriageSchema, { method: 'POST', ...(parts ? { body: JSON.stringify({ parts }) } : {}) }),
};
