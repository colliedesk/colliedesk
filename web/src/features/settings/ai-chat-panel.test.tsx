import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { AIChatSession, AIChatSummary, AIChatUsage } from '../../api/ai';
import { AIChatPanel, ChatTranscript } from './ai-chat-panel';
import { AIChatSettingsRoute } from './ai-chat-route';

const aiApi = vi.hoisted(() => ({ chatUsage: vi.fn(), chatSessions: vi.fn(), chatSession: vi.fn(), deleteChatSession: vi.fn(), chatPreview: vi.fn() }));
vi.mock('../../api/ai', () => ({ aiApi }));
vi.mock('../../api/client', async (actual) => ({ ...(await actual<object>()), api: {
  me: () => Promise.resolve({ agent: { role: 'admin' }, timezone: 'UTC' }), config: () => Promise.resolve({ billing: { enabled: false } }) } }));
vi.mock('./settings-nav', () => ({ SettingsNav: () => null }));

const summary = (change: Partial<AIChatSummary> = {}): AIChatSummary => ({
  id: '11111111-1111-4111-8111-111111111111', channel_id: '22222222-2222-4222-8222-222222222222', channel_name: 'Help chat', surface: 'help_center',
  status: 'open', turns: 2, first_question: 'Where is my order?', not_helpful: false, conversation_id: null,
  created_at: '2026-10-28T10:00:00Z', last_turn_at: '2026-10-28T11:00:00Z', ended_at: null, kept_until: '2027-01-26T11:00:00Z', ...change,
});
const usage = (change: Partial<AIChatUsage> = {}): AIChatUsage => ({
  month: '2026-10', answers: 413, answers_today: 18, answer_limit: 750, day_answer_limit: 75, spent_micro_usd: null, limit_micro_usd: null,
  spent_percent: 61, chats: 128, handed_off: 9, rated_helpful: 34, rated: 41, articles: 38, resets_at: '2026-11-01T00:00:00Z', ...change,
});

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{children}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
});

test('the month shows answers against the plan, spend only as a share, and the chats; a filter asks the server', async () => {
  aiApi.chatUsage.mockResolvedValue(usage());
  aiApi.chatSessions.mockResolvedValue({ items: [summary(), summary({ id: '33333333-3333-4333-8333-333333333333', status: 'handed_off', first_question: null, not_helpful: true })], next_cursor: null });
  const open = vi.fn();
  wrap(<AIChatPanel timezone="UTC" onOpen={open} />);
  expect(await screen.findByText('413 of 750')).toBeTruthy();
  expect(screen.getByText('61% of the budget')).toBeTruthy();
  expect(screen.queryByText(/\$/)).toBeNull();
  expect(screen.getByText('9 handed off · 83% rated helpful (34 of 41)')).toBeTruthy();
  const table = await screen.findByRole('table');
  expect(within(table).getByText('Text expired')).toBeTruthy();
  fireEvent.click(within(table).getByRole('link', { name: 'Where is my order?' }));
  expect(open).toHaveBeenCalledWith('11111111-1111-4111-8111-111111111111');
  fireEvent.click(screen.getByRole('tab', { name: 'Handed off' }));
  await waitFor(() => expect(aiApi.chatSessions).toHaveBeenLastCalledWith({ status: 'handed_off', notHelpful: false, cursor: '' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Only “Not helpful”' }));
  await waitFor(() => expect(aiApi.chatSessions).toHaveBeenLastCalledWith({ status: 'handed_off', notHelpful: true, cursor: '' }));
});

test('a reached limit pauses answers and says so; no published article asks for one first', async () => {
  aiApi.chatUsage.mockResolvedValue(usage({ answers: 750, articles: 0 }));
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} />);
  expect((await screen.findByRole('alert')).textContent).toMatch(/paused until/);
  expect(screen.getByText(/Publish articles first/)).toBeTruthy();
  expect(await screen.findByText(/No chats yet/)).toBeTruthy();
});

test('a workspace key shows dollars against its own limit', async () => {
  aiApi.chatUsage.mockResolvedValue(usage({ answer_limit: null, day_answer_limit: null, spent_micro_usd: 4_120_000, limit_micro_usd: 10_000_000, spent_percent: 41 }));
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} />);
  expect(await screen.findByText('$4.12 of $10.00')).toBeTruthy();
  expect(screen.getByText('Today: 18')).toBeTruthy();
});

const session = (change: Partial<AIChatSession> = {}): AIChatSession => ({
  ...summary({ status: 'handed_off', conversation_id: '44444444-4444-4444-8444-444444444444' }), history_cut_at: null,
  transcript: [{ id: '55555555-5555-4555-8555-555555555555', seq: 1, question: 'Where is my order?', answer: 'It ships in 2 days.', status: 'answered',
    error_code: null, viewing_article_id: null, sources: [{ ordinal: 1, article_id: null, number: 12, version: 3, title: 'Shipping times', folder: 'Orders' }],
    feedback: 'not_helpful', created_at: '2026-10-28T10:00:00Z', finished_at: '2026-10-28T10:00:02Z' }],
  ...change,
});

test('an admin reads a transcript, opens its conversation and deletes it for good after confirming', async () => {
  aiApi.chatSession.mockResolvedValue(session());
  aiApi.deleteChatSession.mockResolvedValue(undefined);
  const back = vi.fn();
  wrap(<ChatTranscript id="11111111-1111-4111-8111-111111111111" timezone="UTC" canDelete back="AI chat" backHref="/settings/ai?tab=chat" onBack={back} />);
  expect(await screen.findByText('It ships in 2 days.')).toBeTruthy();
  expect(screen.getByText('Shipping times')).toBeTruthy();
  expect(screen.getByText('Rated not helpful')).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Open conversation' }).getAttribute('href')).toBe('/conversations/44444444-4444-4444-8444-444444444444');
  fireEvent.click(screen.getByRole('button', { name: 'Delete chat' }));
  const dialog = await screen.findByRole('alertdialog');
  expect(within(dialog).getByText(/keeps its copy/)).toBeTruthy();
  expect(aiApi.deleteChatSession).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Delete chat' }));
  await waitFor(() => expect(back).toHaveBeenCalled());
  expect(aiApi.deleteChatSession).toHaveBeenCalledWith('11111111-1111-4111-8111-111111111111');
});

