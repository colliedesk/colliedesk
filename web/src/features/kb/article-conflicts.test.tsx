import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import { PermissionsContext } from '../../lib/permissions-context';
import { ArticleConflictsBox } from './article-conflicts';
import { ids, json, refusal, serve } from './kb.test.fixture';

afterEach(() => { vi.unstubAllGlobals(); });

const path = `/api/v1/kb/articles/${ids.article}/conflicts`;
const ada = { agent_id: '77777777-7777-4777-8777-777777777777', name: 'Ada' };
const conflict = (id: string, excerpt: string, conversation: { id: string; display_ref: string; subject: string } | null) => ({
  id, reason: 'The reply says "48 hours", the article says "24 hours".', created_at: '2026-11-20T09:00:00Z',
  reply: { message_id: `${id.slice(0, -1)}9`, excerpt, author: { agent_id: null, name: 'Bea' }, sent_at: '2026-11-20T08:59:00Z', conversation },
  cleared_at: null, cleared_by: null, cleared_with_token: null,
});
const flagged = {
  article_id: ids.article, open: 2,
  conflicts: [
    conflict('11111111-1111-4111-8111-111111111111', 'The reset link works for 48 hours.',
      { id: '22222222-2222-4222-8222-222222222222', display_ref: '#1042', subject: 'Link expired' }),
    conflict('33333333-3333-4333-8333-333333333333', 'Links last two days now.', null),
  ],
};
const cleared = {
  ...flagged, open: 0,
  conflicts: flagged.conflicts.map((row) => ({ ...row, cleared_at: '2026-11-21T10:00:00Z', cleared_by: ada })),
};

function open(permissions?: string[]) {
  const wrap = (children: ReactNode) => permissions ? <PermissionsContext.Provider value={permissions}>{children}</PermissionsContext.Provider> : children;
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    {wrap(<ArticleConflictsBox articleId={ids.article} timezone="UTC" headingId="conflicts"
      conversationLink={(id, children) => <a href={`/conversations/${id}`}>{children}</a>} />)}</QueryClientProvider>);
}

test('the editor lists each sent reply that contradicts the article with who sent it and why, linking the conversation it can read', async () => {
  serve({ [`GET ${path}`]: () => json(200, flagged) });
  open();
  const box = await screen.findByRole('region', { name: 'May be out of date' });
  expect(await within(box).findByText('2 sent replies say something the article does not.')).toBeInTheDocument();
  const rows = within(box).getAllByRole('listitem');
  expect(rows[0]).toHaveTextContent('The reset link works for 48 hours.');
  expect(rows[0]).toHaveTextContent('Bea · Nov 20, 2026');
  expect(rows[0]).toHaveTextContent('The reply says "48 hours", the article says "24 hours".');
  expect(within(rows[0]).getByRole('link', { name: '#1042 Link expired' })).toHaveAttribute('href', '/conversations/22222222-2222-4222-8222-222222222222');
  // A reply whose conversation the member may not read names no conversation.
  expect(rows[1]).toHaveTextContent('Links last two days now.');
  expect(within(rows[1]).queryByRole('link')).not.toBeInTheDocument();
});

test('clearing the flag records who cleared it and when, and the replies stay listed', async () => {
  const calls = serve({ [`GET ${path}`]: () => json(200, flagged), [`POST ${path}/clear`]: () => json(200, cleared) });
  open();
  const box = await screen.findByRole('region', { name: 'May be out of date' });
  fireEvent.click(await within(box).findByRole('button', { name: 'Clear the flag' }));
  expect(await screen.findByRole('region', { name: 'Replies that disagreed' })).toHaveTextContent('Cleared by Ada on Nov 21, 2026.');
  expect(screen.getAllByRole('listitem')).toHaveLength(2);
  expect(screen.queryByRole('button', { name: 'Clear the flag' })).not.toBeInTheDocument();
  const clear = calls.find((call) => call.method === 'POST');
  expect(clear?.path).toBe(`${path}/clear`);
  expect(clear?.headers['Idempotency-Key']).toMatch(/.{16,}/);
});

test('without the right to edit articles the flag cannot be cleared', async () => {
  serve({ [`GET ${path}`]: () => json(200, flagged) });
  open(['kb_articles:read']);
  const box = await screen.findByRole('region', { name: 'May be out of date' });
  expect(await within(box).findByText('2 sent replies say something the article does not.')).toBeInTheDocument();
  expect(within(box).queryByRole('button', { name: 'Clear the flag' })).not.toBeInTheDocument();
});

test('a failed clear says so and keeps the flag', async () => {
  serve({ [`GET ${path}`]: () => json(200, flagged), [`POST ${path}/clear`]: () => refusal(500, 'internal', 'Down') });
  open();
  const box = await screen.findByRole('region', { name: 'May be out of date' });
  fireEvent.click(await within(box).findByRole('button', { name: 'Clear the flag' }));
  await waitFor(() => expect(within(box).getByRole('alert')).toHaveTextContent('Could not clear the flag. Try again.'));
  expect(within(box).getByRole('button', { name: 'Clear the flag' })).toBeInTheDocument();
});
