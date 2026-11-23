// What the knowledge base says about an article a sent reply contradicts: the list mark and the editor's box.
export const conflictsMessages = {
  open: 'May be out of date',
  cleared: 'Replies that disagreed',
  loading: 'Loading the replies…',
  loadFailed: 'Could not load the replies. Reload the page to try again.',
  count: (n: number) => (n === 1 ? '1 sent reply says something the article does not.' : `${n} sent replies say something the article does not.`),
  next: 'Fix the article if the reply is right, then clear the flag.',
  clearedBy: (name: string | undefined, day: string) => (name ? `Cleared by ${name} on ${day}.` : `Cleared on ${day}.`),
  sentBy: (name: string | undefined, day: string) => [name, day].filter(Boolean).join(' · '),
  replies: 'Replies, open first',
  clear: 'Clear the flag',
  clearFailed: 'Could not clear the flag. Try again.',
};
