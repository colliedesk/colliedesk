import { track } from '../../lib/product-analytics';
import type { InboxFilterKind } from '../../lib/product-analytics';
import type { InboxQuery, InboxQueryKey } from '../../lib/inbox-query';

// The kind of filter a key belongs to: tag_match counts as tag, both ends of a range as one.
const kindOf = (key: InboxQueryKey) => ({ tag_match: 'tag', has_pending_draft: 'draft', delivery_failed: 'delivery' } as Record<string, string>)[key]
  ?? key.replace(/_(after|before)$/, '');

// trackFilterChanges reports each kind of filter that changed, never a value. Only the Filter, Sort and
// Views menus and the chips change these, so this loads with them; the route reports status and search.
export function trackFilterChanges(previous: InboxQuery, next: InboxQuery) {
  const changed = new Set<string>();
  for (const key of new Set([...Object.keys(previous), ...Object.keys(next)]) as Set<InboxQueryKey>) {
    if (key !== 'search' && key !== 'status' && (previous[key] ?? '') !== (next[key] ?? '')) changed.add(kindOf(key));
  }
  for (const filter of changed) track('inbox_filtered', { filter: filter as InboxFilterKind });
}
