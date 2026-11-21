import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ArticleLinkPicker } from './article-link-picker';
import { article, json, serve, summary } from './kb.test.fixture';

afterEach(() => { vi.unstubAllGlobals(); });

const center = { enabled: true, title: '', locale: 'en', require_approval: false, content_version: 3, updated_at: null, show_tags: false, url: 'https://desk.example.com/help/acme' };
const reset = summary(7, 'Reset your password', 'published');
const internal = { ...summary(8, 'Refund rules', 'published'), id: '88888888-8888-4888-8888-888888888888', effective_visibility: 'agents' as const };
const botOnly = { ...summary(9, 'Plan limits', 'published'), id: '99999999-9999-4999-8999-999999999999', visibility: 'help_chat' as const, effective_visibility: 'help_chat' as const };
const handlers = {
  'GET /api/v1/help-center': () => json(200, center),
  'GET /api/v1/kb/articles': () => json(200, { items: [reset, internal, botOnly], next_cursor: null }),
  [`GET /api/v1/kb/articles/${internal.id}`]: () => json(200, article({ ...internal, body_md: 'Refunds over 30 days need a lead.' })),
};

function open(props: Partial<Parameters<typeof ArticleLinkPicker>[0]>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><ArticleLinkPicker onClose={vi.fn()} {...props} /></QueryClientProvider>);
}

test('in a reply, an article not everyone sees says who sees it and inserts its text instead of a link', async () => {
  const calls = serve(handlers);
  const onInsert = vi.fn();
  open({ onInsert });
  const option = await screen.findByRole('option', { name: 'Refund rules' });
  expect(option).toHaveAccessibleDescription('Agents only Inserts its text: this article has no public page to link.');
  expect(screen.getByRole('option', { name: 'Plan limits' })).toHaveAccessibleDescription('Help chat only Inserts its text: this article has no public page to link.');
  expect(screen.getByRole('option', { name: 'Reset your password' })).toHaveAccessibleDescription('desk.example.com/help/acme/a/7-reset-your-password');
  fireEvent.click(option);
  await waitFor(() => expect(onInsert).toHaveBeenCalledExactlyOnceWith('Refunds over 30 days need a lead.'));
  expect(calls.some((call) => call.path === `/api/v1/kb/articles/${internal.id}`)).toBe(true);
});

test('an article that links another offers only the ones with a public page', async () => {
  serve(handlers);
  const onPick = vi.fn();
  open({ onPick });
  expect(await screen.findByRole('option', { name: 'Reset your password' })).toBeInTheDocument();
  expect(screen.queryByRole('option', { name: 'Refund rules' })).not.toBeInTheDocument();
  expect(screen.queryByRole('option', { name: 'Plan limits' })).not.toBeInTheDocument();
});
