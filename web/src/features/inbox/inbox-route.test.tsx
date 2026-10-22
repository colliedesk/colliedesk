import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { OutboundRefusedError } from '../../api/outbound-error';
import { InboxRoute } from './inbox-route';
import { testRouter } from './test-router';

const api = vi.hoisted(() => ({
  announcePresence: vi.fn(async (_id: string, _body: unknown): Promise<{ viewers: unknown[]; heartbeat_seconds: number }> => ({ viewers: [], heartbeat_seconds: 5 })), leavePresence: vi.fn(async () => undefined),
  me: vi.fn(), conversations: vi.fn(), conversation: vi.fn(), agents: vi.fn(), tags: vi.fn(), messages: vi.fn(), timeEntries: vi.fn(),
  updateConversation: vi.fn(), bulkUpdateConversations: vi.fn(), createMessage: vi.fn(), createTag: vi.fn(), config: vi.fn(),
  addConversationTag: vi.fn(), removeConversationTag: vi.fn(), retryDelivery: vi.fn(),
  contacts: vi.fn(), createContact: vi.fn(), createConversation: vi.fn(),
  savedReplies: vi.fn(async (): Promise<{ items: unknown[] }> => ({ items: [] })), contact: vi.fn(),
  emailChannel: vi.fn(async () => ({ channel: null, inbound_url: 'https://desk.example.com/api/v1/channels/email/inbound', smtp: { configured: false, host: '', port: 587, starttls: true, authenticated: false }, from_policy: 'verified', sending_from: null })),
}));

vi.mock('../../api/client', () => ({ api, InvalidRequestError: class extends Error {}, ReplyConflictError: class extends Error {} }));
vi.mock('./presence-api', () => ({ presenceApi: { announce: api.announcePresence, leave: api.leavePresence }, RetryLater: class extends Error {} }));
vi.mock('../contacts/contact-api', () => ({ contactApi: { startConversation: api.createConversation, searchContacts: api.contacts }, existingContact: () => undefined }));
const views = vi.hoisted(() => ({
  list: vi.fn(async (): Promise<{ items: unknown[] }> => ({ items: [] })), trash: vi.fn(async (): Promise<{ items: unknown[] }> => ({ items: [] })),
  create: vi.fn(), update: vi.fn(), remove: vi.fn(), restore: vi.fn(),
}));
vi.mock('../../api/saved-views', () => ({ savedViewsApi: views }));
vi.mock('@tanstack/react-router', async () => (await import('./test-router')).routerModule);
beforeEach(() => testRouter.reset());

const conversation = (id: string, subject: string) => ({ latest_reply_number: 0,
  id, contact_id: '22222222-2222-4222-8222-222222222222', contact_name: 'Mai Linh', subject,
  status: 'open' as const, priority: 'normal' as const, assignee_id: null, tags: [], channel: 'email' as const, origin: null,
  created_at: '2026-10-14T00:00:00Z', updated_at: '2026-10-14T00:00:00Z',
});

// A ticket opened by its address must not wait for the list or for the conversation detail before it asks for
// its messages: those three requests are independent, and a chain of them costs a round trip each.
test('a ticket opened by its address asks for its messages and time entries at once, before the list or the detail arrive', async () => {
  const id = '11111111-1111-4111-8111-111111111111';
  testRouter.params = { conversationId: id };
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
      for (const side of [api.agents, api.tags, api.savedReplies, api.emailChannel, views.list, views.trash]) {
        // The saved views load with their lazy menu, possibly after this test ends; when they did load, it was later.
        if (side.mock.calls.length === 0 && (side === views.list || side === views.trash)) continue;
        expect(first(critical), 'a side list was requested before the open ticket').toBeLessThan(first(side));
      }
    }
  } finally {
    testRouter.params = {};
  }
});

