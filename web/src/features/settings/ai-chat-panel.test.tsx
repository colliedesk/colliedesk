import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { AIChatSession, AIChatSummary, AIChatUsage } from '../../api/ai';
import { SettingsEditsProvider } from './settings-edits';
import { AIChatPanel, ChatTranscript } from './ai-chat-panel';
import { AIPage } from './ai-page';
import { settings, slot } from './ai-fixtures';
import { permissionsOf, rolesOf } from '../../test-roles';
import { AIChatSettingsRoute } from './ai-chat-route';

const aiApi = vi.hoisted(() => ({ chatUsage: vi.fn(), chatSessions: vi.fn(), chatSession: vi.fn(), deleteChatSession: vi.fn(), chatPreview: vi.fn() }));
vi.mock('../../api/ai', () => ({ aiApi }));
vi.mock('../../api/client', async (actual) => ({ ...(await actual<object>()), api: {
  me: () => Promise.resolve({ agent: { roles: rolesOf('admin') }, timezone: 'UTC' }), config: () => Promise.resolve({ billing: { enabled: false } }) } }));
vi.mock('./settings-nav', () => ({ SettingsNav: () => null }));

const summary = (change: Partial<AIChatSummary> = {}): AIChatSummary => ({
  id: '11111111-1111-4111-8111-111111111111', channel_id: '22222222-2222-4222-8222-222222222222', channel_name: 'Help chat', surface: 'help_center',
  status: 'open', turns: 2, first_question: 'Where is my order?', not_helpful: false, conversation_id: null,
  created_at: '2026-10-28T10:00:00Z', last_turn_at: '2026-10-28T11:00:00Z', ended_at: null, kept_until: '2027-01-26T11:00:00Z', ...change,
});
const usage = (change: Partial<AIChatUsage> = {}): AIChatUsage => ({
  month: '2026-10', answers: 413, answers_today: 18, answer_limit: 750, day_answer_limit: 75, spent_micro_usd: null, limit_micro_usd: null,
  spent_percent: 61, unpriced_model: null, chats: 128, handed_off: 9, rated_helpful: 34, rated: 41, articles: 38, resets_at: '2026-11-01T00:00:00Z', ...change,
});

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><SettingsEditsProvider>{children}</SettingsEditsProvider></QueryClientProvider>);
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
  const slot = { slot: 'chat', source: 'provider', provider_id: null, model: 'claude-haiku-5-5', fallback_model: null, effort: 'low', enabled: true, default_level: 'off' as const, automatic: false, auto_parts: [],
    active: true, off_reason: null, consented_by: null, consented_at: null, updated_by: null, updated_at: null, monthly_cap_micro_usd: null } as const;
  const view = wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} slot={{ ...slot, auto_parts: [] }} onSetCap={setCap} />);
  const field = await screen.findByRole('textbox', { name: 'Monthly spend limit (US dollars)' });
  fireEvent.change(field, { target: { value: 'ten' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(setCap).not.toHaveBeenCalled();
  expect(field.getAttribute('aria-invalid')).toBe('true');
  fireEvent.change(field, { target: { value: '12.5' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(setCap).toHaveBeenCalledWith(12_500_000));
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull());
  // The limit is stored now: the page reads the slot again, and emptying the field removes it.
  view.unmount();
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} slot={{ ...slot, monthly_cap_micro_usd: 12_500_000, auto_parts: [] }} onSetCap={setCap} />);
  fireEvent.change(await screen.findByRole('textbox', { name: 'Monthly spend limit (US dollars)' }), { target: { value: '' } });
  fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
  await waitFor(() => expect(setCap).toHaveBeenLastCalledWith(null));
});

test('a model the server has no price for says so instead of $0.00, and offers no limit in dollars', async () => {
  aiApi.chatUsage.mockResolvedValue(usage({ answers: 21, answer_limit: null, day_answer_limit: null, spent_micro_usd: null, spent_percent: null, unpriced_model: 'claude-haiku-5-5' }));
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  const slot = { slot: 'chat', source: 'server', provider_id: null, model: 'claude-haiku-5-5', fallback_model: null, effort: 'low', enabled: true, default_level: 'off' as const, automatic: false, auto_parts: [],
    active: true, off_reason: null, consented_by: null, consented_at: null, updated_by: null, updated_at: null, monthly_cap_micro_usd: null } as const;
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} slot={{ ...slot, auto_parts: [] }} onSetCap={vi.fn()} />);
  expect(await screen.findByText('Price unknown')).toBeTruthy();
  expect(screen.queryByText(/\$0\.00/)).toBeNull();
  expect(screen.getByText(/no price for claude-haiku-5-5/)).toBeTruthy();
  expect(screen.getByText(/no limit in dollars can be set/)).toBeTruthy();
  expect(screen.queryByRole('textbox', { name: 'Monthly spend limit (US dollars)' })).toBeNull();
});

