import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { AGENT, CARD, CHANNEL, CONVERSATION, detail, gathering } from './intake.test.fixture';
import { ReviewRoute } from './review-route';

const { api, ConflictError } = vi.hoisted(() => ({ api: { me: vi.fn(), agents: vi.fn() },
  ConflictError: class extends Error { constructor(message: string, readonly detail?: Record<string, unknown>) { super(message); } } }));
const intake = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), decide: vi.fn(), requeue: vi.fn(), stats: vi.fn() }));
const summary = vi.hoisted(() => vi.fn());
vi.mock('../../api/client', () => ({ api, ConflictError }));
vi.mock('../../api/intake', () => ({ intakeApi: intake }));
vi.mock('../../api/intake-summary', () => ({ intakeSummary: summary }));

const page = (items = [gathering()]) => ({ items, next_cursor: null });

beforeEach(() => {
  vi.clearAllMocks();
  api.me.mockResolvedValue({ timezone: 'UTC', agent: { id: AGENT, role: 'agent' } });
  api.agents.mockResolvedValue({ items: [{ id: AGENT, display_name: 'Ann Agent', email: 'ann@example.com' }] });
  summary.mockResolvedValue({ available: true, waiting: 1, deciding: 0, scopes: [{ channel_id: CHANNEL, channel_name: 'Zalo HC Labs', scope: 'group' }] });
  intake.list.mockResolvedValue(page());
  intake.stats.mockResolvedValue({ days: 7, gatherings: 1, by_state: {}, decided_by: {}, fallback: 0, ai_decisions: 0, ai_corrected: 0 });
  intake.get.mockResolvedValue(detail());
});

const renderRoute = (tab: 'waiting' | 'dismissed' | 'decided' = 'waiting', filter = '') =>
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <ReviewRoute tab={tab} filter={filter} onChange={vi.fn()} /></QueryClientProvider>);

test('Waiting lists the cards for a person of the channel picked; a card opens with its messages and is decided once', async () => {
  intake.decide.mockResolvedValue(detail({ state: 'created', conversation_id: CONVERSATION }));
  renderRoute('waiting', `${CHANNEL}:group`);
  expect(await screen.findByRole('article', { name: 'Lan Nguyen' })).toHaveTextContent('Zalo HC Labs');
  expect(intake.list).toHaveBeenCalledWith({ state: 'review', channel_id: CHANNEL, scope: 'group', cursor: undefined });
  expect(intake.stats).toHaveBeenCalledWith(CHANNEL);
  fireEvent.click(screen.getByRole('button', { name: 'Decide' }));
  expect(await screen.findByLabelText('Subject')).toHaveValue('The scale stopped printing labels');
  expect(intake.get).toHaveBeenCalledWith(CARD);
  fireEvent.click(screen.getByRole('button', { name: 'Create conversation' }));
  await waitFor(() => expect(intake.decide).toHaveBeenCalledWith(CARD, { action: 'create', subject: 'The scale stopped printing labels' }, expect.any(String)));
  expect(await screen.findByText('Lan Nguyen\'s card opened a conversation.')).toHaveAttribute('role', 'status');
  expect(screen.getByRole('link', { name: 'Open the conversation' })).toHaveAttribute('href', `/conversations/${CONVERSATION}`);
  // The lists, the counts and the badge are read again.
  await waitFor(() => expect(intake.list.mock.calls.length).toBeGreaterThan(1));
  await waitFor(() => expect(summary.mock.calls.length).toBeGreaterThan(1));
});

test('a card a teammate decided first leaves Waiting and the page says who decided it, with its conversation', async () => {
  intake.decide.mockRejectedValue(new ConflictError('This card was decided already.', { code: 'gathering_decided', state: 'attached', decided_by: 'agent',
    decided_by_agent_id: AGENT, conversation_id: CONVERSATION }));
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: 'Decide' }));
  await screen.findByLabelText('Subject');
  intake.list.mockResolvedValue(page([]));
  fireEvent.click(screen.getByRole('button', { name: 'Create conversation' }));
  await waitFor(() => expect(screen.queryByRole('article')).not.toBeInTheDocument());
  expect(screen.getByText("Ann Agent decided Lan Nguyen's card first.")).toHaveAttribute('role', 'status');
  expect(screen.getByRole('link', { name: 'Open the conversation' })).toHaveAttribute('href', `/conversations/${CONVERSATION}`);
});

test('AI dismissed reads what AI set aside, and Put back sends the card back', async () => {
  intake.list.mockResolvedValue(page([gathering({ state: 'ignored', review_reason: null, decided_by: 'ai', decided_at: '2026-10-26T02:03:00Z' })]));
  intake.requeue.mockResolvedValue(detail());
  renderRoute('dismissed');
  fireEvent.click(await screen.findByRole('button', { name: 'Put back in queue' }));
  expect(intake.list).toHaveBeenCalledWith({ state: 'ignored', decided_by: 'ai', channel_id: undefined, scope: undefined, cursor: undefined });
  await waitFor(() => expect(intake.requeue).toHaveBeenCalledWith(CARD, expect.any(String)));
  expect(await screen.findByText('Lan Nguyen\'s card is back in Waiting.')).toBeInTheDocument();
});

test('a workspace without a chat channel that collects messages is told why the page is empty', async () => {
  summary.mockResolvedValue({ available: false, waiting: 0, deciding: 0, scopes: [] });
  intake.list.mockResolvedValue(page([]));
  renderRoute();
  expect(await screen.findByText(/Email and web forms open a conversation for every message/)).toBeInTheDocument();
  expect(intake.stats).not.toHaveBeenCalled();
});