// A ticket opened by its address that is not in the list shows nothing of another ticket: not its header, not its
// composer. A reply typed under the wrong header would go to the wrong customer.
test('a ticket opened by address that is not in the list never shows another ticket\'s header, and its composer stays disabled, while its detail is missing', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const bravo = conversation('33333333-3333-4333-8333-333333333333', 'Bravo conversation');
  testRouter.params = { conversationId: bravo.id };
  try {
    api.me.mockResolvedValue({ timezone: 'UTC' });
    api.conversations.mockReset();
    api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
    let resolveDetail!: (value: typeof bravo) => void;
    api.conversation.mockReset();
    api.conversation.mockReturnValue(new Promise((resolve) => { resolveDetail = resolve; }));
    api.agents.mockResolvedValue({ items: [] });
    api.tags.mockResolvedValue({ items: [] });
    api.messages.mockReset();
    api.messages.mockResolvedValue({ items: [] });
    api.timeEntries.mockResolvedValue({ items: [] });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
    await waitFor(() => expect(api.messages).toHaveBeenCalledWith(bravo.id));
    await screen.findByRole('button', { name: /alpha conversation/i }); // the list has arrived, the detail has not
    expect(screen.queryByRole('heading', { name: 'Alpha conversation' })).toBeNull();
    for (const box of screen.getAllByLabelText('Message body')) expect(box).toBeDisabled(); // nothing can be sent yet
    expect(api.messages).not.toHaveBeenCalledWith(alpha.id);
    resolveDetail(bravo);
    expect(await screen.findByRole('heading', { name: 'Bravo conversation' })).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByLabelText('Message body').some((box) => !(box as HTMLTextAreaElement).disabled)).toBe(true));
  } finally {
    testRouter.params = {};
  }
});

test('a ticket whose detail fails to load never gets a usable composer, and nothing is sent to the first listed ticket', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const bravo = conversation('33333333-3333-4333-8333-333333333333', 'Bravo conversation');
  testRouter.params = { conversationId: bravo.id };
  try {
    api.me.mockResolvedValue({ timezone: 'UTC' });
    api.conversations.mockReset();
    api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
    api.conversation.mockReset();
    api.conversation.mockRejectedValue(new Error('server error'));
    api.agents.mockResolvedValue({ items: [] });
    api.tags.mockResolvedValue({ items: [] });
    api.messages.mockReset();
    api.messages.mockResolvedValue({ items: [] });
    api.timeEntries.mockResolvedValue({ items: [] });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
    await screen.findByRole('button', { name: /alpha conversation/i });
    await waitFor(() => expect(api.conversation).toHaveBeenCalledWith(bravo.id));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(screen.queryByRole('heading', { name: 'Alpha conversation' })).toBeNull();
    for (const box of screen.getAllByLabelText('Message body')) expect(box).toBeDisabled();
    expect(api.createMessage).not.toHaveBeenCalled();
  } finally {
    testRouter.params = {};
  }
});

// A ticket the person opens from the list has its row already. Its header and composer come from that row at
// once; the detail request is not made, and a slow one would not matter. This is what keeps a warm open fast.
test('opening a listed ticket renders its header and asks for its messages without any detail request', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const bravo = conversation('33333333-3333-4333-8333-333333333333', 'Bravo conversation');
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockReset();
  api.conversations.mockResolvedValue({ items: [alpha, bravo], page: 1, page_size: 50 });
  api.conversation.mockReset();
  api.conversation.mockReturnValue(new Promise(() => {}));
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockReset();
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await screen.findByRole('button', { name: /bravo conversation/i });
  fireEvent.click(screen.getByRole('button', { name: /bravo conversation/i }));
  expect(await screen.findByRole('heading', { name: 'Bravo conversation' })).toBeInTheDocument();
  await waitFor(() => expect(api.messages).toHaveBeenCalledWith(bravo.id));
  expect(api.conversation).not.toHaveBeenCalled();
});

