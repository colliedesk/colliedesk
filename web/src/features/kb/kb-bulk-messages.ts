import type { KBBulkResult, KBBulkUndoResult } from './kb-bulk-api';

const articles = (count: number) => `${count.toLocaleString('en')} ${count === 1 ? 'article' : 'articles'}`;
// The refusals a toast names one by one; the rest are counted.
const named = 3;

type Item = { title?: string; error?: { message: string } };
function reasons(items: Item[]): string {
  const lines = items.slice(0, named).map((item) => `${item.title ?? 'An article'}: ${item.error?.message ?? ''}`.trim());
  if (items.length > named) lines.push(`And ${(items.length - named).toLocaleString('en')} more.`);
  return lines.join(' ');
}

export const kbBulkMessages = {
  select: (title: string) => `Select ${title}`,
  bar: 'Selected articles',
  selected: (count: number) => `${count.toLocaleString('en')} selected`,
  allMatching: 'All matching articles selected',
  selectAllMatching: 'Select all matching articles',
  selectAllMatchingTip: 'Every article the filters match, on every page, up to 500.',
  move: 'Move to collection',
  visibility: 'Visibility',
  author: 'Credit to',
  addTag: 'Add tag',
  removeTag: 'Remove tag',
  newTag: 'New tag',
  add: 'Add',
  noTags: 'No tags yet.',
  sendForReview: 'Send for review',
  noReviewers: 'No teammate can publish yet.',
  approve: 'Approve',
  publish: 'Publish',
  unpublish: 'Unpublish',
  trash: 'Move to trash',
  clear: 'Clear selection',
  undo: 'Undo',
  failed: 'The change was not made.',
  undoFailed: 'Undo did not run.',
  // done: what one bulk action did, then why each refused article was refused.
  done: ({ counts, results }: KBBulkResult) => {
    const parts = [counts.done > 0 && `${articles(counts.done)} changed`,
      counts.unchanged > 0 && `${counts.unchanged.toLocaleString('en')} already ${counts.unchanged === 1 ? 'was' : 'were'}`,
      counts.refused > 0 && `${counts.refused.toLocaleString('en')} refused`].filter(Boolean);
    const text = `${parts.join(', ')}.`;
    const refused = results.filter((item) => item.outcome === 'refused');
    return refused.length > 0 ? `${text} ${reasons(refused)}` : text;
  },
  // undone: what Undo put back, and the articles it left as they are because they changed since.
  undone: ({ counts, results }: KBBulkUndoResult) => {
    const text = counts.undone > 0 ? `${articles(counts.undone)} put back.` : 'Nothing put back.';
    const left = results.filter((item) => item.outcome === 'not_undone');
    return left.length > 0 ? `${text} ${articles(left.length)} left as ${left.length === 1 ? 'it is' : 'they are'}. ${reasons(left)}` : text;
  },
};
