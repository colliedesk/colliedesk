import type { Generated } from '../../api/schemas';

// The knowledge base article list keeps its filters in the address. This module is the one place that
// reads them from the address, turns them into the filters a saved view stores and the query the list
// and its CSV export send, and back. It has no other imports, so the first bundle can read the address.

export type KBArticleFilters = Generated<'KBArticleFilters'>;
const visibilities = ['everyone', 'help_chat', 'agents'] as const;
type Visibility = (typeof visibilities)[number];

export interface KnowledgeSearch {
  collection?: string;
  // subcollections 'true' also keeps the articles of the collection's collections.
  subcollections?: 'true';
  q?: string;
  status?: 'draft' | 'published';
  // has_draft keeps the published articles with unpublished changes.
  has_draft?: 'true';
  // visibility keeps the articles one audience sees (their effective visibility).
  visibility?: Visibility;
  author?: string[];
  tag?: string[];
  // tag_match 'all' keeps the articles with every tag; without it, one of them is enough.
  tag_match?: 'all';
  // Dates (YYYY-MM-DD) in the workspace's timezone: from _after, up to but not including _before.
  created_after?: string;
  created_before?: string;
  updated_after?: string;
  updated_before?: string;
  // translation keeps the articles missing a translation, or with an outdated one; locale names the language.
  translation?: 'missing' | 'outdated';
  locale?: string;
}

export const dateKeys = ['created_after', 'created_before', 'updated_after', 'updated_before'] as const;
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const date = /^\d{4}-\d{2}-\d{2}$/;
const maxTagLength = 40;

const text = (value: unknown): string | undefined => (typeof value === 'string' && value !== '' ? value : undefined);
const list = (value: unknown): string[] => (Array.isArray(value) ? value : typeof value === 'string' ? [value] : []).filter((item): item is string => typeof item === 'string');
// A tag as the server stores it: trimmed, in lower case, with runs of white space as one space.
const cleanTag = (tag: string) => tag.trim().split(/\s+/).join(' ').toLowerCase();
const defined = <T extends object>(value: T): T => Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined)) as T;
const some = (items: string[]) => (items.length > 0 ? items : undefined);

export function knowledgeSearchFrom(search: Record<string, unknown>): KnowledgeSearch {
  const collection = text(search.collection);
  const translation = search.translation === 'missing' || search.translation === 'outdated' ? search.translation : undefined;
  const tags = [...new Set(list(search.tag).map(cleanTag).filter((tag) => tag !== '' && tag.length <= maxTagLength))];
  return defined({
    collection, subcollections: collection && (search.subcollections === 'true' || search.subcollections === true) ? 'true' as const : undefined,
    q: text(search.q), status: search.status === 'draft' || search.status === 'published' ? search.status : undefined,
    has_draft: search.has_draft === 'true' || search.has_draft === true ? 'true' as const : undefined,
    visibility: visibilities.find((item) => item === search.visibility),
    author: some(list(search.author).filter((id) => uuid.test(id))), tag: some(tags), tag_match: search.tag_match === 'all' ? 'all' as const : undefined,
    ...Object.fromEntries(dateKeys.map((key) => [key, typeof search[key] === 'string' && date.test(search[key]) ? search[key] : undefined])),
    translation, locale: translation ? text(search.locale) : undefined,
  });
}

// articleFiltersOf is what the list asks for and a saved view stores.
export function articleFiltersOf(search: KnowledgeSearch): KBArticleFilters {
  return defined({
    collection_id: search.collection, include_subcollections: search.subcollections ? true : undefined, q: search.q, status: search.status,
    has_draft: search.has_draft ? true : undefined, visibility: search.visibility, author: search.author, tag: search.tag, tag_match: search.tag_match,
    created_after: search.created_after, created_before: search.created_before, updated_after: search.updated_after, updated_before: search.updated_before,
    translation: search.translation, locale: search.locale,
  });
}

// knowledgeSearchOf opens a saved view: what the page cannot show (a time instead of a date, a review state, an order) is left out.
export function knowledgeSearchOf(filters: KBArticleFilters): KnowledgeSearch {
  return knowledgeSearchFrom({
    ...filters, collection: filters.collection_id, subcollections: filters.include_subcollections ? 'true' : undefined, has_draft: filters.has_draft ? 'true' : undefined,
  });
}

// filtersBeyondCollection: the list is filtered by more than the collection open in the tree, so it is not the collection's own order.
export function filtersBeyondCollection(search: KnowledgeSearch): boolean {
  return Object.keys(search).some((key) => key !== 'collection');
}

// articleQuery is the query string of GET /api/v1/kb/articles and its CSV export: tag repeats (a tag may hold a comma), authors join with commas.
export function articleQuery(values: KBArticleFilters & { limit?: number; cursor?: string }): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (key === 'tag' && Array.isArray(value)) for (const tag of value) params.append(key, tag);
    else if (Array.isArray(value)) { if (value.length > 0) params.set(key, value.join(',')); }
    else if (value !== undefined && value !== false && value !== '') params.set(key, String(value));
  }
  const query = params.toString();
  return query ? `?${query}` : '';
}

// The person picks the last day to include; the server stops before the next one. A date has no timezone of its own, so UTC days are exact.
const shift = (day: string, by: number) => {
  const at = new Date(`${day}T00:00:00Z`);
  at.setUTCDate(at.getUTCDate() + by);
  return at.toISOString().slice(0, 10);
};
export const nextDay = (day: string) => shift(day, 1);
export const previousDay = (day: string) => shift(day, -1);

// collectionHref is the list's address with another collection open (none: all articles), keeping the other
// filters. It writes values as the router does (lists and number-like text as JSON), so the router reads back the same search.
const jsonStart = /^(?:\s|["[{\d-]|fa|nu|tr)/;
const addressValue = (value: string | string[]) => {
  if (Array.isArray(value)) return JSON.stringify(value);
  if (!jsonStart.test(value)) return value;
  try { JSON.parse(value); return JSON.stringify(value); } catch { return value; }
};
export function collectionHref(search: KnowledgeSearch, collection?: string): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(search) as [keyof KnowledgeSearch, string | string[] | undefined][]) {
    if (key !== 'collection' && key !== 'subcollections' && value !== undefined) params.set(key, addressValue(value));
  }
  if (collection) params.set('collection', collection);
  return `/knowledge${params.size > 0 ? `?${params}` : ''}`;
}