test('a ticket opened by an address that is in the list renders from its row while the detail is still pending', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const bravo = conversation('33333333-3333-4333-8333-333333333333', 'Bravo conversation');
  testRouter.params = { conversationId: bravo.id };
  try {
    api.me.mockResolvedValue({ timezone: 'UTC' });
    api.conversations.mockReset();
    api.conversations.mockResolvedValue({ items: [alpha, bravo], page: 1, page_size: 50 });
    api.conversation.mockReset();
    api.conversation.mockReturnValue(new Promise(() => {})); // never answers
    api.agents.mockResolvedValue({ items: [] });
    api.tags.mockResolvedValue({ items: [] });
    api.messages.mockReset();
    api.messages.mockResolvedValue({ items: [] });
    api.timeEntries.mockResolvedValue({ items: [] });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
    expect(await screen.findByRole('heading', { name: 'Bravo conversation' })).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByLabelText('Message body').some((box) => !(box as HTMLTextAreaElement).disabled)).toBe(true));
  } finally {
    testRouter.params = {};
  }
});

// When the ticket an address names cannot be loaded, the pane says so and offers to try again.
test('a ticket whose detail fails to load says so and Retry asks for it again', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const bravo = conversation('33333333-3333-4333-8333-333333333333', 'Bravo conversation');
  testRouter.params = { conversationId: bravo.id };
  try {
    api.me.mockResolvedValue({ timezone: 'UTC' });
    api.conversations.mockReset();
    api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
    api.conversation.mockReset();
    api.conversation.mockRejectedValueOnce(new Error('server error')).mockResolvedValue(bravo);
    api.agents.mockResolvedValue({ items: [] });
    api.tags.mockResolvedValue({ items: [] });
    api.messages.mockReset();
    api.messages.mockResolvedValue({ items: [] });
    api.timeEntries.mockResolvedValue({ items: [] });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(/could not be loaded/i);
    expect(screen.queryByRole('heading', { name: 'Alpha conversation' })).toBeNull();
    fireEvent.click(within(alert).getByRole('button', { name: /retry/i }));
    expect(await screen.findByRole('heading', { name: 'Bravo conversation' })).toBeInTheDocument();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(api.conversation).toHaveBeenCalledTimes(2);
  } finally {
    testRouter.params = {};
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

// A reply that just failed may mean Gmail broke: the channel is read again, so the composer notice follows
// without a reload.
test('a failed delivery on the open conversation reloads the email channel', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [{
    id: '44444444-4444-4444-8444-444444444444', conversation_id: alpha.id, author_agent_id: null, author_name: null, agent_token: null, agent_request_id: null, kind: 'outgoing', body: 'Answer.',
    body_html: null, origin: null, attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', email: null,
    delivery: { status: 'failed', attempts: 1, last_error: 'Gmail stopped.', sent_at: null },
  }] });
  api.emailChannel.mockClear();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await screen.findByText('Gmail stopped.');
  await waitFor(() => expect(api.emailChannel.mock.calls.length).toBeGreaterThan(1));
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

test('the filters in the address are the list, and changing one keeps the open conversation in the address', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  testRouter.params = { conversationId: alpha.id };
  testRouter.search = { priority: 'high,urgent', has_pending_draft: true, sort: 'waiting_longest' };
  const navigate = vi.spyOn(testRouter, 'navigate');
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockReset();
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.conversation.mockResolvedValue(alpha);
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InboxRoute /></QueryClientProvider>);
  await waitFor(() => expect(api.conversations).toHaveBeenCalledWith('priority=high%2Curgent&sort=waiting_longest&has_pending_draft=true&status=open&page=1&page_size=50', expect.anything()));
  expect(await screen.findByRole('list', { name: 'Active filters' })).toHaveTextContent('Reply draft waiting for approval');
  fireEvent.click(screen.getByRole('button', { name: 'Remove filter Priority: High' }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/conversations/$conversationId', params: { conversationId: alpha.id },
    search: { priority: 'urgent', has_pending_draft: true, sort: 'waiting_longest' }, replace: false });
  await waitFor(() => expect(api.conversations).toHaveBeenLastCalledWith('priority=urgent&sort=waiting_longest&has_pending_draft=true&status=open&page=1&page_size=50', expect.anything()));
  // Opening another conversation carries the filters, so Back and a shared link show the same list.
  fireEvent.click(await screen.findByRole('button', { name: /alpha conversation/i }));
  expect(navigate).toHaveBeenLastCalledWith({ to: '/conversations/$conversationId', params: { conversationId: alpha.id },
    search: { priority: 'urgent', has_pending_draft: true, sort: 'waiting_longest' } });
});

