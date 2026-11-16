// Words of Settings → Tags.
const conversations = (count: number) => (count === 1 ? '1 conversation' : `${count} conversations`);

export const tagMessages = {
  tags: 'Tags',
  lead: 'Renaming or deleting a tag changes every conversation.',
  noTags: 'No tags yet. Add one from the details of a conversation.',
  loadFailed: 'The tags could not be loaded.',
  tryAgain: 'Try again',
  count: (count: number) => (count === 0 ? 'No conversations' : conversations(count)),
  rename: 'Rename',
  renameTag: (name: string) => `Rename ${name}`,
  newNameFor: (name: string) => `New name for ${name}`,
  tagLabel: (name: string) => `Tag ${name}`,
  nameRequired: 'A tag needs a name.',
  renamed: (from: string, to: string) => `Renamed “${from}” to “${to}”.`,
  deleteTag: (name: string) => `Delete ${name}`,
  confirmDelete: (name: string, count: number) =>
    `Delete “${name}”? ${count === 0 ? 'No conversation has it.' : `It comes off ${conversations(count)}.`} This cannot be undone.`,
  deleteButton: 'Delete tag',
  deleted: (name: string) => `Deleted “${name}”.`,
  actionFailed: 'That did not work. Check your connection and try again.',
};
