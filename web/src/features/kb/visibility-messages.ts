import type { KBVisibility } from './kb-api';

// Who can see an article or a collection: the words the editor, the lists and the reply picker use.
export const visibilityMessages = {
  heading: 'Who can see this',
  names: { everyone: 'Everyone', help_chat: 'Help chat only', agents: 'Agents only' } satisfies Record<KBVisibility, string>,
  lines: {
    everyone: 'The help center, help chat and your team.',
    help_chat: 'Help chat quotes it without a link. Never on the help center.',
    agents: 'Only your team: reply suggestions and AI drafts, never linked.',
  } satisfies Record<KBVisibility, string>,
  done: {
    everyone: 'Everyone can see this article now.',
    help_chat: 'Only help chat can use this article now.',
    agents: 'Only your team can see this article now.',
  } satisfies Record<KBVisibility, string>,
  because: (visibility: KBVisibility, collection: string) => `${visibilityMessages.names[visibility]} because of ${collection}.`,
  failed: 'Who can see this could not be changed. Try again.',
  collectionHelp: 'Applies to every article in it and its collections. An article is as restricted as the most restrictive of itself and its collections.',
  filter: 'Who can see',
  anyone: 'Anyone',
  insertText: 'Inserts its text: this article has no public page to link.',
} as const;
