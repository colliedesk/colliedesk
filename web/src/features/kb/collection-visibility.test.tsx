import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { KnowledgeRoute } from './knowledge-page';
import type { KnowledgeSearch } from './knowledge-page';
import { collection, collections, ids, json, me, refusal, serve, summary } from './kb.test.fixture';

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
afterEach(() => { vi.unstubAllGlobals(); });

const base = {
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  'GET /api/v1/kb/articles': () => json(200, { items: [summary(1, 'Reset your password')], next_cursor: null }),
  'GET /api/v1/help-center': () => json(200, { enabled: true, title: '', locale: 'en', require_approval: false, content_version: 1, updated_at: null, show_tags: false, url: 'https://desk.example.com/help/acme' }),
};
const setup = collections.find((item) => item.id === ids.setup)!;
const helpText = 'Applies to every article in it and its collections.';
const tipText = 'An article is as restricted as the most restrictive of itself and every collection it is in.';

function open(search: KnowledgeSearch = { collection: ids.setup }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><KnowledgeRoute search={search} /></QueryClientProvider>);
}

test('a collection says who can see its articles, and sends only a changed choice', async () => {
  const calls = serve({ ...base, [`PATCH /api/v1/kb/collections/${ids.setup}`]: () => json(200, { ...setup, visibility: 'agents', effective_visibility: 'agents' }) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Setup' }));
  const form = screen.getByRole('form', { name: 'Edit Setup' });
  const select = within(form).getByRole('combobox', { name: 'Who can see this' });
  expect(select).toHaveValue('everyone');
  expect(within(select).getAllByRole('option').map((option) => option.textContent)).toEqual(['Everyone', 'Help chat only', 'Agents only']);
  expect(within(form).getByText(helpText)).toBeInTheDocument();
  expect(within(form).getByRole('button', { name: 'About Who can see this' })).toHaveAccessibleDescription(tipText);
  fireEvent.change(select, { target: { value: 'agents' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save collection' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({ updated_at: setup.updated_at, visibility: 'agents' }));
});

test('opening a collection to everyone the server refuses says why and keeps the form', async () => {
  const reason = '2 articles would become public without an approved review: 55555555-5555-4555-8555-555555555555, 66666666-6666-4666-8666-666666666666.';
  const hidden = { ...setup, visibility: 'agents' as const, effective_visibility: 'agents' as const };
  serve({ ...base, 'GET /api/v1/kb/collections': () => json(200, { items: collections.map((item) => item.id === ids.setup ? hidden : item) }),
    [`PATCH /api/v1/kb/collections/${ids.setup}`]: () => refusal(409, 'approval_required', reason) });
  open();
  fireEvent.click(await screen.findByRole('button', { name: 'Edit Setup' }));
  const form = screen.getByRole('form', { name: 'Edit Setup' });
  const select = within(form).getByRole('combobox', { name: 'Who can see this' });
  expect(select).toHaveValue('agents');
  fireEvent.change(select, { target: { value: 'everyone' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save collection' }));
  expect(await within(form).findByRole('alert')).toHaveTextContent(reason);
  expect(select).toHaveValue('everyone');
});

test('a new collection can start for help chat only', async () => {
  const calls = serve({ ...base, 'POST /api/v1/kb/collections': () => json(201, { ...collection(ids.billing, 'Billing', null), visibility: 'help_chat', effective_visibility: 'help_chat' }) });
  open({});
  fireEvent.click((await screen.findAllByRole('button', { name: 'New collection' }))[0]!);
  const form = screen.getByRole('form', { name: 'New collection' });
  fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'Billing' } });
  fireEvent.change(within(form).getByRole('combobox', { name: 'Who can see this' }), { target: { value: 'help_chat' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Create collection' }));
  await waitFor(() => expect(calls.find((call) => call.method === 'POST')?.body).toEqual({ name: 'Billing', parent_id: null, visibility: 'help_chat' }));
});
