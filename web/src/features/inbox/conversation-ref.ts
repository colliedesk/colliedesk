import type { Conversation } from '../../api/schemas';

// ownRef is the code a conversation shows: only one numbered in a series of its own (a channel's, such as
// CHD-2026-0142). The default series' "#12" stays off the list and the header, so an inbox of email and
// web forms reads as before; its code still finds it (?ref=, search).
export function ownRef(conversation: Pick<Conversation, 'number_series' | 'display_ref'>): string | undefined {
  return conversation.number_series && conversation.number_series !== 'default' ? conversation.display_ref : undefined;
}
