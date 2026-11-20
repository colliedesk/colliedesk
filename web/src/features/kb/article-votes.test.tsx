import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { PermissionsContext } from '../../lib/permissions-context';
import { ArticleVotesBox } from './article-votes';
import { ids, json, refusal, serve } from './kb.test.fixture';

afterEach(() => { vi.unstubAllGlobals(); });

const path = `/api/v1/kb/articles/${ids.article}/votes`;
const votes = {
  article_id: ids.article, helpful: 13, not_helpful: 4, imported: { helpful: 12, not_helpful: 3 },
  votes: [
    { helpful: false, missing: 'The screenshot shows the old menu, write to [email removed]', contact: { id: '99999999-9999-4999-8999-999999999999', name: 'Jane Doe' },
      created_at: '2026-11-18T09:00:00Z', updated_at: '2026-11-19T09:00:00Z' },
    { helpful: true, missing: null, contact: null, created_at: '2026-11-17T09:00:00Z', updated_at: '2026-11-17T09:00:00Z' },
  ],
  resets: [],
};
const afterReset = {
  ...votes, helpful: 0, not_helpful: 0, imported: { helpful: 0, not_helpful: 0 }, votes: [],
  resets: [{ id: '66666666-6666-4666-8666-666666666666', helpful: 13, not_helpful: 4, reset_by: { agent_id: '77777777-7777-4777-8777-777777777777', name: 'Ada' }, token: null,
    created_at: '2026-11-20T08:00:00Z' }],
};

function open(permissions?: string[]) {
  const wrap = (children: ReactNode) => permissions ? <PermissionsContext.Provider value={permissions}>{children}</PermissionsContext.Provider> : children;
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    {wrap(<ArticleVotesBox articleId={ids.article} title="Reset your password" timezone="UTC" headingId="votes" />)}</QueryClientProvider>);
}

test('the team reads the count the page shows, each reader\'s vote with what was missing, and the Freshdesk part', async () => {
  serve({ [`GET ${path}`]: () => json(200, votes) });
  open();
  const box = await screen.findByRole('region', { name: 'Readers\' votes' });
  expect(await within(box).findByText('13 out of 17 found this helpful')).toBeInTheDocument();
  expect(within(box).getByText('Includes 12 out of 15 from Freshdesk.')).toBeInTheDocument();
  const rows = within(box).getAllByRole('listitem');
  expect(rows[0]).toHaveTextContent('Jane Doe');
  expect(rows[0]).toHaveTextContent('No');
  expect(rows[0]).toHaveTextContent('Nov 19, 2026');
  expect(rows[0]).toHaveTextContent('The screenshot shows the old menu, write to [email removed]');
  expect(rows[1]).toHaveTextContent('Anonymous reader');
  expect(rows[1]).toHaveTextContent('Yes');
});

test('a reset asks first, starts the count from 0, and the earlier count stays readable', async () => {
  const calls = serve({ [`GET ${path}`]: () => json(200, votes), [`POST ${path}/reset`]: () => json(200, afterReset) });
  open();
  const box = await screen.findByRole('region', { name: 'Readers\' votes' });
  fireEvent.click(await within(box).findByRole('button', { name: 'Reset votes' }));
  const confirm = screen.getByRole('alertdialog', { name: 'Reset the votes of "Reset your password"?' });
  expect(confirm).toHaveTextContent('The count starts again from 0. The votes and what readers wrote are deleted; the earlier count stays in the history.');
  fireEvent.click(within(confirm).getByRole('button', { name: 'Reset votes' }));
  expect(await within(box).findByText('No votes yet.')).toBeInTheDocument();
  expect(within(box).getByText('Before Nov 20, 2026: 13 out of 17, reset by Ada')).toBeInTheDocument();
  const reset = calls.find((call) => call.method === 'POST');
  expect(reset?.path).toBe(`${path}/reset`);
  expect(reset?.headers['Idempotency-Key']).toMatch(/.{16,}/);
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
});

test('without the right to edit articles there is no reset', async () => {
  serve({ [`GET ${path}`]: () => json(200, votes) });
  open(['kb_articles:read']);
  const box = await screen.findByRole('region', { name: 'Readers\' votes' });
  expect(await within(box).findByText('13 out of 17 found this helpful')).toBeInTheDocument();
  expect(within(box).queryByRole('button', { name: 'Reset votes' })).not.toBeInTheDocument();
});

test('a failed load says so', async () => {
  serve({ [`GET ${path}`]: () => refusal(500, 'internal', 'Down') });
  open();
  const box = await screen.findByRole('region', { name: 'Readers\' votes' });
  await waitFor(() => expect(within(box).getByRole('alert')).toHaveTextContent('Could not load the votes. Reload the page to try again.'));
});
