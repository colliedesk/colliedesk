import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { OutboundRefusedError } from '../../api/outbound-error';
import { InboxRoute } from './inbox-route';

const api = vi.hoisted(() => ({
  me: vi.fn(), conversations: vi.fn(), conversation: vi.fn(), agents: vi.fn(), tags: vi.fn(), messages: vi.fn(), timeEntries: vi.fn(),
  updateConversation: vi.fn(), bulkUpdateConversations: vi.fn(), createMessage: vi.fn(), createTag: vi.fn(), config: vi.fn(),
  addConversationTag: vi.fn(), removeConversationTag: vi.fn(), retryDelivery: vi.fn(),
  savedReplies: vi.fn(async (): Promise<{ items: unknown[] }> => ({ items: [] })), contact: vi.fn(),
  emailChannel: vi.fn(async () => ({ channel: null, inbound_url: 'https://desk.example.com/api/v1/channels/email/inbound', smtp: { configured: false, host: '', port: 587, starttls: true, authenticated: false }, from_policy: 'verified', sending_from: null })),
}));

vi.mock('../../api/client', () => ({ api }));
const router = vi.hoisted(() => ({ params: {} as { conversationId?: string } }));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn(), useParams: () => router.params }));

const conversation = (id: string, subject: string) => ({
  id, contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'Mai Linh', subject,
  status: 'open' as const, priority: 'normal' as const, assignee_id: null, tags: [], origin: null,
  created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z',
});

// A ticket opened by its address must not wait for the list or for the conversation detail before it asks for
// its messages: those three requests are independent, and a chain of them costs a round trip each.
test('a ticket opened by its address asks for its messages and time entries at once, before the list or the detail arrive', async () => {
  const id = '11111111-1111-4111-8111-111111111111';
  router.params = { conversationId: id };
  try {
    api.me.mockResolvedValue({ timezone: 'UTC' });
    api.conversations.mockReset();
    api.conversations.mockReturnValue(new Promise(() => {})); // the list never answers in this test
    api.conversation.mockReturnValue(new Promise(() => {})); // nor does the detail
    api.agents.mockResolvedValue({ items: [] });
    api.tags.mockResolvedValue({ items: [] });
    api.messages.mockReset();
    api.messages.mockReturnValue(new Promise(() => {}));
    api.timeEntries.mockReset();
    api.timeEntries.mockReturnValue(new Promise(() => {}));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
    await waitFor(() => expect(api.messages).toHaveBeenCalledWith(id));
    expect(api.timeEntries).toHaveBeenCalledWith(id);
    expect(api.conversations).toHaveBeenCalled();
    expect(api.conversation).toHaveBeenCalledWith(id);
    // What the open ticket needs goes out before the side lists (agents, tags, saved replies, channel): a
    // browser allows only six HTTP/1.1 connections per host, and the later requests wait for a free one.
    const first = (mock: { mock: { invocationCallOrder: number[] } }) => mock.mock.invocationCallOrder[0];
    for (const critical of [api.conversations, api.conversation, api.messages, api.timeEntries]) {
      for (const side of [api.agents, api.tags, api.savedReplies, api.emailChannel]) {
        expect(first(critical), 'a side list was requested before the open ticket').toBeLessThan(first(side));
      }
    }
  } finally {
    router.params = {};
  }
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
    id: '33333333-3333-4333-8333-333333333333', conversation_id: alpha.id, author_agent_id: null, author_name: null, agent_token: null, agent_request_id: null, kind: 'outgoing', body: 'Answer.',
    body_html: null, origin: null, attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', email: null,
    delivery: { status: 'failed', attempts: 1, last_error: 'x', sent_at: null },
  }] });
  api.retryDelivery.mockRejectedValue(new OutboundRefusedError('trial_outbound_limit', 'server text', 7200));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  fireEvent.click(await screen.findByRole('button', { name: /retry delivery/i }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/200 people in 24 hours, counting everyone copied.*about 2 hours/i);
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

test('a bulk status change sends the selected ids, then reloads the list and reports the result', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const beta = conversation('33333333-3333-4333-8333-333333333333', 'Beta conversation');
  const gamma = conversation('55555555-5555-4555-8555-555555555555', 'Gamma conversation');
  api.me.mockResolvedValue({ timezone: 'UTC', agent: { id: '66666666-6666-4666-8666-666666666666', email: 'alice@example.com', display_name: 'Alice', role: 'agent' } });
  api.conversations.mockReset();
  api.conversations.mockResolvedValue({ items: [alpha, beta, gamma], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.bulkUpdateConversations.mockResolvedValue({ updated: 2 });

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await screen.findByRole('button', { name: /alpha conversation/i });
  fireEvent.click(screen.getByRole('checkbox', { name: /alpha conversation/i }));
  fireEvent.click(screen.getByRole('checkbox', { name: /gamma conversation/i }));
  const loadsBefore = api.conversations.mock.calls.length;
  fireEvent.click(screen.getByRole('button', { name: 'Resolved' }));

  await waitFor(() => expect(api.bulkUpdateConversations).toHaveBeenCalledWith([alpha.id, gamma.id], { status: 'resolved' }));
  expect(await screen.findByText('Updated 2 conversations')).toHaveAttribute('aria-live', 'polite');
  await waitFor(() => expect(api.conversations.mock.calls.length).toBeGreaterThan(loadsBefore));
  expect(screen.queryByRole('group', { name: /bulk actions/i })).not.toBeInTheDocument();
});

test('a refused bulk change reloads the list, so a conversation that is gone stops being selectable', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockResolvedValue({ timezone: 'UTC', agent: { id: '66666666-6666-4666-8666-666666666666', email: 'alice@example.com', display_name: 'Alice', role: 'agent' } });
  api.conversations.mockReset();
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.bulkUpdateConversations.mockRejectedValue(new Error('API 404'));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await screen.findByRole('button', { name: /alpha conversation/i });
  fireEvent.click(screen.getByRole('checkbox', { name: /alpha conversation/i }));
  const loadsBefore = api.conversations.mock.calls.length;
  fireEvent.click(screen.getByRole('button', { name: 'Closed' }));
  expect(await screen.findByRole('alert')).toBeInTheDocument();
  await waitFor(() => expect(api.conversations.mock.calls.length).toBeGreaterThan(loadsBefore));
});

const quietApis = () => {
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
};
const renderRoute = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
};

