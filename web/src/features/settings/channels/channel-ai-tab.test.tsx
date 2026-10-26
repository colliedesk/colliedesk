import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import type { AISettings, ChannelAI } from '../../../api/ai';
import type { ChannelDetail } from '../../../api/channels';
import { ChannelAITab } from './channel-ai-tab';

const aiApi = vi.hoisted(() => ({ channel: vi.fn(), setChannel: vi.fn(), settings: vi.fn(), readiness: vi.fn() }));
const intakeApi = vi.hoisted(() => ({ stats: vi.fn() }));
vi.mock('../../../api/ai', () => ({ aiApi }));
vi.mock('../../../api/intake', () => ({ intakeApi }));

const CHAT = '44444444-4444-4444-8444-444444444444';

const channel = { id: CHAT, type: 'loopback', name: 'Zalo HC Labs', enabled: true, status: { state: 'working', reason_code: null, message: null }, summary: '',
  capabilities: [], held_count: 0, unsent_count: 0, last_inbound_at: null, created_at: '2026-10-25T08:00:00Z', updated_at: '2026-10-25T08:00:00Z', config: {} } as unknown as ChannelDetail;

const levels = (intake: Partial<ChannelAI['scopes'][number]['slots'][number]> = {}): ChannelAI => ({
  channel_id: CHAT,
  scopes: [
    { scope: '', slots: [{ slot: 'triage', level: null, effective_level: 'off', source: 'workspace', off_reason: null, options: null },
      { slot: 'draft', level: null, effective_level: 'off', source: 'workspace', off_reason: null, options: null }] },
    { scope: 'group', slots: [{ slot: 'triage', level: null, effective_level: 'off', source: 'workspace', off_reason: null, options: null },
      { slot: 'draft', level: null, effective_level: 'off', source: 'workspace', off_reason: null, options: null },
      { slot: 'intake', level: 'suggest', effective_level: 'suggest', source: 'scope', off_reason: null, options: { create_threshold: 0.8 }, ...intake }] },
  ],
});

const slot = (name: 'triage' | 'draft' | 'intake') => ({ slot: name, source: 'provider', provider_id: '11111111-1111-4111-8111-111111111111', model: 'm', fallback_model: null,
  effort: 'low', enabled: true, default_level: 'off', automatic: false, auto_parts: [], active: true, off_reason: null, consented_by: null, consented_at: null, updated_by: null, updated_at: null });
const settings = (consents: string[] = []) => ({ slots: [slot('triage'), slot('draft'), slot('intake')], source_consents: consents.map((source_class) => ({ source_class,
  consented_by: null, consented_at: '2026-10-25T08:00:00Z' })) }) as unknown as AISettings;

const view = (isAdmin = true) => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
  <ChannelAITab channel={channel} isAdmin={isAdmin} /></QueryClientProvider>);

beforeEach(() => {
  vi.clearAllMocks();
  aiApi.channel.mockResolvedValue(levels());
  aiApi.settings.mockResolvedValue(settings());
  intakeApi.stats.mockResolvedValue({ days: 7, gatherings: 12, by_state: { created: 6, attached: 3, review: 2, ignored: 1 }, decided_by: { rules: 4, ai: 5, agent: 3 },
    fallback: 0, ai_decisions: 9, ai_corrected: 1 });
  aiApi.setChannel.mockImplementation((_id: string, _slot: string, input: { options: object }) => Promise.resolve(levels({ options: input.options })));
});

test('a chat channel has an Intake row only on its intake scope', async () => {
  view();
  expect(await screen.findByRole('combobox', { name: 'Intake · Groups' })).toHaveValue('suggest');
  expect(screen.queryByRole('combobox', { name: 'Intake · Whole channel' })).not.toBeInTheDocument();
});

