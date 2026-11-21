import { expect, test } from 'vitest';
import type { KBArticleFilters } from './knowledge-search';
import { articleFiltersOf, articleQuery, collectionHref, filtersBeyondCollection, knowledgeSearchFrom, knowledgeSearchOf, nextDay, previousDay } from './knowledge-search';

const ann = '11111111-1111-4111-8111-111111111111';
const bea = '22222222-2222-4222-8222-222222222222';
const billing = '33333333-3333-4333-8333-333333333333';

test('the address keeps every filter it knows and drops values it does not', () => {
  expect(knowledgeSearchFrom({
    collection: billing, subcollections: 'true', q: 'refund', status: 'published', visibility: 'agents',
    author: [ann, 'not-an-id', bea], tag: ['Billing', 'a, b', ''], tag_match: 'all',
    created_after: '2026-10-01', created_before: '2026-10-08', updated_after: 'last week', updated_before: '2026-11-01',
    translation: 'missing', locale: 'de', has_draft: true, sort: 'manual', other: 'x',
  })).toEqual({
    collection: billing, subcollections: 'true', q: 'refund', status: 'published', visibility: 'agents',
    author: [ann, bea], tag: ['billing', 'a, b'], tag_match: 'all',
    created_after: '2026-10-01', created_before: '2026-10-08', updated_before: '2026-11-01',
    translation: 'missing', locale: 'de', has_draft: 'true',
  });
});

test('a single tag or author in the address is a list of one; a locale without a translation filter is dropped', () => {
  expect(knowledgeSearchFrom({ tag: 'faq', author: ann, locale: 'de', tag_match: 'any', status: 'archived' })).toEqual({ tag: ['faq'], author: [ann] });
  expect(knowledgeSearchFrom({ translation: 'outdated', subcollections: 'true' })).toEqual({ translation: 'outdated' });
});

test('the address turns into the filters a saved view stores, and a saved view back into the address', () => {
  const search = {
    collection: billing, subcollections: 'true' as const, q: 'refund', has_draft: 'true' as const, author: [ann], tag: ['faq', 'billing'],
    tag_match: 'all' as const, created_after: '2026-10-01', updated_before: '2026-11-01', translation: 'missing' as const, locale: 'de', visibility: 'help_chat' as const,
  };
  const filters: KBArticleFilters = {
    collection_id: billing, include_subcollections: true, q: 'refund', has_draft: true, author: [ann], tag: ['faq', 'billing'],
    tag_match: 'all', created_after: '2026-10-01', updated_before: '2026-11-01', translation: 'missing', locale: 'de', visibility: 'help_chat',
  };
  expect(articleFiltersOf(search)).toEqual(filters);
  expect(knowledgeSearchOf(filters)).toEqual(search);
  expect(articleFiltersOf({})).toEqual({});
  // A view an agent saved through the API with a time keeps only what the page can show.
  expect(knowledgeSearchOf({ status: 'draft', created_after: '2026-10-01T00:00:00Z', state: 'in_review', sort: 'recent' })).toEqual({ status: 'draft' });
});

test('the list query repeats tag, joins authors with commas and leaves out what is not set', () => {
  expect(articleQuery({ q: 'refund policy', tag: ['a, b', 'billing'], tag_match: 'all', author: [ann, bea], has_draft: true, include_subcollections: false, cursor: 'next' }))
    .toBe(`?q=refund+policy&tag=a%2C+b&tag=billing&tag_match=all&author=${ann}%2C${bea}&has_draft=true&cursor=next`);
  expect(articleQuery({})).toBe('');
  expect(articleQuery({ limit: 200, sort: 'manual', tag: [], author: [] })).toBe('?limit=200&sort=manual');
});

test('only filters other than the collection count as filtering the list', () => {
  expect(filtersBeyondCollection({ collection: billing })).toBe(false);
  expect(filtersBeyondCollection({})).toBe(false);
  expect(filtersBeyondCollection({ collection: billing, tag: ['faq'] })).toBe(true);
  expect(filtersBeyondCollection({ collection: billing, subcollections: 'true' })).toBe(true);
  expect(filtersBeyondCollection({ updated_before: '2026-11-01' })).toBe(true);
});

test('a "to" date the person picks is the next day the server stops before', () => {
  expect(nextDay('2026-10-07')).toBe('2026-10-08');
  expect(nextDay('2026-02-28')).toBe('2026-03-01');
  expect(nextDay('2026-12-31')).toBe('2027-01-01');
  expect(previousDay('2026-03-01')).toBe('2026-02-28');
  expect(previousDay('2027-01-01')).toBe('2026-12-31');
});

test('a collection in the tree keeps the other filters, written the way the router reads them back', () => {
  const search = { collection: billing, subcollections: 'true' as const, q: 'refund', tag: ['faq', '2026'], created_after: '2026-10-01', author: [ann] };
  expect(collectionHref(search, bea)).toBe(`/knowledge?q=refund&tag=%5B%22faq%22%2C%222026%22%5D&created_after=2026-10-01&author=%5B%22${ann}%22%5D&collection=${bea}`);
  expect(collectionHref({ q: '2026' })).toBe('/knowledge?q=%222026%22');
  expect(collectionHref({})).toBe('/knowledge');
});