test('the inbox opens on open conversations', async () => {
  quietApis();
  api.conversations.mockReset();
  api.conversations.mockResolvedValue({ items: [conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation')], page: 1, page_size: 50 });
  renderRoute();
  await screen.findByRole('button', { name: /alpha conversation/i });
  expect(new URLSearchParams(api.conversations.mock.calls[0][0]).get('status')).toBe('open');
});

test('a workspace whose conversations are all closed says nothing is open, not "Get started"', async () => {
  quietApis();
  api.conversations.mockReset();
  const closed = { ...conversation('11111111-1111-4111-8111-111111111111', 'Imported from Freshdesk'), status: 'closed' as const };
  api.conversations.mockImplementation(async (query: string) =>
    ({ items: new URLSearchParams(query).get('status') === 'open' ? [] : [closed], page: 1, page_size: 50 }));
  renderRoute();
  expect(await screen.findByText('No open conversations. Every customer has an answer.')).toBeInTheDocument();
  expect(screen.queryByRole('region', { name: /get started/i })).not.toBeInTheDocument();
});

test('a brand-new workspace still shows "Get started"', async () => {
  quietApis();
  api.conversations.mockReset();
  api.conversations.mockResolvedValue({ items: [], page: 1, page_size: 50 });
  renderRoute();
  expect(await screen.findByRole('region', { name: /get started/i })).toBeInTheDocument();
});

test('resolving the open conversation keeps it on screen although it left the Open list', async () => {
  quietApis();
  api.conversations.mockReset();
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const resolved = { ...alpha, status: 'resolved' as const };
  let isResolved = false;
  api.conversations.mockImplementation(async () => ({ items: isResolved ? [] : [alpha], page: 1, page_size: 50 }));
  api.conversation.mockResolvedValue(resolved);
  api.updateConversation.mockImplementation(async () => { isResolved = true; return resolved; });
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: /alpha conversation/i }));
  fireEvent.change(screen.getByRole('combobox', { name: 'Status' }), { target: { value: 'resolved' } });
  await waitFor(() => expect(api.conversation).toHaveBeenCalledWith(alpha.id));
  expect(await screen.findByRole('heading', { name: 'Alpha conversation' })).toBeInTheDocument();
});