test('intake at Suggest has its settings: defaults as hints, and Save sends what was changed with the level', async () => {
  view();
  const form = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  expect(within(form).getByLabelText('Open a conversation')).toHaveValue(0.8);
  expect(within(form).getByLabelText('Add to a conversation')).toHaveAttribute('placeholder', '0.90');
  expect(within(form).getByRole('checkbox', { name: /Never add on its own/ })).toBeChecked();
  fireEvent.change(within(form).getByLabelText('Add to a conversation'), { target: { value: '0.93' } });
  fireEvent.change(within(form).getByLabelText('Reopen a resolved conversation within (days)'), { target: { value: '3' } });
  fireEvent.click(within(form).getByRole('checkbox', { name: /Past corrections by people/ }));
  fireEvent.change(within(form).getByLabelText('Words that mark a secret'), { target: { value: 'mật khẩu\nOTP\n' } });
  fireEvent.change(within(form).getByLabelText('Instructions for this channel'), { target: { value: 'Lệnh "Báo lỗi" mở phiếu.' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save intake settings' }));
  await waitFor(() => expect(aiApi.setChannel).toHaveBeenCalledWith(CHAT, 'intake', { scope: 'group', level: 'suggest', options: {
    create_threshold: 0.8, attach_threshold: 0.93, reopen_window_days: 3, sources: { corrections: true }, redact_keywords: ['mật khẩu', 'OTP'], instructions: 'Lệnh "Báo lỗi" mở phiếu.',
  } }));
  // The form stays as it was saved, and says so.
  const after = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  expect(await within(after).findByRole('status')).toHaveTextContent('Saved');
  expect(within(after).getByLabelText('Add to a conversation')).toHaveValue(0.93);
});

test('a source that needs a consent says so and links Built-in AI; with the consent it can be turned on', async () => {
  const { unmount } = view();
  const form = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  expect(within(form).getByRole('checkbox', { name: /Images the customer sent/ })).toBeDisabled();
  expect(within(form).getAllByText('Needs consent in Built-in AI')).toHaveLength(2);
  unmount();
  aiApi.settings.mockResolvedValue(settings(['images']));
  view();
  const allowed = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  await waitFor(() => expect(within(allowed).getByRole('checkbox', { name: /Images the customer sent/ })).toBeEnabled());
});

test('a value out of range is refused by the server and said under the form; agents only read', async () => {
  aiApi.setChannel.mockRejectedValue(Object.assign(new Error('create_threshold must be between 0.5 and 0.99'), { name: 'InvalidRequestError' }));
  const { unmount } = view();
  const form = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  fireEvent.change(within(form).getByLabelText('Open a conversation'), { target: { value: '1.5' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save intake settings' }));
  expect(await within(form).findByRole('alert')).toBeInTheDocument();
  unmount();
  view(false);
  const read = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  expect(within(read).getByLabelText('Open a conversation')).toBeDisabled();
  expect(within(read).queryByRole('button', { name: 'Save intake settings' })).not.toBeInTheDocument();
});

test('intake off on a scope has no settings to show', async () => {
  aiApi.channel.mockResolvedValue(levels({ level: 'off', effective_level: 'off', options: null }));
  view();
  await screen.findByRole('combobox', { name: 'Intake · Groups' });
  expect(screen.queryByRole('form', { name: 'Intake settings · Groups' })).not.toBeInTheDocument();
});

test('a chat channel with intake on says how its last 7 days went and links its cards in Review', async () => {
  view();
  const block = await screen.findByRole('region', { name: 'Last 7 days' });
  await waitFor(() => expect(block).toHaveTextContent('12 cards · AI decided 5 by itself · people changed 1 of 9 AI decisions · 2 waiting'));
  expect(intakeApi.stats).toHaveBeenCalledWith(CHAT);
  expect(within(block).getByRole('link', { name: 'Open Review' })).toHaveAttribute('href', `/review?channel=${CHAT}&scope=group`);
});

test('saving the settings keeps what this form does not show, since the server takes the options whole', async () => {
  aiApi.channel.mockResolvedValue(levels({ options: { create_threshold: 0.8, gate_override: ['create'], gate_override_reason: 'Checked 40 cards by hand' } as never }));
  view();
  const form = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  fireEvent.change(within(form).getByLabelText('Reopen a resolved conversation within (days)'), { target: { value: '3' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save intake settings' }));
  await waitFor(() => expect(aiApi.setChannel).toHaveBeenCalledWith(CHAT, 'intake', { scope: 'group', level: 'suggest', options: {
    create_threshold: 0.8, reopen_window_days: 3, gate_override: ['create'], gate_override_reason: 'Checked 40 cards by hand',
  } }));
});

const counts = (labels: number, correct: number, wrong = labels - correct, severe = 0) => ({ labels, correct, wrong, severe });
const readiness = () => ({ channel_id: CHAT, scope: 'group', model: 'claude-haiku-5-5', prompt_version: 'v1',
  actions: [
    { action: 'create', gate: 'open', ...counts(40, 38), needed: 30, accuracy_needed: 0.9, missing: [], live: counts(40, 38), evaluation: null },
    { action: 'attach', gate: 'closed', ...counts(12, 12), needed: 30, accuracy_needed: 0.95, missing: ['labels'], live: counts(12, 12), evaluation: null },
    { action: 'ignore', gate: 'closed', ...counts(40, 33), needed: 30, accuracy_needed: 0.95, missing: ['accuracy'], live: counts(40, 33), evaluation: null },
  ],
  applied: { decisions: 0, corrected: 0, correction_rate: null },
  estimate: { days: 30, cards: 420, ai_calls: 180, cost_per_call: { micro_usd: 1200, provider_cost: null } } });

test('raising intake to Auto asks first: what each action does alone, what is not ready yet, and an estimate of 30 days', async () => {
  aiApi.readiness.mockResolvedValue(readiness());
  view();
  fireEvent.change(await screen.findByRole('combobox', { name: 'Intake · Groups' }), { target: { value: 'auto' } });
  const dialog = await screen.findByRole('dialog', { name: 'Let AI decide on its own · Groups' });
  expect(aiApi.readiness).toHaveBeenCalledWith(CHAT, 'group');
  expect(aiApi.setChannel).not.toHaveBeenCalled();
  const rows = (await within(dialog).findAllByRole('listitem')).map((row) => row.textContent);
  expect(rows[0]).toContain('Open a conversation: ready · 38 of 40 right');
  expect(rows[1]).toContain('Add to a conversation: stays a suggestion until 30 decisions are labelled (12 of 30)');
  expect(rows[2]).toContain('Set aside: stays a suggestion: 7 wrong of 40 (needs 95% right)');
  expect(dialog).toHaveTextContent('About 180 AI calls in 30 days, about $0.22');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Turn on Auto' }));
  await waitFor(() => expect(aiApi.setChannel).toHaveBeenCalledWith(CHAT, 'intake', { scope: 'group', level: 'auto', options: { create_threshold: 0.8 } }));
});

test('an admin may let a closed action decide alone with a reason, sent with the level in one change', async () => {
  aiApi.readiness.mockResolvedValue(readiness());
  view();
  fireEvent.change(await screen.findByRole('combobox', { name: 'Intake · Groups' }), { target: { value: 'auto' } });
  const dialog = await screen.findByRole('dialog', { name: 'Let AI decide on its own · Groups' });
  fireEvent.click(await within(dialog).findByRole('checkbox', { name: /Let it add to a conversation alone anyway/ }));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Turn on Auto' }));
  expect(within(dialog).getByRole('alert')).toHaveTextContent('Say why');
  expect(aiApi.setChannel).not.toHaveBeenCalled();
  fireEvent.change(within(dialog).getByLabelText('Why AI may do this before it is ready'), { target: { value: 'We checked 50 by hand' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Turn on Auto' }));
  await waitFor(() => expect(aiApi.setChannel).toHaveBeenCalledWith(CHAT, 'intake', { scope: 'group', level: 'auto',
    options: { create_threshold: 0.8, gate_override: ['attach'], gate_override_reason: 'We checked 50 by hand' } }));
});

test('readiness that could not be loaded says so and Auto cannot be turned on blind; Cancel changes nothing', async () => {
  aiApi.readiness.mockRejectedValue(new Error('API 500'));
  view();
  fireEvent.change(await screen.findByRole('combobox', { name: 'Intake · Groups' }), { target: { value: 'auto' } });
  const dialog = await screen.findByRole('dialog', { name: 'Let AI decide on its own · Groups' });
  expect(await within(dialog).findByText(/could not be loaded/)).toBeInTheDocument();
  expect(within(dialog).getByRole('button', { name: 'Turn on Auto' })).toBeDisabled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(aiApi.setChannel).not.toHaveBeenCalled();
});

test('lowering intake from Auto asks nothing', async () => {
  aiApi.channel.mockResolvedValue(levels({ level: 'auto', effective_level: 'auto' }));
  view();
  fireEvent.change(await screen.findByRole('combobox', { name: 'Intake · Groups' }), { target: { value: 'suggest' } });
  await waitFor(() => expect(aiApi.setChannel).toHaveBeenCalledWith(CHAT, 'intake', { scope: 'group', level: 'suggest' }));
  expect(aiApi.readiness).not.toHaveBeenCalled();
});

test('masking patterns: an admin adds one with what it stands for, removes one, and the server refuses a bad one under the form', async () => {
  aiApi.channel.mockResolvedValue(levels({ options: { create_threshold: 0.8, redact_patterns: [{ pattern: 'HD-\\d{6}', mask: 'id_number' }] } }));
  view();
  const form = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  const table = within(form).getByRole('group', { name: 'Patterns to mask' });
  expect(within(table).getByLabelText('Pattern 1')).toHaveValue('HD-\\d{6}');
  expect(within(table).getByLabelText('Shown as, pattern 1')).toHaveValue('id_number');
  fireEvent.click(within(table).getByRole('button', { name: 'Add a pattern' }));
  fireEvent.change(within(table).getByLabelText('Pattern 2'), { target: { value: '\\b0\\d{9}\\b' } });
  fireEvent.change(within(table).getByLabelText('Shown as, pattern 2'), { target: { value: 'phone' } });
  fireEvent.click(within(table).getByRole('button', { name: 'Remove pattern 1' }));
  fireEvent.click(within(form).getByRole('button', { name: 'Save intake settings' }));
  await waitFor(() => expect(aiApi.setChannel).toHaveBeenCalledWith(CHAT, 'intake', { scope: 'group', level: 'suggest', options: {
    create_threshold: 0.8, redact_patterns: [{ pattern: '\\b0\\d{9}\\b', mask: 'phone' }],
  } }));
});

test('Auto that the server refuses says why on the tab, and the level stays as it was', async () => {
  aiApi.readiness.mockResolvedValue(readiness());
  aiApi.setChannel.mockRejectedValue(new Error('API 500'));
  view();
  fireEvent.change(await screen.findByRole('combobox', { name: 'Intake · Groups' }), { target: { value: 'auto' } });
  const dialog = await screen.findByRole('dialog', { name: 'Let AI decide on its own · Groups' });
  fireEvent.click(await within(dialog).findByRole('button', { name: 'Turn on Auto' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('The action failed. Try again.');
  expect(screen.getByRole('combobox', { name: 'Intake · Groups' })).toHaveValue('suggest');
});

test('settings the server refuses are said once, under the form', async () => {
  aiApi.setChannel.mockRejectedValue(new Error('API 500'));
  view();
  const form = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  fireEvent.click(within(form).getByRole('button', { name: 'Save intake settings' }));
  await within(form).findByRole('alert');
  expect(screen.getAllByRole('alert')).toHaveLength(1);
});

test('a scope the server took back to Suggest says when and why, with Turn Auto back on asking first', async () => {
  aiApi.readiness.mockResolvedValue(readiness());
  aiApi.channel.mockResolvedValue(levels({ lowered: { at: '2026-10-24T09:00:00Z', reason: 'model_changed' } }));
  view();
  const note = await screen.findByText(/Auto was lowered to Suggest on Oct 24, 2026: intake moved to another model/);
  fireEvent.click(within(note.closest('p') as HTMLElement).getByRole('button', { name: 'Turn Auto back on' }));
  expect(await screen.findByRole('dialog', { name: 'Let AI decide on its own · Groups' })).toBeInTheDocument();
  expect(aiApi.setChannel).not.toHaveBeenCalled();
});

test('a scope lowered because the provider changed says that', async () => {
  aiApi.channel.mockResolvedValue(levels({ lowered: { at: '2026-10-24T09:00:00Z', reason: 'recipient_changed' } }));
  view();
  expect(await screen.findByText(/Auto was lowered to Suggest on Oct 24, 2026: intake moved to another provider/)).toBeInTheDocument();
});

test('the estimate is in the provider\'s own credits, or a share of the included AI on Colliedesk AI', async () => {
  aiApi.readiness.mockResolvedValue({ ...readiness(), estimate: { days: 30, cards: 420, ai_calls: 180, cost_per_call: { micro_usd: null, provider_cost: 0.0004 } } });
  const { unmount } = view();
  fireEvent.change(await screen.findByRole('combobox', { name: 'Intake · Groups' }), { target: { value: 'auto' } });
  expect(await screen.findByText(/About 180 AI calls in 30 days, about 0.072 provider credits/)).toBeInTheDocument();
  unmount();
  aiApi.readiness.mockResolvedValue(readiness());
  aiApi.settings.mockResolvedValue({ ...settings(), slots: [slot('triage'), slot('draft'), { ...slot('intake'), source: 'server', provider_id: null }],
    allowance: { limit_micro_usd: 5_000_000, used_micro_usd: 0, period_start: '2026-10-01T00:00:00Z', period_end: '2026-11-01T00:00:00Z' } } as unknown as AISettings);
  view();
  fireEvent.change(await screen.findByRole('combobox', { name: 'Intake · Groups' }), { target: { value: 'auto' } });
  expect(await screen.findByText(/about \$0.22, 4% of the AI included each month/)).toBeInTheDocument();
});

test('a scope that follows the default shows its settings read-only and says how to set its own', async () => {
  aiApi.channel.mockResolvedValue(levels({ level: null, effective_level: 'suggest', source: 'workspace', options: { create_threshold: 0.85 } }));
  aiApi.settings.mockResolvedValue({ ...settings(), slots: [slot('triage'), slot('draft'), { ...slot('intake'), default_level: 'suggest' }] } as unknown as AISettings);
  view();
  const form = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  expect(within(form).getByText('These follow the default. Pick Suggest or Auto for Groups above to set its own.')).toBeInTheDocument();
  expect(within(form).getByLabelText('Open a conversation')).toBeDisabled();
  expect(within(form).queryByRole('button', { name: 'Save intake settings' })).not.toBeInTheDocument();
});

test('an age set to seconds that are not whole minutes is kept when other fields are saved', async () => {
  aiApi.channel.mockResolvedValue(levels({ options: { auto_max_age_seconds: 90 } }));
  view();
  const form = await screen.findByRole('form', { name: 'Intake settings · Groups' });
  fireEvent.change(within(form).getByLabelText('Reopen a resolved conversation within (days)'), { target: { value: '3' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Save intake settings' }));
  await waitFor(() => expect(aiApi.setChannel).toHaveBeenCalledWith(CHAT, 'intake', { scope: 'group', level: 'suggest', options: { auto_max_age_seconds: 90, reopen_window_days: 3 } }));
});