test('without an AI provider of the server, an unpriced model says no price can be added here, not to edit AI_SERVER_PRICES', async () => {
  aiApi.chatUsage.mockResolvedValue(usage({ answer_limit: null, day_answer_limit: null, spent_micro_usd: null, spent_percent: null, unpriced_model: 'claude-haiku-5-5' }));
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} serverPrices={false} />);
  expect(await screen.findByText('Price unknown')).toBeTruthy();
  expect(screen.getByText(/no AI provider of its own/)).toBeTruthy();
  expect(screen.queryByText(/AI_SERVER_PRICES/)).toBeNull();
});

test('an admin tries the chat and reads the whole answer with what it read', async () => {
  aiApi.chatUsage.mockResolvedValue(usage());
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  aiApi.chatPreview.mockResolvedValue({ answer: 'Orders ship in 2 days [#12].', status: 'answered', sources: [{ number: 12, title: 'Shipping times', folder: 'Orders' }],
    redacted: false, whole: true, articles: 38, took_ms: 1400 });
  const slot = { slot: 'chat', source: 'server', provider_id: null, model: 'claude-haiku-5-5', fallback_model: null, effort: 'low', enabled: true, default_level: 'off' as const, automatic: false, auto_parts: [],
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

// A row of the AI chat keeps its label and one line of 60 characters or less; what it explained below that line is the
// accessible description of the control it is about.
const chatSlot = { slot: 'chat', source: 'provider', provider_id: null, model: 'claude-haiku-5-5', fallback_model: null, effort: 'low', enabled: true, default_level: 'off' as const, automatic: false, auto_parts: [],
  active: true, off_reason: null, consented_by: null, consented_at: null, updated_by: null, updated_at: null, monthly_cap_micro_usd: null } as const;

test('the spend limit says "Empty means no limit." and what happens past it sits in the tip of its label', async () => {
  aiApi.chatUsage.mockResolvedValue(usage({ answer_limit: null, day_answer_limit: null, spent_micro_usd: 0, spent_percent: null }));
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} slot={{ ...chatSlot, auto_parts: [] }} onSetCap={vi.fn()} />);
  const field = await screen.findByRole('textbox', { name: 'Monthly spend limit (US dollars)' });
  expect(screen.getByText('Empty means no limit.')).toBeVisible();
  expect(field).toHaveAccessibleDescription(/Empty means no limit\..*customers see related articles and Talk to a person until the next month \(UTC\)/);
  expect(screen.getByRole('button', { name: 'About Monthly spend limit (US dollars)' })).toBeInTheDocument();
});

test('Try it keeps no paragraph under its heading: who sees the question and what it costs are the heading’s tip', async () => {
  aiApi.chatUsage.mockResolvedValue(usage());
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} slot={{ ...chatSlot, auto_parts: [] }} />);
  const tip = await screen.findByRole('button', { name: 'About Try it' });
  expect(tip).toHaveAccessibleDescription(/Not shown to customers and not one of the month’s answers; what it costs counts toward the AI chat limit\./);
});

test('an empty list of chats says what shows here in one line', async () => {
  aiApi.chatUsage.mockResolvedValue(usage());
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  wrap(<AIChatPanel timezone="UTC" onOpen={vi.fn()} />);
  expect(await screen.findByText('No chats yet. Chats from your help center show here.')).toBeVisible();
});

test('a chat cut short says so in one line, and the date it was cut at is in the tip', async () => {
  aiApi.chatSession.mockResolvedValue(session({ history_cut_at: '2026-10-28T11:00:00Z' }));
  wrap(<ChatTranscript id="11111111-1111-4111-8111-111111111111" timezone="UTC" canDelete back="AI chat" backHref="/settings/ai?tab=chat" onBack={vi.fn()} />);
  expect(await screen.findByText('Older questions are no longer read by the AI.')).toBeVisible();
  expect(screen.getByRole('button', { name: 'About Older questions' })).toHaveAccessibleDescription(/Questions before .+ no longer fit what the AI reads of this chat\./);
});

