import type { Conversation } from '../../api/schemas';

// Chat channels can tell a customer their conversation's new status; email never does.
const chatTypes = new Set(['zalo', 'help_chat']);

/** Whether setting this conversation's status may tell its customer in a chat: its channel is a chat, or
 * the server said a status would tell (a chat message moved into an email conversation). */
export function mayTell(conversation: Conversation) {
  return chatTypes.has(conversation.channel_ref?.type ?? '') || (conversation.status_notices?.length ?? 0) > 0;
}

/** Whether a status change of these rows may tell a customer (the server decides each one). */
export function mayTellCustomers(rows: Conversation[]) {
  return rows.some(mayTell);
}
