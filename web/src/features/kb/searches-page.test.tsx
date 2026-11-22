import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { SearchesRoute } from './searches-page';
import { json, me, serve } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const term = (query: string, searches: number, results: number, opened = 0) => ({ query, searches, results, opened, last_searched_at: '2026-11-21T09:00:00Z' });
const exportInvoices = term('export invoices', 4, 0);
const resetPassword = term('reset password', 3, 2);
const report = {
  from: '2026-11-14T10:00:00Z', to: '2026-11-21T10:00:00Z', searches: 9,
  top: [exportInvoices, resetPassword, term('billing', 2, 5, 2)], no_results: [exportInvoices], nothing_opened: [resetPassword],
};

function open() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><SearchesRoute /></QueryClientProvider>);
}

test('the searches page lists the words that found nothing first, each with the next step', async () => {
  serve({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/searches/report': () => json(200, report) });
  open();
  const nothing = await screen.findByRole('list', { name: 'Searches that found no article' });
  expect(within(nothing).getByText('export invoices')).toBeInTheDocument();
  expect(within(nothing).getByText('4 searches')).toBeInTheDocument();
  expect(within(nothing).getByRole('link', { name: 'Write an article for export invoices' })).toHaveAttribute('href', '/knowledge/new');
  const unopened = screen.getByRole('list', { name: 'Searches whose results nobody opened' });
  expect(within(unopened).getByText('reset password')).toBeInTheDocument();
  expect(within(unopened).getByText('3 searches · 2 results')).toBeInTheDocument();
  const top = screen.getByRole('list', { name: 'Top searches' });
  expect(within(top).getByText('2 searches · 5 results · 2 opened')).toBeInTheDocument();
  expect(screen.getByText('9 searches on the help center in the last 7 days.')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Searches' })).toHaveAttribute('aria-current', 'page');
});

test('a week without searches says where they come from', async () => {
  serve({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/kb/searches/report': () => json(200, { ...report, searches: 0, top: [], no_results: [], nothing_opened: [] }) });
  open();
  expect(await screen.findByText('No one searched the help center in the last 7 days. A search shows here as soon as a reader makes it.')).toBeInTheDocument();
  expect(screen.queryByRole('list', { name: 'Top searches' })).not.toBeInTheDocument();
});
