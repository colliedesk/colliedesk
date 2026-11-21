import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { KnowledgeRoute } from './knowledge-page';
import type { KnowledgeSearch } from './knowledge-page';
import { collections, json, me, serve, summary } from './kb.test.fixture';

const navigate = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }));
afterEach(() => { navigate.mockReset(); vi.unstubAllGlobals(); });

function open(search: KnowledgeSearch = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(<QueryClientProvider client={client}><KnowledgeRoute search={search} /></QueryClientProvider>);
  return (next: KnowledgeSearch) => view.rerender(<QueryClientProvider client={client}><KnowledgeRoute search={next} /></QueryClientProvider>);
}

const helpCenterOn = () => json(200, { enabled: true, title: '', locale: 'en', require_approval: false, content_version: 1, updated_at: null, url: 'https://desk.example.com/help/acme' });
const internal = { ...summary(1, 'Refund rules', 'published'), effective_visibility: 'agents' as const };
const botOnly = { ...summary(3, 'Plan limits', 'published'), visibility: 'help_chat' as const, effective_visibility: 'help_chat' as const };
const base = {
  'GET /api/v1/auth/me': () => json(200, me),
  'GET /api/v1/kb/collections': () => json(200, { items: collections }),
  'GET /api/v1/help-center': helpCenterOn,
  'GET /api/v1/kb/articles': () => json(200, { items: [internal, botOnly, summary(2, 'Add a teammate', 'published')], next_cursor: null }),
};

test('a restricted article shows who can see it in the list, and only an article for everyone opens on the help center', async () => {
  serve(base);
  open();
  const refund = (await screen.findByText('Refund rules')).closest('li')!;
  const plans = screen.getByText('Plan limits').closest('li')!;
  const teammate = screen.getByText('Add a teammate').closest('li')!;
  expect(within(refund).getByText('Agents only')).toBeInTheDocument();
  expect(within(plans).getByText('Help chat only')).toBeInTheDocument();
  expect(teammate).not.toHaveTextContent(/only/i);
  await waitFor(() => expect(within(teammate).getByRole('link', { name: 'Open Add a teammate on the help center' })).toBeInTheDocument());
  expect(within(refund).queryByRole('link', { name: 'Open Refund rules on the help center' })).not.toBeInTheDocument();
  expect(within(plans).queryByRole('link', { name: 'Open Plan limits on the help center' })).not.toBeInTheDocument();
});

test('the list can keep only the articles one audience sees', async () => {
  const calls = serve(base);
  const show = open();
  const filter = await screen.findByRole('combobox', { name: 'Who can see' });
  expect(filter).toHaveValue('');
  fireEvent.change(filter, { target: { value: 'agents' } });
  expect(navigate).toHaveBeenLastCalledWith({ to: '/knowledge', search: { visibility: 'agents' }, replace: false });
  show({ visibility: 'agents' });
  await waitFor(() => expect(calls.some((call) => call.path === '/api/v1/kb/articles' && call.search.get('visibility') === 'agents')).toBe(true));
  expect(screen.getByRole('combobox', { name: 'Who can see' })).toHaveValue('agents');
});
