import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { OutboundRefusedError } from '../../api/outbound-error';
import { InboxRoute } from './inbox-route';

const api = vi.hoisted(() => ({
  me: vi.fn(), conversations: vi.fn(), agents: vi.fn(), tags: vi.fn(), messages: vi.fn(), timeEntries: vi.fn(),
  updateConversation: vi.fn(), createMessage: vi.fn(), createTag: vi.fn(), config: vi.fn(),
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

test('a reply the trial refuses stays in the composer with the reason', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.createMessage.mockRejectedValue(new OutboundRefusedError('trial_outbound_restricted', 'server text'));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await screen.findByRole('button', { name: /alpha conversation/i });
  const body = screen.getByLabelText('Message body');
  fireEvent.change(body, { target: { value: 'Please read this' } });
  fireEvent.keyDown(body, { key: 'Enter', ctrlKey: true });
  expect(await screen.findByRole('alert')).toHaveTextContent(/verify your sending domain/i);
  expect(body).toHaveValue('Please read this');
});

test('a retry the trial refuses says why instead of doing nothing', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [{
    id: '33333333-3333-4333-8333-333333333333', conversation_id: alpha.id, author_agent_id: null, author_name: null, kind: 'outgoing', body: 'Answer.',
    body_html: null, origin: null, attachments: [], created_at: '2026-10-14T00:00:00Z', email: null,
    delivery: { status: 'failed', attempts: 1, last_error: 'x', sent_at: null },
  }] });
  api.retryDelivery.mockRejectedValue(new OutboundRefusedError('trial_outbound_limit', 'server text', 7200));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: /retry delivery/i }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/200 replies in 24 hours.*about 2 hours/i);
});

test('typing in search keeps the previous rows until the new response arrives', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const beta = conversation('33333333-3333-4333-8333-333333333333', 'Beta conversation');
  let resolveSearch!: (value: unknown) => void;
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockReset();
  api.conversations.mockResolvedValueOnce({ items: [alpha, beta], page: 1, page_size: 50 })
    .mockReturnValue(new Promise((resolve) => { resolveSearch = resolve; }));
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InboxRoute /></QueryClientProvider>);
  await screen.findByRole('button', { name: /alpha conversation/i });
  fireEvent.change(screen.getByRole('searchbox', { name: /search/i }), { target: { value: 'beta' } });
  await waitFor(() => expect(api.conversations).toHaveBeenCalledTimes(2));
  expect(screen.getByRole('button', { name: /alpha conversation/i })).toBeInTheDocument();
  expect(document.querySelector('.conversation-scroll')).toHaveAttribute('aria-busy', 'true');
  resolveSearch({ items: [beta], page: 1, page_size: 50 });
  await waitFor(() => expect(screen.queryByRole('button', { name: /alpha conversation/i })).not.toBeInTheDocument());
  expect(document.querySelector('.conversation-scroll')).toHaveAttribute('aria-busy', 'false');
});

test('opening a conversation shows the loading status in its timeline until the messages arrive', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockReset();
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockReturnValue(new Promise(() => {}));
  api.timeEntries.mockResolvedValue({ items: [] });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InboxRoute /></QueryClientProvider>);
  await screen.findByRole('button', { name: /alpha conversation/i });
  expect(within(screen.getByRole('region', { name: /timeline/i })).getByRole('status')).toHaveTextContent('Loading workspace…');
});

test('the server limit from the config warns about oversized files before anything is uploaded', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.config.mockResolvedValue({ cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false }, signup: { enabled: false },
    limits: { outgoing_attachments_bytes: 1024, outgoing_attachments_files: 2 } });
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.createMessage.mockReset();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await screen.findByRole('button', { name: /alpha conversation/i });
  await waitFor(() => expect(api.config).toHaveBeenCalled());
  fireEvent.change(screen.getByLabelText('Attach files'), { target: { files: [new File([new Uint8Array(2048)], 'big.bin')] } });
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Here' } });
  expect(await screen.findByRole('alert')).toHaveTextContent('1.0 KiB');
  fireEvent.keyDown(screen.getByLabelText('Message body'), { key: 'Enter', ctrlKey: true });
  expect(api.createMessage).not.toHaveBeenCalled();
});
