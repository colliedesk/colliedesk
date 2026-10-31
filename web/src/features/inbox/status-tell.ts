import type { Conversation } from '../../api/schemas';

// Chat channels can tell a customer their conversation's new status; email never does.
const chatTypes = new Set(['zalo', 'help_chat']);

/** Whether setting this conversation's status may tell its customer: the server says so on every conversation
 * (a chat message moved into an email conversation included); an older server is read from its channel. */
export function mayTell(conversation: Conversation) {
  return conversation.may_tell_customer ?? (chatTypes.has(conversation.channel_ref?.type ?? '') || (conversation.status_notices?.length ?? 0) > 0);
}

/** Whether a status change of these rows may tell a customer (the server decides each one). */
export function mayTellCustomers(rows: Conversation[]) {
  return rows.some(mayTell);
}

/** The statuses that would tell now; undefined while only the conversation itself can say which. */
export function noticesOf(conversation: Conversation) {
  return conversation.status_notices ?? (conversation.may_tell_customer === false ? [] : undefined);
}
