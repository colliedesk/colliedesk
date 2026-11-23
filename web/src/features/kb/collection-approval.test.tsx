import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { PermissionsContext } from '../../lib/permissions-context';
import { KnowledgeRoute } from './knowledge-page';
import { collections, ids, json, me, serve, summary } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const center = (on: boolean) => ({ enabled: true, title: '', locale: 'en', require_approval: on, content_version: 1, updated_at: null, show_tags: false, feedback_conversations: false, url: 'https://desk.example.com/help/acme' });
const base = {
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  'GET /api/v1/kb/articles': () => json(200, { items: [summary(1, 'Reset your password')], next_cursor: null }),
  'GET /api/v1/help-center': () => json(200, center(true)),
};
const setup = collections.find((item) => item.id === ids.setup);

function open(held?: string[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><PermissionsContext.Provider value={held}><KnowledgeRoute search={{ collection: ids.setup }} /></PermissionsContext.Provider></QueryClientProvider>);
}

test('a collection overrides the help center\'s approval in its form, naming what inherit means, and sends it only when changed', async () => {
  const calls = serve({ ...base, [`PATCH /api/v1/kb/collections/${ids.setup}`]: () => json(200, { ...setup, require_approval: 'off' }) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Setup' }));
  const form = screen.getByRole('form', { name: 'Edit Setup' });
  const select = await within(form).findByRole('combobox', { name: 'Approval before publishing' });
  expect(select).toHaveValue('inherit');
  await waitFor(() => expect(within(select).getAllByRole('option').map((option) => option.textContent)).toEqual(['As the help center (required)', 'Required', 'Not required']));
  fireEvent.change(select, { target: { value: 'off' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save collection' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: setup?.updated_at, require_approval: 'off' }));
});

test('a role that may not change the help center edits a collection without the approval choice', async () => {
  serve(base);
  open(['kb_articles:read', 'kb_articles:create', 'kb_articles:update']);
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Setup' }));
  const form = screen.getByRole('form', { name: 'Edit Setup' });
  expect(within(form).queryByRole('combobox', { name: 'Approval before publishing' })).not.toBeInTheDocument();
});
