import { z } from 'zod';
import { json, request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated } from '../../api/schemas';

// Moving customer messages between conversations (docs/user/inbox.md "Move messages to another
// conversation") and why each message is in its conversation. Loaded with the message-moves part, never in
// the first bundle.

const uuid = z.string().uuid();

export const messageMoveSchema = z.object({
  id: uuid, message_ids: z.array(uuid), from_conversation_id: uuid, to_conversation_id: uuid.nullable(), created_conversation: z.boolean(),
  closed_source: z.boolean(), gathering_id: uuid.nullable(), decision_id: uuid.nullable(), moved_by_agent_id: uuid.nullable(), via_token: z.boolean(),
  moved_at: z.string().datetime(), undone_at: z.string().datetime().nullable(), undone_by_agent_id: uuid.nullable(), undoable: z.boolean(),
}) satisfies z.ZodType<Generated<'MessageMove'>>;

const messageMoveResultSchema = z.object({
  move: messageMoveSchema, warnings: z.array(z.enum(['other_contact', 'other_channel'])),
}) satisfies z.ZodType<Generated<'MessageMoveResult'>>;

const messageMovePageSchema = z.object({ items: z.array(messageMoveSchema) }) satisfies z.ZodType<Generated<'MessageMovePage'>>;

// Only the messages' part of the answer: what decided each message. The cards it also carries are the
// Review page's.
const conversationIntakeSchema = z.object({
  messages: z.array(z.object({
    message_id: uuid, decision_basis: z.enum(['match', 'default', 'person', 'model']).nullable(), decision_rule: z.string().nullable(), gathering_id: uuid.nullable(),
  })),
}) satisfies z.ZodType<Pick<Generated<'ConversationIntake'>, 'messages'>>;

export type MessageMove = z.infer<typeof messageMoveSchema>;
export type MessageMoveResult = z.infer<typeof messageMoveResultSchema>;
export type MessageIntake = z.infer<typeof conversationIntakeSchema>['messages'][number];
export type MoveInput = RequestBody<'moveConversationMessages'>;

export const messageMovesApi = {
  list: (conversationID: string) => request(`/api/v1/conversations/${conversationID}/message-moves`, messageMovePageSchema),
  // A move and its undo take the key of their press (Idempotency-Key, from api/use-create): a repeat answers the first.
  move: (conversationID: string, input: MoveInput, key: string) => request(`/api/v1/conversations/${conversationID}/messages/move`, messageMoveResultSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'moveConversationMessages'>(input) }),
  undo: (moveID: string, key: string) => request(`/api/v1/message-moves/${moveID}/undo`, messageMoveSchema, { method: 'POST', headers: { 'Idempotency-Key': key } }),
  intake: (conversationID: string) => request(`/api/v1/conversations/${conversationID}/intake`, conversationIntakeSchema),
};
