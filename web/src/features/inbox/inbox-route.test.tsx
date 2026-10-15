import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { InboxRoute } from './inbox-route';

const api = vi.hoisted(() => ({
  me: vi.fn(), conversations: vi.fn(), agents: vi.fn(), tags: vi.fn(), messages: vi.fn(), timeEntries: vi.fn(),
  updateConversation: vi.fn(), createMessage: vi.fn(), createTag: vi.fn(),
  addConversationTag: vi.fn(), removeConversationTag: vi.fn(), retryDelivery: vi.fn(),
  emailChannel: vi.fn(async () => ({ channel: null, inbound_url: 'https://desk.example.com/api/v1/channels/email/inbound', smtp: { configured: false, host: '', port: 587, starttls: true, authenticated: false }, from_policy: 'verified', sending_from: null })),
}));

vi.mock('../../api/client', () => ({ api }));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn(), useParams: () => ({}) }));

const conversation = (id: string, subject: string) => ({
  id, contact_id: '22222222-2222-4222-8222-222222222222', subject,
  status: 'open' as const, priority: 'normal' as const, assignee_id: null, tags: [], origin: null,
  created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z',
});

test('inline tag creation stays bound to the conversation selected at call time', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const beta = conversation('33333333-3333-4333-8333-333333333333', 'Beta conversation');
  const tag = { id: '44444444-4444-4444-8444-444444444444', name: 'Escalated', color: '#64748b' };
  let resolveTag!: (value: typeof tag) => void;
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockResolvedValue({ items: [alpha, beta], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.createTag.mockReturnValue(new Promise((resolve) => { resolveTag = resolve; }));
  api.addConversationTag.mockResolvedValue({ ...alpha, tags: [tag] });

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await screen.findByRole('button', { name: /alpha conversation/i });
  fireEvent.change(screen.getByRole('textbox', { name: /new tag name/i }), { target: { value: tag.name } });
  fireEvent.click(screen.getByRole('button', { name: /create tag/i }));
  fireEvent.click(screen.getByRole('button', { name: /beta conversation/i }));
  resolveTag(tag);

  await waitFor(() => expect(api.addConversationTag).toHaveBeenCalledWith(alpha.id, tag.id));
  expect(api.addConversationTag).not.toHaveBeenCalledWith(beta.id, tag.id);
  expect(api.updateConversation).not.toHaveBeenCalledWith(beta.id, expect.objectContaining({ tag_ids: expect.anything() }));
});

test('loads the time entries of the selected conversation', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [{ id: '55555555-5555-4555-8555-555555555555', conversation_id: alpha.id, agent_id: null,
    agent_name: 'Alice Agent', minutes: 30, note: 'Setup', billable: true, executed_at: '2025-01-16T02:00:00Z' }] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  expect(await screen.findByText('Total 30 min')).toBeInTheDocument();
  expect(api.timeEntries).toHaveBeenCalledWith(alpha.id);
});

test('coming back to the inbox shows mail that arrived meanwhile, even within the cache window', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockReset();
  api.conversations.mockResolvedValueOnce({ items: [], page: 1, page_size: 50 }).mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  // Same cache settings as the app (main.tsx).
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 15_000 } } });
  const first = render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await waitFor(() => expect(api.conversations).toHaveBeenCalledTimes(1));
  first.unmount(); // the agent opens Settings, the customer's email arrives
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  expect(await screen.findByRole('button', { name: /alpha conversation/i })).toBeInTheDocument();
});