test('a saved view is saved from the filters on screen and opens them again', async () => {
  testRouter.search = { status: 'pending', awaiting: 'team' };
  api.me.mockResolvedValue({ timezone: 'UTC' });
  api.conversations.mockReset();
  api.conversations.mockResolvedValue({ items: [], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  views.create.mockResolvedValue({});
  const urgent = { id: '99999999-9999-4999-8999-999999999999', name: 'Urgent', scope: 'shared', filters: { priority: ['urgent'] }, owner_agent_id: null,
    created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z', deleted_at: null, purges_at: null };
  views.list.mockResolvedValue({ items: [urgent] });
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InboxRoute /></QueryClientProvider>);
  // The views menu loads on its own chunk; its button shows meanwhile.
  fireEvent.click(await screen.findByText('Views', { selector: 'summary span' }));
  const menu = screen.getByRole('group', { name: 'Views' });
  fireEvent.change(within(menu).getByLabelText('View name'), { target: { value: 'Pending, our turn' } });
  fireEvent.click(within(menu).getByRole('button', { name: 'Save view' }));
  await waitFor(() => expect(views.create).toHaveBeenCalledWith({ name: 'Pending, our turn', scope: 'personal', filters: { status: ['pending'], awaiting: 'team' } }));
  fireEvent.click(await within(menu).findByRole('button', { name: 'Urgent' }));
  await waitFor(() => expect(api.conversations).toHaveBeenLastCalledWith('priority=urgent&page=1&page_size=50', expect.anything()));
});

test('on the trial with an unverified domain, New conversation says it cannot send yet; once verified it starts the conversation', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  const pat = { id: '77777777-7777-4777-8777-777777777777', name: 'Pat', email: 'pat@prospect.example', organization: null, merged_into_id: null,
    created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z' };
  api.me.mockResolvedValue({ timezone: 'UTC', plan: { plan: 'trial', trial_ends_at: '2026-11-01T00:00:00Z', locked: false, trial_extended: false, agents_over: 0, seat_paused: false } });
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  const channel = { support_address: 'support@desk.example', domain_verified_at: null };
  api.emailChannel.mockResolvedValue({ channel, inbound_url: 'https://desk.example.com/x', smtp: { configured: true, host: '', port: 587, starttls: true, authenticated: false },
    from_policy: 'verified', sending_from: 'support@desk.example' } as never);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  api.emailChannel.mockClear();
  const { unmount } = render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
  await screen.findAllByText('Alpha conversation');
  await waitFor(() => expect(api.emailChannel).toHaveBeenCalled());
  fireEvent.click(screen.getByRole('button', { name: 'New conversation' }));
  const dialog = await screen.findByRole('dialog', { name: 'Start a conversation' });
  await waitFor(() => expect(within(dialog).getByRole('status')).toHaveTextContent(/trial/));
  unmount();

  api.emailChannel.mockResolvedValue({ channel: { ...channel, domain_verified_at: '2026-10-01T00:00:00Z' }, inbound_url: 'https://desk.example.com/x',
    smtp: { configured: true, host: '', port: 587, starttls: true, authenticated: false }, from_policy: 'verified', sending_from: 'support@desk.example' } as never);
  api.contacts.mockResolvedValue({ items: [pat] });
  api.createConversation.mockResolvedValue({ ...alpha, id: '88888888-8888-4888-8888-888888888888', subject: 'Hello', contact_id: pat.id, contact_name: 'Pat' });
  api.emailChannel.mockClear();
  const second = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={second}><InboxRoute /></QueryClientProvider>);
  await screen.findAllByText('Alpha conversation');
  await waitFor(() => expect(api.emailChannel).toHaveBeenCalled());
  fireEvent.click(screen.getByRole('button', { name: 'New conversation' }));
  const ready = await screen.findByRole('dialog', { name: 'Start a conversation' });
  expect(within(ready).queryByRole('status')).not.toBeInTheDocument();
  fireEvent.change(within(ready).getByLabelText('Find a customer by name or email'), { target: { value: 'pat' } });
  fireEvent.click(await within(ready).findByRole('button', { name: /pat@prospect.example/ }));
  fireEvent.change(within(ready).getByLabelText('Subject'), { target: { value: 'Hello' } });
  fireEvent.change(within(ready).getByLabelText('Message'), { target: { value: 'First words' } });
  fireEvent.click(within(ready).getByRole('button', { name: 'Send' }));
  await waitFor(() => expect(api.createConversation).toHaveBeenCalledWith(pat.id, 'Hello', { body: 'First words', files: [], cc: [] }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
});

test('New conversation takes no input until the plan and the email channel are known', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  api.me.mockReturnValue(new Promise(() => {})); // whether the workspace is on the trial is not known yet
  api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
  api.agents.mockResolvedValue({ items: [] });
  api.tags.mockResolvedValue({ items: [] });
  api.messages.mockResolvedValue({ items: [] });
  api.timeEntries.mockResolvedValue({ items: [] });
  api.emailChannel.mockResolvedValue({ channel: { support_address: 'support@desk.example', domain_verified_at: null }, inbound_url: 'https://desk.example.com/x',
    smtp: { configured: true, host: '', port: 587, starttls: true, authenticated: false }, from_policy: 'verified', sending_from: 'support@desk.example' } as never);
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><InboxRoute /></QueryClientProvider>);
  await screen.findAllByText('Alpha conversation');
  fireEvent.click(screen.getByRole('button', { name: 'New conversation' }));
  const dialog = await screen.findByRole('dialog', { name: 'Start a conversation' });
  // Typing now could be lost: the trial may refuse the email once the plan is known.
  expect(within(dialog).getByLabelText('Subject')).toBeDisabled();
  expect(within(dialog).getByLabelText('Message')).toBeDisabled();
  expect(within(dialog).getByRole('button', { name: 'Send' })).toBeDisabled();
});

test('the open ticket is announced only after its own requests and the side lists went out, and names who else is writing', async () => {
  const alpha = conversation('11111111-1111-4111-8111-111111111111', 'Alpha conversation');
  testRouter.params = { conversationId: alpha.id };
  try {
    api.me.mockResolvedValue({ timezone: 'UTC' });
    api.conversations.mockReset();
    api.conversations.mockResolvedValue({ items: [alpha], page: 1, page_size: 50 });
    api.conversation.mockReset();
    api.conversation.mockResolvedValue(alpha);
    api.agents.mockResolvedValue({ items: [] });
    api.tags.mockResolvedValue({ items: [] });
    api.messages.mockReset();
    api.messages.mockResolvedValue({ items: [] });
    api.timeEntries.mockResolvedValue({ items: [] });
    api.announcePresence.mockReset();
    api.announcePresence.mockResolvedValue({ viewers: [{ agent_id: '44444444-4444-4444-8444-444444444444', name: 'Lan', writing: true,
      writing_since: new Date().toISOString(), edited_at: new Date().toISOString(), agent_token: null }], heartbeat_seconds: 5 });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={client}><InboxRoute /></QueryClientProvider>);
    expect(await screen.findByText('Lan is writing a reply')).toBeInTheDocument();
    expect(api.announcePresence).toHaveBeenCalledWith(alpha.id, expect.objectContaining({ writing: false }));
    const first = (mock: { mock: { invocationCallOrder: number[] } }) => mock.mock.invocationCallOrder[0];
    for (const earlier of [api.conversations, api.messages, api.timeEntries, api.agents, api.tags, api.savedReplies, api.emailChannel]) {
      expect(first(earlier)).toBeLessThan(first(api.announcePresence));
    }
    // Typing a reply is said to the others (never the text).
    fireEvent.change(screen.getByLabelText('Message body'), { target: { value: 'On its way' } });
    await waitFor(() => expect(api.announcePresence).toHaveBeenLastCalledWith(alpha.id, expect.objectContaining({ writing: true })), { timeout: 3_000 });
    expect(JSON.stringify(api.announcePresence.mock.calls)).not.toContain('On its way');
  } finally {
    testRouter.params = {};
  }
});