test('resolving the open conversation never swaps the screen to another one, so its draft survives', async () => {
  quietApis();
  api.conversations.mockReset();
  api.conversation.mockReset();
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const beta = conversation('33333333-3333-4333-8333-333333333333', 'Beta conversation');
  let isResolved = false;
  api.conversations.mockImplementation(async () => ({ items: isResolved ? [beta] : [alpha, beta], page: 1, page_size: 50 }));
  api.conversation.mockReturnValue(new Promise(() => {})); // the by-id fetch never answers: only the update response can keep alpha
  api.updateConversation.mockImplementation(async () => { isResolved = true; return { ...alpha, status: 'resolved' as const }; });
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: /alpha conversation/i }));
  fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'Half-written answer' } });
  fireEvent.change(screen.getByRole('combobox', { name: 'Status' }), { target: { value: 'resolved' } });
  await waitFor(() => expect(screen.queryByRole('button', { name: /alpha conversation/i })).not.toBeInTheDocument());
  expect(screen.getByRole('heading', { name: 'Alpha conversation' })).toBeInTheDocument();
  expect(screen.queryByRole('heading', { name: 'Beta conversation' })).not.toBeInTheDocument();
  expect(screen.getByLabelText('Message body')).toHaveValue('Half-written answer');
});

test('when the caught-up check fails, the inbox does not pretend the workspace is new', async () => {
  quietApis();
  api.conversations.mockReset();
  api.conversations.mockImplementation(async (query: string) => {
    if (new URLSearchParams(query).get('status') === 'open') return { items: [], page: 1, page_size: 50 };
    throw new Error('network');
  });
  renderRoute();
  expect(await screen.findByText('No open conversations. Every customer has an answer.')).toBeInTheDocument();
  expect(screen.queryByRole('region', { name: /get started/i })).not.toBeInTheDocument();
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

test('a saved reply inserted the moment a conversation is selected already has the customer name, with no extra request', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.contact.mockReset();
  api.me.mockResolvedValue({ timezone: 'UTC', agent: { id: '66666666-6666-4666-8666-666666666666', email: 'chinh@example.com', display_name: 'Chinh', role: 'agent' } });
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.savedReplies.mockResolvedValue({ items: [{ id: '77777777-7777-4777-8777-777777777777', title: 'Greeting', body: 'Hi {{customer.name}}, {{agent.name}} here.',
    created_by_agent_id: null, created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z' }] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await waitFor(() => expect(screen.getByRole('button', { name: 'Insert saved reply' })).toBeEnabled());
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Greeting' }));
  expect(screen.getByLabelText('Message body')).toHaveValue('Hi Mai Linh, Chinh here.');
  expect(api.contact).not.toHaveBeenCalled();
});

test('a saved reply signs with the email name of an agent who has no display name', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockResolvedValue({ timezone: 'UTC', agent: { id: '66666666-6666-4666-8666-666666666666', email: 'qa-b@hclabs.colliedesk.com', display_name: '', role: 'agent' } });
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.savedReplies.mockResolvedValue({ items: [{ id: '77777777-7777-4777-8777-777777777777', title: 'Closing', body: 'Best regards,\n{{agent.name}}',
    created_by_agent_id: null, created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z' }] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await waitFor(() => expect(screen.getByRole('button', { name: 'Insert saved reply' })).toBeEnabled());
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Closing' }));
  expect(screen.getByLabelText('Message body')).toHaveValue('Best regards,\nqa-b');
});

test('Manage saved replies opens Settings in a new tab, so a draft is not lost', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const open = vi.spyOn(window, 'open').mockReturnValue(null);
  api.me.mockResolvedValue({ timezone: 'UTC', agent: { id: '66666666-6666-4666-8666-666666666666', email: 'chinh@example.com', display_name: 'Chinh', role: 'agent' } });
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.savedReplies.mockResolvedValue({ items: [] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await waitFor(() => expect(screen.getByRole('button', { name: 'Insert saved reply' })).toBeEnabled());
  fireEvent.click(screen.getByRole('button', { name: 'Insert saved reply' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Manage saved replies' }));
  expect(open).toHaveBeenCalledExactlyOnceWith('/settings', '_blank', 'noopener');
  open.mockRestore();
});
