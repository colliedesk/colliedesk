import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleStatsBox } from './article-stats';
import { ids, json, serve } from './kb.test.fixture';

afterEach(() => { vi.unstubAllGlobals(); });

const path = `/api/v1/kb/articles/${ids.article}/stats`;
const conversation = '55555555-5555-4555-8555-555555555555';
const lastMonth = {
  article_id: ids.article, timezone: 'Europe/Berlin', from: '2026-10-21T22:00:00Z', to: '2026-11-20T23:00:00Z', first_day: '2026-10-22', last_day: '2026-11-20',
  views: 4, uses: 1, days: [], replies_total: 1,
  replies: [{ conversation_id: conversation, display_ref: '#12', subject: 'Printer jam', message_id: '66666666-6666-4666-8666-666666666666', kinds: ['link'], at: '2026-11-19T10:00:00Z' }],
};

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <ArticleStatsBox articleId={ids.article} headingId="stats" conversationLink={(id, children) => <a href={`/conversations/${id}`}>{children}</a>} />
  </QueryClientProvider>);
  return screen.getByRole('region', { name: 'Last 30 days' });
}

test('the editor shows the article\'s views and the replies that used it in the last 30 days', async () => {
  const calls = serve({ [`GET ${path}`]: () => json(200, lastMonth) });
  const box = open();
  expect(await within(box).findByText('4 views on the help center')).toBeInTheDocument();
  expect(within(box).getByText('Used in 1 reply')).toBeInTheDocument();
  const replies = within(box).getByRole('list', { name: 'Replies that used this article' });
  expect(within(replies).getByRole('link', { name: '#12 Printer jam' })).toHaveAttribute('href', `/conversations/${conversation}`);
  const call = calls.find((each) => each.path === path);
  expect(call?.search.has('from')).toBe(false);
});

test('an article nobody read or used says so', async () => {
  serve({ [`GET ${path}`]: () => json(200, { ...lastMonth, views: 0, uses: 0, replies_total: 0, replies: [] }) });
  const box = open();
  expect(await within(box).findByText('No views on the help center')).toBeInTheDocument();
  expect(within(box).getByText('Not used in a reply')).toBeInTheDocument();
  expect(within(box).queryByRole('list')).toBeNull();
});
