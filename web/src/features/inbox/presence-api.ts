import { z } from 'zod';
import type { RequestBody } from '../../api/client';
import { conversationViewerSchema } from '../../api/schemas';
import type { Generated } from '../../api/schemas';

// The presence calls, in the presence chunk with the code that makes them (presence-runtime.ts).

const answerSchema = z.object({ viewers: z.array(conversationViewerSchema), heartbeat_seconds: z.number().int().positive() }) satisfies z.ZodType<Generated<'ConversationPresence'>>;

// RetryLater is an announce repeated sooner than the server takes it (429 too_many_requests).
export class RetryLater extends Error {
  constructor(readonly seconds: number) {
    super('Too many requests');
  }
}

export const presenceApi = {
  // Says this tab has the conversation open (and whether a reply is being written) and answers who else has.
  announce: async (id: string, body: RequestBody<'putConversationPresence'>) => {
    const response = await fetch(`/api/v1/conversations/${id}/presence`, { method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    if (response.status === 429) throw new RetryLater(Number(response.headers.get('Retry-After')) || 1);
    if (!response.ok) throw new Error(`API ${response.status}`);
    return answerSchema.parse(await response.json());
  },
  // keepalive lets the leave finish while the tab closes.
  leave: async (id: string, clientId: string) => {
    await fetch(`/api/v1/conversations/${id}/presence/${clientId}`, { method: 'DELETE', credentials: 'same-origin', keepalive: true });
  },
};
