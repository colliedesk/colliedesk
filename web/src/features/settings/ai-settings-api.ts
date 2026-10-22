import { z } from 'zod';
import { json, request } from '../../api/client';
import type { components } from '../../api/generated';

type Generated<Name extends keyof components['schemas']> = components['schemas'][Name];

const aiSettingsSchema = z.object({
  available: z.boolean(), drafts_enabled: z.boolean(), auto_answer_enabled: z.boolean(), auto_answer_delay_seconds: z.number().int(),
  auto_answer_min_reviewed: z.number().int(), auto_answer_min_accuracy: z.number().int(), updated_at: z.string().datetime().nullable(),
}) satisfies z.ZodType<Generated<'AISettings'>>;
const aiWorkspaceSchema = z.object({
  settings: aiSettingsSchema,
  topics: z.array(z.object({ article_id: z.string().uuid(), article_number: z.number().int(), title: z.string(), reviewed: z.number().int(), accepted: z.number().int(), eligible: z.boolean() })),
  usage: z.object({ drafts: z.number().int(), calls: z.number().int(), input_tokens: z.number().int(), output_tokens: z.number().int(),
    sent_unchanged: z.number().int(), sent_edited: z.number().int(), discarded: z.number().int(), auto_sent: z.number().int() }),
}) satisfies z.ZodType<Generated<'AIWorkspace'>>;
export type AIWorkspace = z.infer<typeof aiWorkspaceSchema>;
export type AISettingsChange = Generated<'AISettingsUpdate'>;

export const aiSettingsApi = {
  get: () => request('/api/v1/workspace/ai', aiWorkspaceSchema),
  update: (change: AISettingsChange) => request('/api/v1/workspace/ai', aiSettingsSchema, { method: 'PATCH', body: json<'updateAISettings'>(change) }),
};
