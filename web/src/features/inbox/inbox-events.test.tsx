import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { InboxRoute } from './inbox-route';
import { testRouter } from './test-router';

// The two boundaries: the API and the analytics service.
const api = vi.hoisted(() => ({
  me: vi.fn(), conversations: vi.fn(), conversation: vi.fn(), agents: vi.fn(), tags: vi.fn(), messages: vi.fn(), timeEntries: vi.fn(),
  updateConversation: vi.fn(), bulkUpdateConversations: vi.fn(), config: vi.fn(),
  savedReplies: vi.fn(async () => ({ items: [] })),
  emailChannel: vi.fn(async () => ({ channel: null, inbound_url: '', smtp: { configured: false }, from_policy: 'verified', sending_from: null })),
}));
const analytics = vi.hoisted(() => ({ track: vi.fn(() => true) }));
vi.mock('../../api/client', () => ({ api, InvalidRequestError: class extends Error {} }));
const views = vi.hoisted(() => ({
  list: vi.fn(async (): Promise<{ items: unknown[] }> => ({ items: [] })), trash: vi.fn(async (): Promise<{ items: unknown[] }> => ({ items: [] })),
  create: vi.fn(), update: vi.fn(), remove: vi.fn(), restore: vi.fn(),
}));
vi.mock('../../api/saved-views', () => ({ savedViewsApi: views }));
vi.mock('../../lib/product-analytics', () => ({ track: analytics.track }));
vi.mock('@tanstack/react-router', async () => (await import('./test-router')).routerModule);

const meId = '66666666-6666-4666-8666-666666666666';
const conversation = (id: string, subject: string) => ({
  id, contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'Mai Linh', subject,
  status: 'open' as const, priority: 'normal' as const, assignee_id: null, tags: [], origin: null,
  created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z',
});
const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
const gamma = conversation('55555555-5555-4555-8555-555555555555', 'Gamma conversation');

beforeEach(() => {
  testRouter.reset();
  vi.clearAllMocks();
  api.me.mockResolvedValue({ timezone: 'UTC', agent: { id: meId, email: 'a@example.com', display_name: 'Ada', role: 'agent' } });
  api.conversations.mockResolvedValue({ items: [alpha, gamma], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.config.mockResolvedValue({ billing: { enabled: false }, limits: undefined });
});

async function open() {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InboxRoute /></QueryClientProvider>);
  await screen.findByRole('button', { name: /alpha conversation/i });
}

test('picking a conversation from the list reports conversation_opened from the list', async () => {
  await open();
  fireEvent.click(screen.getByRole('button', { name: /gamma conversation/i }));
  expect(analytics.track).toHaveBeenCalledWith('conversation_opened', { source: 'list' });
});

test('changing the status reports the new status only after the server accepted it', async () => {
  api.updateConversation.mockResolvedValue({ ...alpha, status: 'resolved' });
  await open();
  fireEvent.click(screen.getByRole('button', { name: /alpha conversation/i }));
  fireEvent.change(await screen.findByRole('combobox', { name: 'Status' }), { target: { value: 'resolved' } });
  await waitFor(() => expect(analytics.track).toHaveBeenCalledWith('conversation_status_changed', { to: 'resolved' }));
});

test('a refused status change reports nothing', async () => {
  api.updateConversation.mockRejectedValue(new Error('refused'));
  await open();
  fireEvent.click(screen.getByRole('button', { name: /alpha conversation/i }));
  fireEvent.change(await screen.findByRole('combobox', { name: 'Status' }), { target: { value: 'resolved' } });
  await waitFor(() => expect(api.updateConversation).toHaveBeenCalled());
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(analytics.track).not.toHaveBeenCalledWith('conversation_status_changed', expect.anything());
});

test('a bulk status change reports the action and how many conversations, not which', async () => {
  api.bulkUpdateConversations.mockResolvedValue({ updated: 2 });
  await open();
  fireEvent.click(screen.getByRole('checkbox', { name: /alpha conversation/i }));
  fireEvent.click(screen.getByRole('checkbox', { name: /gamma conversation/i }));
  fireEvent.click(screen.getByRole('button', { name: 'Resolved' }));
  await waitFor(() => expect(analytics.track).toHaveBeenCalledWith('bulk_action_applied', { action: 'status', count: 2 }));
  expect(JSON.stringify(analytics.track.mock.calls)).not.toContain(alpha.id);
});

test('a refused bulk change reports nothing', async () => {
  api.bulkUpdateConversations.mockRejectedValue(new Error('refused'));
  await open();
  fireEvent.click(screen.getByRole('checkbox', { name: /alpha conversation/i }));
  fireEvent.click(screen.getByRole('button', { name: 'Resolved' }));
  await waitFor(() => expect(api.bulkUpdateConversations).toHaveBeenCalled());
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(analytics.track).not.toHaveBeenCalledWith('bulk_action_applied', expect.anything());
});

test('a status tab reports inbox_filtered with the filter kind', async () => {
  await open();
  fireEvent.click(screen.getByRole('radio', { name: 'Resolved' }));
  expect(analytics.track).toHaveBeenCalledWith('inbox_filtered', { filter: 'status' });
});

test('a finished search reports whether it found anything and never the words', async () => {
  await open();
  api.conversations.mockResolvedValue({ items: [], page: 1, page_size: 50 });
  fireEvent.change(screen.getByRole('searchbox', { name: /search/i }), { target: { value: 'refund for Mai' } });
  await waitFor(() => expect(analytics.track).toHaveBeenCalledWith('inbox_searched', { has_results: false }), { timeout: 3000 });
  expect(analytics.track).toHaveBeenCalledTimes(1);
  expect(JSON.stringify(analytics.track.mock.calls)).not.toContain('refund');
});
