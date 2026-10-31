import type { Conversation } from '../../api/schemas';

// Chat channels can tell a customer their conversation's new status; email never does.
const chatTypes = new Set(['zalo', 'help_chat']);
const isChatChannel = (conversation: Conversation) => chatTypes.has(conversation.channel_ref?.type ?? '');

/** Whether setting this conversation's status may tell its customer: the server says so on every conversation
 * (a chat message moved into an email conversation included); an older server is read from its channel. */
export function mayTell(conversation: Conversation) {
  return conversation.may_tell_customer ?? (isChatChannel(conversation) || (conversation.status_notices?.length ?? 0) > 0);
}

/** Whether a status change of these rows may tell a customer (the server decides each one). */
export function mayTellCustomers(rows: Conversation[]) {
  return rows.some(mayTell);
}

/** Whether its Status in the details is a menu, never a select: any conversation of a chat, which may tell
 * when it comes back to the status it has now. */
export function statusAsMenu(conversation: Conversation) {
  return isChatChannel(conversation) || mayTell(conversation);
}

/** The statuses that would tell now; undefined while only the conversation itself can say which. */
export function noticesOf(conversation: Conversation) {
  return conversation.status_notices ?? (conversation.may_tell_customer === false ? [] : undefined);
}