test('a team member reads a transcript without Delete, and a chat of another workspace is not there', async () => {
  aiApi.chatSession.mockResolvedValueOnce(session());
  const first = wrap(<ChatTranscript id="1" timezone="UTC" canDelete={false} back="AI chats" backHref="/ai-chat" onBack={vi.fn()} />);
  expect(await screen.findByText('It ships in 2 days.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Delete chat' })).toBeNull();
  first.unmount();
  aiApi.chatSession.mockRejectedValueOnce(new Error('API 404'));
  wrap(<ChatTranscript id="2" timezone="UTC" canDelete={false} back="AI chats" backHref="/ai-chat" onBack={vi.fn()} />);
  expect((await screen.findByRole('alert')).textContent).toMatch(/not there/);
});

test('on a workspace key an admin sets the monthly spend limit in dollars, and empties it to remove it', async () => {
  aiApi.chatUsage.mockResolvedValue(usage({ answer_limit: null, day_answer_limit: null, spent_micro_usd: 0, spent_percent: null }));
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  const setCap = vi.fn().mockResolvedValue(undefined);
  const slot = { slot: 'chat', source: 'provider', provider_id: null, model: 'claude-haiku-5-5', effort: 'low', enabled: true, automatic: false, auto_parts: [],
    active: true, off_reason: null, consented_by: null, consented_at: null, updated_by: null, updated_at: null, monthly_cap_micro_usd: null } as const;
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} slot={{ ...slot, auto_parts: [] }} onSetCap={setCap} />);
  const field = await screen.findByRole('textbox', { name: 'Monthly spend limit (US dollars)' });
  fireEvent.change(field, { target: { value: 'ten' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save limit' }));
  expect(setCap).not.toHaveBeenCalled();
  expect(field.getAttribute('aria-invalid')).toBe('true');
  fireEvent.change(field, { target: { value: '12.5' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save limit' }));
  await waitFor(() => expect(setCap).toHaveBeenCalledWith(12_500_000));
  fireEvent.change(field, { target: { value: '' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save limit' }));
  await waitFor(() => expect(setCap).toHaveBeenLastCalledWith(null));
});

test('an admin tries the chat and reads the whole answer with what it read', async () => {
  aiApi.chatUsage.mockResolvedValue(usage());
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  aiApi.chatPreview.mockResolvedValue({ answer: 'Orders ship in 2 days [#12].', status: 'answered', sources: [{ number: 12, title: 'Shipping times', folder: 'Orders' }],
    redacted: false, whole: true, articles: 38, took_ms: 1400 });
  const slot = { slot: 'chat', source: 'server', provider_id: null, model: 'claude-haiku-5-5', effort: 'low', enabled: true, automatic: false, auto_parts: [],
    active: true, off_reason: null, consented_by: null, consented_at: null, updated_by: null, updated_at: null, monthly_cap_micro_usd: null } as const;
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} slot={{ ...slot, auto_parts: [] }} />);
  fireEvent.change(await screen.findByRole('textbox', { name: 'Ask as a customer would' }), { target: { value: 'When does it ship?' } });
  fireEvent.click(screen.getByRole('button', { name: 'Ask' }));
  expect(await screen.findByText('Orders ship in 2 days [1].')).toBeTruthy();
  expect(screen.getByText('1.4 s · whole help center (38 articles) · its cost counts toward the limit')).toBeTruthy();
  expect(aiApi.chatPreview).toHaveBeenCalledWith('When does it ship?', expect.any(String));
});

test('the inbox link finds the chat that handed off to a conversation and shows its transcript', async () => {
  aiApi.chatSessions.mockResolvedValue({ items: [summary({ status: 'handed_off', conversation_id: '44444444-4444-4444-8444-444444444444' })], next_cursor: null });
  aiApi.chatSession.mockResolvedValue(session());
  wrap(<AIChatSettingsRoute conversation="44444444-4444-4444-8444-444444444444" navigate={vi.fn()} />);
  expect(await screen.findByText('It ships in 2 days.')).toBeTruthy();
  expect(aiApi.chatSessions).toHaveBeenCalledWith({ conversationId: '44444444-4444-4444-8444-444444444444' });
  expect(aiApi.chatSession).toHaveBeenCalledWith('11111111-1111-4111-8111-111111111111');
});

test('a conversation whose chat is gone says so', async () => {
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  wrap(<AIChatSettingsRoute conversation="44444444-4444-4444-8444-444444444444" navigate={vi.fn()} />);
  expect((await screen.findByRole('alert')).textContent).toMatch(/This chat is not there/);
});

test('a transcript shows an answer’s markers as its sources’ numbers', async () => {
  aiApi.chatSession.mockResolvedValue(session({ transcript: [{ ...session().transcript[0], answer: 'It ships in 2 days [#12]. Ask us [#99].' }] }));
  wrap(<ChatTranscript id="11111111-1111-4111-8111-111111111111" timezone="UTC" canDelete={false} back="AI chat" backHref="/settings/ai?tab=chat" onBack={vi.fn()} />);
  expect(await screen.findByText('It ships in 2 days [1]. Ask us.')).toBeTruthy();
});