test('before a chat is deleted for good, the dialog still says what goes and what stays', async () => {
  aiApi.chatSession.mockResolvedValue(session());
  wrap(<ChatTranscript id="11111111-1111-4111-8111-111111111111" timezone="UTC" canDelete back="AI chat" backHref="/settings/ai?tab=chat" onBack={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Delete chat' }));
  const dialog = await screen.findByRole('alertdialog');
  expect(within(dialog).getByText('The transcript is removed for good. The conversation it opened keeps its copy.')).toBeVisible();
});


test('a spend limit typed on the Chat tab is kept when another tab is opened and comes back: the text, the bar and Save all survive', async () => {
  aiApi.chatUsage.mockResolvedValue(usage({ answer_limit: null, day_answer_limit: null, spent_micro_usd: 0, spent_percent: null }));
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  const setCap = vi.fn().mockResolvedValue(undefined);
  const chat = slot({ slot: 'chat' as never, source: 'provider', model: 'claude-haiku-5-5', enabled: true, active: true, monthly_cap_micro_usd: null } as never);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><AIPage isAdmin workspaceName="Northwind" timezone="UTC" showConsent={false} settings={settings({ slots: [chat] })} onSetChatCap={setCap}
    shown={{ general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: false, ai: true }}
    onSetSlot={vi.fn()} onConsent={vi.fn()} onDismissConsent={vi.fn()} onCheck={vi.fn()} onAdd={vi.fn()} onListModels={vi.fn()} onSetKey={vi.fn()} onRemove={vi.fn()} onSetAutomatic={vi.fn()} onUsageMonth={vi.fn()} /></QueryClientProvider>);
  fireEvent.click(screen.getByRole('tab', { name: 'AI chat' }));
  const field = await screen.findByRole('textbox', { name: /Monthly spend limit/ });
  fireEvent.change(field, { target: { value: '12.5' } });
  fireEvent.click(screen.getByRole('tab', { name: 'History' }));
  expect(screen.queryByRole('textbox', { name: /Monthly spend limit/ })).toBeNull();
  expect(screen.getByRole('region', { name: 'Unsaved changes' })).toHaveTextContent('Monthly spend limit');
  fireEvent.click(screen.getByRole('tab', { name: 'AI chat' }));
  expect(await screen.findByRole('textbox', { name: /Monthly spend limit/ })).toHaveValue('12.5');
  fireEvent.click(screen.getByRole('tab', { name: 'History' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(setCap).toHaveBeenCalledWith(12_500_000));
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Unsaved changes' })).toBeNull());
});


test('a spend limit that cannot be saved says why on the bar even when the Chat tab is not the one shown', async () => {
  aiApi.chatUsage.mockResolvedValue(usage({ answer_limit: null, day_answer_limit: null, spent_micro_usd: 0, spent_percent: null }));
  aiApi.chatSessions.mockResolvedValue({ items: [], next_cursor: null });
  const chat = slot({ slot: 'chat' as never, source: 'provider', model: 'claude-haiku-5-5', enabled: true, active: true, monthly_cap_micro_usd: null } as never);
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><AIPage isAdmin workspaceName="Northwind" timezone="UTC" showConsent={false} settings={settings({ slots: [chat] })} onSetChatCap={vi.fn()}
    shown={{ general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: false, ai: true }}
    onSetSlot={vi.fn()} onConsent={vi.fn()} onDismissConsent={vi.fn()} onCheck={vi.fn()} onAdd={vi.fn()} onListModels={vi.fn()} onSetKey={vi.fn()} onRemove={vi.fn()} onSetAutomatic={vi.fn()} onUsageMonth={vi.fn()} /></QueryClientProvider>);
  fireEvent.click(screen.getByRole('tab', { name: 'AI chat' }));
  fireEvent.change(await screen.findByRole('textbox', { name: /Monthly spend limit/ }), { target: { value: 'abc' } });
  fireEvent.click(screen.getByRole('tab', { name: 'History' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await within(screen.getByRole('region', { name: 'Unsaved changes' })).findByText(/Monthly spend limit.*: .*amount above 0/i)).toBeInTheDocument();
});
