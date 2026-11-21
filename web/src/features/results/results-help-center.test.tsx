import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import type { HelpCenterWindow } from '../../api/results';
import { json, serve } from '../kb/kb.test.fixture';
import { ResultsHelpCenter } from './results-help-center';

afterEach(() => { vi.unstubAllGlobals(); });

const jam = '33333333-3333-4333-8333-333333333333';
const wifi = '44444444-4444-4444-8444-444444444444';
const conversation = '55555555-5555-4555-8555-555555555555';
const dayViews: Record<string, number> = { '2026-11-14': 25, '2026-11-15': 15, '2026-11-16': 100, '2026-11-17': 80, '2026-11-18': 60, '2026-11-19': 55, '2026-11-20': 5 };

// Last 7 days in Berlin: Saturday 14 to Friday 20 November. The weekend had views and no request.
const lastWeek: HelpCenterWindow = {
  from: '2026-11-13T23:00:00Z', to: '2026-11-20T23:00:00Z', views: 340, requests: 30, score: 11.3,
  weeks: [
    { from: '2026-11-13T23:00:00Z', to: '2026-11-15T23:00:00Z', first_day: '2026-11-14', last_day: '2026-11-15', views: 40, requests: 0, score: null },
    { from: '2026-11-15T23:00:00Z', to: '2026-11-20T23:00:00Z', first_day: '2026-11-16', last_day: '2026-11-20', views: 300, requests: 30, score: 10 },
  ],
  days: Object.entries(dayViews).map(([day, views]) => ({ day, views, requests: day === '2026-11-20' ? 30 : 0 })),
  articles: [
    { id: jam, number: 3, title: 'Clear a paper jam', views: 120, helpful: 9, votes: 12, helpful_percent: 75, uses: 2 },
    { id: wifi, number: 4, title: 'Join the wifi', views: 0, helpful: 0, votes: 0, helpful_percent: null, uses: 1 },
  ],
  articles_total: 2,
};

function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <ResultsHelpCenter help={lastWeek} links={{
      requests: (range, children) => <a href={`/?status=all&source=native&created_after=${range.from}&created_before=${range.to}`}>{children}</a>,
      article: (id, children) => <a href={`/knowledge/articles/${id}`}>{children}</a>,
      conversation: (id, children) => <a href={`/conversations/${id}`}>{children}</a>,
    }} /></QueryClientProvider>);
  return screen.getByRole('region', { name: 'Help center' });
}

test('the help center shows its views next to the requests opened, and the score per week never divides by zero', () => {
  const box = open();
  expect(within(box).getByText('Nov 14 – Nov 20')).toBeVisible();
  expect(within(box).getByRole('link', { name: '30' })).toHaveAttribute('href', '/?status=all&source=native&created_after=2026-11-13T23:00:00Z&created_before=2026-11-20T23:00:00Z');
  expect(within(box).getByText('11.3')).toBeVisible();
  expect(within(box).getByText('340 views ÷ 30 requests')).toBeVisible();

  const weeks = within(within(box).getByRole('table', { name: 'Self-service score per week' })).getAllByRole('row');
  expect(weeks[1]).toHaveTextContent(/^Nov 14 – Nov 15400No requests$/);
  expect(weeks[2]).toHaveTextContent(/^Nov 16 – Nov 203003010\.0$/);
});

test('the views open their days', () => {
  const box = open();
  expect(within(box).queryByRole('table', { name: 'Article views per day' })).toBeNull();
  const views = within(box).getByRole('button', { name: '340' });
  expect(views).toHaveAttribute('aria-expanded', 'false');
  fireEvent.click(views);
  const days = within(within(box).getByRole('table', { name: 'Article views per day' })).getAllByRole('row');
  expect(days).toHaveLength(8);
  expect(days[1]).toHaveTextContent(/^Sat, Nov 1425$/);
  expect(days[7]).toHaveTextContent(/^Fri, Nov 205$/);
});

test('each article shows its views, helpful votes and the replies that used it, which open their conversations', async () => {
  const calls = serve({ [`GET /api/v1/kb/articles/${jam}/stats`]: () => json(200, {
    article_id: jam, timezone: 'Europe/Berlin', from: lastWeek.from, to: lastWeek.to, first_day: '2026-11-14', last_day: '2026-11-20', views: 120, uses: 2,
    days: [], replies_total: 2, replies: [
      { conversation_id: conversation, display_ref: '#12', subject: 'Printer jam', message_id: '66666666-6666-4666-8666-666666666666', kinds: ['link'], at: '2026-11-19T10:00:00Z' },
      { conversation_id: conversation, display_ref: '#12', subject: 'Printer jam', message_id: '77777777-7777-4777-8777-777777777777', kinds: ['ai_source', 'link'], at: '2026-11-18T10:00:00Z' },
    ],
  }) });
  const box = open();
  const table = within(box).getByRole('table', { name: 'Articles' });
  const rows = within(table).getAllByRole('row');
  expect(rows[1]).toHaveTextContent(/^Clear a paper jam12075% · 9 of 122$/);
  expect(within(rows[1]).getByRole('link', { name: 'Clear a paper jam' })).toHaveAttribute('href', `/knowledge/articles/${jam}`);
  expect(rows[2]).toHaveTextContent(/^Join the wifi0No votes1$/);

  fireEvent.click(within(rows[1]).getByRole('button', { name: '2' }));
  const used = await within(box).findByRole('list', { name: 'Replies that used Clear a paper jam' });
  const replies = within(used).getAllByRole('link', { name: '#12 Printer jam' });
  expect(replies).toHaveLength(2);
  expect(replies[0]).toHaveAttribute('href', `/conversations/${conversation}`);
  const stats = calls.find((call) => call.path === `/api/v1/kb/articles/${jam}/stats`);
  expect(stats?.search.get('from')).toBe('2026-11-14');
  expect(stats?.search.get('to')).toBe('2026-11-21');
});

test('a period without views or articles says so', () => {
  render(<QueryClientProvider client={new QueryClient()}><ResultsHelpCenter help={{ ...lastWeek, views: 0, requests: 0, score: null, weeks: [], articles: [], articles_total: 0,
    days: lastWeek.days.map((day) => ({ ...day, views: 0, requests: 0 })) }} links={{ requests: (_range, children) => children, article: (_id, children) => children, conversation: (_id, children) => children }} />
  </QueryClientProvider>);
  const box = screen.getByRole('region', { name: 'Help center' });
  expect(within(box).getByText('No requests')).toBeVisible();
  expect(within(box).getByText('No article was read or linked in a reply in this period.')).toBeVisible();
});
