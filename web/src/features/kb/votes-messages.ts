// What the article editor's votes box says.
export const votesMessages = {
  heading: 'Readers\' votes',
  loading: 'Loading the votes…',
  loadFailed: 'Could not load the votes. Reload the page to try again.',
  none: 'No votes yet.',
  // The same words the public article page shows under its question.
  count: (helpful: number, total: number) => `${helpful} out of ${total} found this helpful`,
  imported: (helpful: number, total: number) => `Includes ${helpful} out of ${total} from Freshdesk.`,
  yes: 'Yes',
  no: 'No',
  anonymous: 'Anonymous reader',
  readers: 'Votes, newest change first',
  showAll: (count: number) => `Show all ${count}`,
  reset: 'Reset votes',
  resetHeading: (title: string) => `Reset the votes of "${title}"?`,
  resetBody: 'The count starts again from 0. The votes and what readers wrote are deleted; the earlier count stays in the history.',
  resetFailed: 'Could not reset the votes. Try again.',
  cancel: 'Cancel',
  history: 'Earlier counts',
  earlier: (day: string, helpful: number, total: number, by?: string) => `Before ${day}: ${helpful} out of ${total}${by ? `, reset by ${by}` : ''}`,
};
