// The words of Knowledge → Searches: what readers searched for on the help center.
const count = (n: number, one: string, many: string) => `${n.toLocaleString('en')} ${n === 1 ? one : many}`;

export const searchesMessages = {
  searchesTab: 'Searches',
  heading: 'Help center searches',
  total: (n: number) => `${count(n, 'search', 'searches')} on the help center in the last 7 days.`,
  tip: 'Words are grouped when only case or accents differ. An email address or a long number a reader typed is removed before it is kept. Bots and signed-in teammates are not counted.',
  empty: 'No one searched the help center in the last 7 days. A search shows here as soon as a reader makes it.',
  noResults: 'Searches that found no article',
  noResultsLead: 'Write an article that answers them.',
  noResultsEmpty: 'Every search found at least one article.',
  nothingOpened: 'Searches whose results nobody opened',
  nothingOpenedLead: 'Make the titles of the articles they found match the words.',
  nothingOpenedEmpty: 'Readers opened an article from every search that found one.',
  top: 'Top searches',
  topLead: 'The words readers searched for most.',
  stats: (searches: number, results: number | null, opened: number | null) => [
    count(searches, 'search', 'searches'),
    ...(results === null ? [] : [count(results, 'result', 'results')]),
    ...(opened === null ? [] : [`${opened.toLocaleString('en')} opened`]),
  ].join(' · '),
  write: (query: string) => `Write an article for ${query}`,
  writeShort: 'Write an article',
  loadFailed: 'The searches could not be loaded.',
};
