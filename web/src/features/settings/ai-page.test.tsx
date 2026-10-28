import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { ConflictError } from '../../api/client';
import type { AIProvider, AISettings, AISlot } from '../../api/ai';
import { AIPage } from './ai-page';
import type { AIPageProps } from './ai-page';
import { refusal } from './ai-route';

const providerID = '11111111-1111-4111-8111-111111111111';

function slot(change: Partial<AISlot> = {}): AISlot {
  return { slot: 'triage', source: 'none', provider_id: null, model: null, effort: 'low', enabled: false, automatic: false, auto_parts: [], active: false, off_reason: null,
    consented_by: null, consented_at: null, updated_by: null, updated_at: null, ...change };
}

function provider(change: Partial<AIProvider> = {}): AIProvider {
  return { id: providerID, kind: 'anthropic', label: 'Team key', base_url: null, auth_kind: 'api_key', key_hint: '7Qx2',
    models: ['claude-sonnet-5-5', 'claude-haiku-5-5'], check_status: 'ok', check_error_code: null, checked_at: '2026-10-22T08:00:00Z',
    created_by: null, created_at: '2026-10-22T08:00:00Z', updated_at: '2026-10-22T08:00:00Z', used_by: [], ...change };
}

function settings(change: Partial<AISettings> = {}): AISettings {
  return { server: null, providers: [], slots: [slot(), slot({ slot: 'draft' })], events: [], provider_limit: 5, can_store_keys: true,
    allow_private_base_urls: false, automatic: { per_day: 300, per_minute: 30, used_today: 0, paused_until: null, draft_per_day: 100, drafts_today: 0, drafts_paused_until: null, drafts_reserved_until: null }, allowance: null,
    auto_triage: { applied: 0, undone: 0 }, auto_draft_estimate: { customer_messages: 0, draft_cost_micro_usd: null }, ...change };
}

function page(props: Partial<AIPageProps> = {}) {
  const handlers = {
    onSetSlot: vi.fn(), onConsent: vi.fn(), onDismissConsent: vi.fn(), onCheck: vi.fn(),
    onAdd: vi.fn().mockResolvedValue(undefined), onListModels: vi.fn().mockResolvedValue([]), onSetKey: vi.fn().mockResolvedValue(undefined), onRemove: vi.fn().mockResolvedValue(undefined),
    onSetAutomatic: vi.fn().mockResolvedValue(undefined), onUsageMonth: vi.fn(),
  };
  render(<AIPage isAdmin workspaceName="Northwind" timezone="UTC" shown={{ general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: false, ai: true }}
    showConsent={false} settings={settings()} {...handlers} {...props} />);
  return handlers;
}

const server = { kind: 'anthropic' as const, label: 'Colliedesk AI', models: { triage: ['claude-haiku-5-5'], draft: ['claude-sonnet-5-5', 'claude-opus-5-5'] } };

test('a server without any provider explains the two ways to start, and every feature is off', () => {
  page();
  expect(screen.getByRole('heading', { name: 'No AI provider on this server yet' })).toBeInTheDocument();
  expect(screen.getByText(/set up by whoever runs it/)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Setup guide' })).toHaveAttribute('href', expect.stringContaining('environment-variables.md#ai-provider'));
  expect(screen.queryByText(/AI_SERVER_PROVIDER/)).toBeNull();
  for (const name of ['Triage suggestions on', 'Reply drafts on']) expect(screen.getByRole('switch', { name })).toBeDisabled();
  expect(screen.getAllByText('Add a provider first, then turn this on.')).toHaveLength(2);
});

test('an agent reads the settings and cannot change them', () => {
  page({ isAdmin: false, settings: settings({ server, slots: [slot({ source: 'server', model: 'claude-haiku-5-5', enabled: true, active: true }), slot({ slot: 'draft' })] }) });
  expect(screen.getByText('Only admins can change AI settings.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Add provider/ })).not.toBeInTheDocument();
  expect(screen.getByRole('switch', { name: 'Triage suggestions on' })).toBeChecked();
  expect(screen.getByRole('switch', { name: 'Triage suggestions on' })).toBeDisabled();
  expect(screen.getByRole('combobox', { name: 'Triage suggestions provider' })).toBeDisabled();
});

test('the consent banner turns both features on with the server provider, or goes away for now', () => {
  const { onConsent, onDismissConsent } = page({ showConsent: true, settings: settings({ server }) });
  const banner = screen.getByRole('region', { name: 'Turn on AI for Northwind' });
  expect(banner).toHaveTextContent('go to Colliedesk AI, which runs Anthropic’s Claude models (triage claude-haiku-5-5 · drafts claude-sonnet-5-5)');
  expect(banner).toHaveTextContent('Private notes are never sent');
  fireEvent.click(within(banner).getByRole('button', { name: 'Turn on suggestions and drafts' }));
  expect(onConsent).toHaveBeenCalled();
  fireEvent.click(within(banner).getByRole('button', { name: 'Not now' }));
  expect(onDismissConsent).toHaveBeenCalled();
});

test('a feature is switched on with its provider and model, and moving it sends the new provider', () => {
  const { onSetSlot } = page({ settings: settings({ server, providers: [provider()], slots: [slot({ source: 'server', model: 'claude-haiku-5-5' }), slot({ slot: 'draft' })] }) });
  fireEvent.click(screen.getByRole('switch', { name: 'Triage suggestions on' }));
  expect(onSetSlot).toHaveBeenLastCalledWith('triage', { source: 'server', model: 'claude-haiku-5-5', effort: 'low', enabled: true });
  fireEvent.change(screen.getByRole('combobox', { name: 'Reply drafts provider' }), { target: { value: providerID } });
  expect(onSetSlot).toHaveBeenLastCalledWith('draft', { source: 'provider', provider_id: providerID, effort: 'low', enabled: false });
});

test('a feature on a server provider the operator removed says it stopped and why', () => {
  page({ settings: settings({ slots: [slot({ source: 'server', model: 'claude-haiku-5-5', enabled: true, active: false, off_reason: 'server_provider_missing' }), slot({ slot: 'draft' })] }) });
  expect(screen.getByText('Stopped')).toBeInTheDocument();
  expect(screen.getByText(/removed the server provider/)).toBeInTheDocument();
});

test('providers show the key only by its last characters, and why a check failed', () => {
  const { onCheck } = page({ settings: settings({ providers: [provider({ used_by: ['triage'] }), provider({ id: '22222222-2222-4222-8222-222222222222', label: 'OpenRouter', kind: 'openai_compatible',
    base_url: 'https://openrouter.ai/api/v1', key_hint: 'a91f', check_status: 'failing', check_error_code: 'auth' })] }) });
  fireEvent.click(screen.getByRole('tab', { name: /Providers/ }));
  expect(screen.getByText('key ••••7Qx2')).toBeInTheDocument();
  expect(screen.getByText(/Refused the key \(401\) at the last check/)).toBeInTheDocument();
  expect(screen.getByText('3 more providers you can add.')).toBeInTheDocument();
  fireEvent.click(screen.getAllByRole('button', { name: 'Check' })[0]);
  expect(onCheck).toHaveBeenCalledWith(providerID);
});

test('adding a provider checks the fields, sends them, and shows a refusal under the field it is about', async () => {
  const { onAdd } = page();
  fireEvent.click(screen.getAllByRole('button', { name: /Add provider/ })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add a provider' });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Check and save' }));
  expect(within(dialog).getByText('Give the provider a name.')).toBeInTheDocument();
  expect(onAdd).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('radio', { name: /Other or self-hosted/ }));
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Office' } });
  fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://10.0.0.12/v1' } });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'sk-1' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Type model names instead' }));
  fireEvent.change(within(dialog).getByLabelText('Model names'), { target: { value: 'llama, qwen ,' } });
  expect(within(dialog).getByLabelText('API key')).toHaveAttribute('type', 'password');
  onAdd.mockResolvedValueOnce({ field: 'base_url', text: 'This address is on a private network.' });
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Check and save' })); });
  expect(onAdd).toHaveBeenCalledWith({ kind: 'openai_compatible', label: 'Office', base_url: 'https://10.0.0.12/v1', api_key: 'sk-1', models: ['llama', 'qwen'] });
  expect(within(dialog).getByLabelText('Base URL')).toHaveAttribute('aria-invalid', 'true');
  expect(within(dialog).getByText('This address is on a private network.')).toBeInTheDocument();
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Check and save' })); });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('the model search finds a model whatever punctuation, spacing, case or word order is typed', async () => {
  const { onListModels } = page();
  const all = ['gpt-5-mini', 'gpt-5.4-nano', 'gpt-6-sol', 'o3', 'claude-sonnet-4-6', 'claude-opus-4-6', 'gpt-5.6-terra', 'gpt-5', 'gpt-6.1-sol', 'gpt-5.5', 'gpt-6-luna', 'gpt-5.4'];
  onListModels.mockResolvedValueOnce(all);
  fireEvent.click(screen.getAllByRole('button', { name: /Add provider/ })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add a provider' });
  fireEvent.click(within(dialog).getByText('OpenAI', { exact: true }));
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'sk-1' } });
  await act(async () => { fireEvent.blur(within(dialog).getByLabelText('API key')); });
  const models = within(dialog).getByRole('group', { name: 'Models' });
  const search = within(models).getByRole('searchbox', { name: 'Search models' });
  const shown = (query: string) => {
    fireEvent.change(search, { target: { value: query } });
    return within(models).queryAllByRole('checkbox').map((box) => box.closest('label')?.textContent?.replace('Default', ''));
  };
  expect(shown('gpt5mini')).toEqual(['gpt-5-mini']);
  expect(shown('GPT 5 Mini')).toEqual(['gpt-5-mini']);
  expect(shown('mini gpt5')).toEqual(['gpt-5-mini']);
  expect(shown('gpt 5.4 nano')).toEqual(['gpt-5.4-nano']);
  expect(shown('sonnet 4.6')).toEqual(['claude-sonnet-4-6']);
  expect(shown('6.1sol')).toEqual(['gpt-6.1-sol']);
  expect(shown('  ')).toHaveLength(all.length);
  expect(shown('gemini')).toEqual([]);
});

test('the models a key may use are loaded and ticked instead of typed; the first ticked is the default', async () => {
  const { onAdd, onListModels } = page();
  onListModels.mockResolvedValueOnce(['claude-opus-5-5', 'claude-sonnet-5-5', 'claude-haiku-5-5']);
  fireEvent.click(screen.getAllByRole('button', { name: /Add provider/ })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add a provider' });
  expect(within(dialog).getByRole('button', { name: 'Load models' })).toBeDisabled();
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Team key' } });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'sk-ant-1' } });
  // Leaving the key loads the list, so the next step is to pick.
  await act(async () => { fireEvent.blur(within(dialog).getByLabelText('API key')); });
  expect(onListModels).toHaveBeenCalledWith({ kind: 'anthropic', api_key: 'sk-ant-1' });
  const models = within(dialog).getByRole('group', { name: 'Models' });
  // The newest Sonnet is ticked as the default, not the newest (often dearest) model.
  expect(within(models).getByRole('checkbox', { name: /claude-opus-5-5/ })).not.toBeChecked();
  expect(within(models).getByRole('checkbox', { name: /claude-sonnet-5-5/ })).toBeChecked();
  expect(within(models).getByText('Default')).toBeInTheDocument();
  fireEvent.click(within(models).getByRole('checkbox', { name: /claude-haiku-5-5/ }));
  fireEvent.click(within(models).getByRole('checkbox', { name: /claude-sonnet-5-5/ }));
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Check and save' })); });
  expect(onAdd).toHaveBeenCalledWith({ kind: 'anthropic', label: 'Team key', api_key: 'sk-ant-1', models: ['claude-haiku-5-5'] });
});

test('a provider that keeps no list of models says why and takes typed names', async () => {
  const { onAdd, onListModels } = page();
  onListModels.mockResolvedValueOnce({ field: 'models', text: 'This provider does not list its models. Type the model names instead.' });
  fireEvent.click(screen.getAllByRole('button', { name: /Add provider/ })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add a provider' });
  fireEvent.click(within(dialog).getByRole('radio', { name: /Other or self-hosted/ }));
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Office' } });
  fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://llm.example.com/v1' } });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'sk-1' } });
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Load models' })); });
  expect(onListModels).toHaveBeenCalledWith({ kind: 'openai_compatible', base_url: 'https://llm.example.com/v1', api_key: 'sk-1' });
  expect(within(dialog).getByText('This provider does not list its models. Type the model names instead.')).toBeInTheDocument();
  expect(within(dialog).getByLabelText('Model names')).toHaveAttribute('aria-invalid', 'true');
  fireEvent.change(within(dialog).getByLabelText('Model names'), { target: { value: 'qwen3' } });
  // Typing a name answers the refusal: the field is no longer marked wrong, and its help stays.
  expect(within(dialog).getByLabelText('Model names')).not.toHaveAttribute('aria-invalid');
  expect(within(dialog).getByText(/Separate with commas/)).toBeInTheDocument();
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Check and save' })); });
  expect(onAdd).toHaveBeenCalledWith({ kind: 'openai_compatible', label: 'Office', base_url: 'https://llm.example.com/v1', api_key: 'sk-1', models: ['qwen3'] });
});

test('a list loaded for a key changed since is dropped, and a list that fails on leaving the key keeps the focus where the person went', async () => {
  const { onListModels } = page();
  let first: (value: string[]) => void = () => {};
  onListModels.mockImplementationOnce(() => new Promise((resolve) => { first = resolve; }));
  onListModels.mockResolvedValueOnce({ field: 'api_key', text: 'The provider refused this key.' });
  fireEvent.click(screen.getAllByRole('button', { name: /Add provider/ })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add a provider' });
  const key = within(dialog).getByLabelText('API key');
  fireEvent.change(key, { target: { value: 'sk-old' } });
  await act(async () => { fireEvent.blur(key); });
  // The key changes while the old list is on its way: that list is not this key's.
  fireEvent.change(key, { target: { value: 'sk-new' } });
  await act(async () => { first(['claude-old-model']); });
  expect(within(dialog).queryByText('claude-old-model')).not.toBeInTheDocument();
  const name = within(dialog).getByLabelText('Name');
  await act(async () => { fireEvent.blur(key); name.focus(); });
  expect(within(dialog).getByText('The provider refused this key.')).toBeInTheDocument();
  expect(name).toHaveFocus();
});

test('models ticked for one provider are not sent after switching to another', async () => {
  const { onAdd, onListModels } = page();
  onListModels.mockResolvedValueOnce(['claude-sonnet-5-5']);
  fireEvent.click(screen.getAllByRole('button', { name: /Add provider/ })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add a provider' });
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Team' } });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'sk-1' } });
  await act(async () => { fireEvent.blur(within(dialog).getByLabelText('API key')); });
  expect(within(dialog).getByRole('checkbox', { name: /claude-sonnet-5-5/ })).toBeChecked();
  fireEvent.click(within(dialog).getByRole('radio', { name: /^OpenAI/ }));
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Check and save' })); });
  expect(onAdd).not.toHaveBeenCalled();
  expect(within(dialog).getByText('Pick at least one model.')).toBeInTheDocument();
});

test('a server that cannot store keys says so before anyone types one', () => {
  page({ settings: settings({ can_store_keys: false }) });
  fireEvent.click(screen.getAllByRole('button', { name: /Add provider/ })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add a provider' });
  expect(within(dialog).getByRole('alert')).toHaveTextContent('ask whoever runs this server');
  expect(within(dialog).getByRole('alert')).not.toHaveTextContent('MAILBOX_TOKEN_KEYS');
  expect(within(within(dialog).getByRole('alert')).getByRole('link', { name: 'Self-hosting guide: the key that encrypts stored secrets' })).toHaveAttribute('href', 'https://github.com/colliedesk/colliedesk/blob/main/docs/self-host/environment-variables.md#variables');
  expect(within(dialog).getByRole('button', { name: 'Check and save' })).toBeDisabled();
});

test('changing a key keeps the address, and says how to use another one', () => {
  page({ settings: settings({ providers: [provider({ kind: 'openai_compatible', base_url: 'https://openrouter.ai/api/v1' })] }) });
  fireEvent.click(screen.getByRole('tab', { name: /Providers/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Change key' }));
  const dialog = screen.getByRole('dialog', { name: 'Change the key of Team key' });
  expect(within(dialog).getByText('The address stays https://openrouter.ai/api/v1. To use another address, add a new provider.')).toBeInTheDocument();
});

test('changing to a key that fails says the old key still works', async () => {
  const { onSetKey } = page({ settings: settings({ providers: [provider()] }) });
  onSetKey.mockResolvedValueOnce({ field: 'api_key', text: 'The provider refused this key.' });
  fireEvent.click(screen.getByRole('tab', { name: /Providers/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Change key' }));
  const dialog = screen.getByRole('dialog', { name: 'Change the key of Team key' });
  fireEvent.change(within(dialog).getByLabelText('New key'), { target: { value: 'sk-bad' } });
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Check and replace' })); });
  expect(within(dialog).getByText(/Nothing changed: ••••7Qx2 is still in use./)).toBeInTheDocument();
});

test('deleting a provider names the features that will turn off', async () => {
  const { onRemove } = page({ settings: settings({ providers: [provider({ used_by: ['triage'] })] }) });
  fireEvent.click(screen.getByRole('tab', { name: /Providers/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Delete Team key' }));
  const dialog = screen.getByRole('alertdialog', { name: 'Delete Team key?' });
  expect(within(dialog).getByRole('listitem')).toHaveTextContent('Triage suggestions');
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Delete provider' })); });
  expect(onRemove).toHaveBeenCalledWith(providerID);
});

test('the history reads as sentences and never shows a key', () => {
  page({ settings: settings({ events: [
    { id: 'e1', kind: 'provider_key_changed', actor_agent_id: null, actor_name: 'Mai Tran', via_token: false, provider_id: providerID, provider_label: 'Team key', slot: null,
      detail: { from_hint: '3bQ1', to_hint: '7Qx2' }, created_at: '2026-10-22T08:00:00Z' },
    { id: 'e2', kind: 'slot_changed', actor_agent_id: null, actor_name: 'Mai Tran', via_token: true, provider_id: null, provider_label: 'Colliedesk AI', slot: 'draft',
      detail: { from: { enabled: false }, to: { enabled: true, provider_label: 'Colliedesk AI' } }, created_at: '2026-10-22T07:00:00Z' },
  ] }) });
  fireEvent.click(screen.getByRole('tab', { name: 'History' }));
  expect(screen.getByText(/changed the key of Team key \(••••3bQ1 → ••••7Qx2\)/)).toBeInTheDocument();
  expect(screen.getByText(/through an agent token/)).toBeInTheDocument();
  expect(screen.getByText(/turned on Reply drafts on Colliedesk AI/)).toBeInTheDocument();
});

test('a failed check is placed under the field it is about', () => {
  expect(refusal(new ConflictError('private', { code: 'ai_check_failed', check_error_code: 'blocked_address' }))).toEqual({ field: 'base_url', text: 'private' });
  expect(refusal(new ConflictError('refused', { code: 'ai_check_failed', check_error_code: 'auth' }))).toEqual({ field: 'api_key', text: 'refused' });
  expect(refusal(new ConflictError('no model', { code: 'ai_check_failed', check_error_code: 'model_not_found' }))).toEqual({ field: 'models', text: 'no model' });
  expect(refusal(new ConflictError('taken', { code: 'ai_provider_name_taken' }))).toEqual({ field: 'label', text: 'taken' });
  expect(refusal(new Error('API 500')).field).toBe('form');
});

test('the automatic runs are shown on triage and changed by an admin', async () => {
  const { onSetAutomatic } = page({ settings: settings({ server, slots: [slot({ source: 'server', model: 'claude-haiku-5-5', enabled: true, active: true }), slot({ slot: 'draft' })] }) });
  expect(screen.getByText('300 a day · 30 a minute')).toBeInTheDocument();
  expect(screen.getByText('None: only when an agent asks')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Change the automatic limits' }));
  const dialog = screen.getByRole('dialog', { name: 'Automatic runs' });
  fireEvent.change(within(dialog).getByLabelText('A day (UTC)'), { target: { value: '500' } });
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Save' })); });
  expect(onSetAutomatic).toHaveBeenCalledWith(500, 30, 100);
});

test('a number out of range marks its own field', async () => {
  const { onSetAutomatic } = page({ settings: settings({ server, slots: [slot({ source: 'server', model: 'claude-haiku-5-5', enabled: true, active: true }), slot({ slot: 'draft' })] }) });
  fireEvent.click(screen.getByRole('button', { name: 'Change the automatic limits' }));
  const dialog = screen.getByRole('dialog', { name: 'Automatic runs' });
  fireEvent.change(within(dialog).getByLabelText('A minute'), { target: { value: '5000' } });
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Save' })); });
  expect(onSetAutomatic).not.toHaveBeenCalled();
  expect(within(dialog).getByLabelText('A minute')).toHaveAttribute('aria-invalid', 'true');
  expect(within(dialog).getByLabelText('A minute')).toHaveAccessibleDescription('Enter a whole number from 0 to 1000.');
  expect(within(dialog).getByLabelText('A day (UTC)')).not.toHaveAttribute('aria-invalid');
});

test('a paused day and a used-up allowance each say what happens and what to do', () => {
  page({ settings: settings({ server, slots: [slot({ source: 'server', model: 'claude-haiku-5-5', enabled: true, active: true }), slot({ slot: 'draft', source: 'server', model: 'claude-sonnet-5-5', enabled: true, active: true })],
    automatic: { per_day: 300, per_minute: 30, used_today: 300, paused_until: '2026-10-23T00:00:00Z', draft_per_day: 100, drafts_today: 0, drafts_paused_until: null, drafts_reserved_until: null },
    allowance: { limit_micro_usd: 9_000_000, used_micro_usd: 9_000_000, resets_at: '2026-11-01T00:00:00Z', drafts_left: 0 } }) });
  expect(screen.queryByText(/drafts left/)).not.toBeInTheDocument();
  expect(screen.getByRole('status')).toHaveTextContent('Today’s limit of 300 is reached');
  expect(screen.getByRole('button', { name: 'Raise the daily limit' })).toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent('This month’s Colliedesk AI allowance is used up');
  expect(screen.getByRole('meter', { name: 'Allowance used this month' })).toHaveAttribute('aria-valuetext', '$9.00 of $9.00 used');
  expect(screen.getAllByText('Stopped: allowance used')).toHaveLength(2);
  expect(document.querySelector('.ai-st-ok')).toBeNull();
});

test('the usage tab is asked for when it opens and sums each feature', () => {
  const { onUsageMonth } = page({ usage: { month: '2026-10', allowance: null, rows: [
    { feature: 'draft', billed_to: 'server', provider_label: 'Colliedesk AI', calls: 87, failed: 1, automatic: 0, input_tokens: 740_000, output_tokens: 98_000, cost_micro_usd: 2_150_000 },
    { feature: 'check', billed_to: 'workspace_key', provider_label: 'Team key', calls: 6, failed: 2, automatic: 0, input_tokens: 400, output_tokens: 100, cost_micro_usd: null },
  ] } });
  fireEvent.click(screen.getByRole('tab', { name: 'Usage' }));
  expect(onUsageMonth).toHaveBeenCalled();
  const rows = screen.getAllByRole('row');
  expect(rows[1]).toHaveTextContent('Reply drafts');
  expect(rows[1]).toHaveTextContent('$2.15');
  expect(rows[1]).toHaveTextContent('740.0k · 98.0k');
  expect(rows[2]).toHaveTextContent('Your key');
  expect(rows[2]).toHaveTextContent('—');
});

test('agents do not see the usage tab', () => {
  page({ isAdmin: false });
  expect(screen.queryByRole('tab', { name: 'Usage' })).not.toBeInTheDocument();
});

test('a paused feature says so on its row', () => {
  page({ settings: settings({ server, slots: [slot({ source: 'server', model: 'claude-haiku-5-5', enabled: true, active: true }), slot({ slot: 'draft' })],
    automatic: { per_day: 0, per_minute: 30, used_today: 0, paused_until: '2026-10-23T00:00:00Z', draft_per_day: 100, drafts_today: 0, drafts_paused_until: null, drafts_reserved_until: null } }) });
  expect(screen.getByText('Paused until tomorrow')).toBeInTheDocument();
});

test('a server allowance of nothing reads as none included', () => {
  page({ settings: settings({ server, allowance: { limit_micro_usd: 0, used_micro_usd: 0, resets_at: '2026-11-01T00:00:00Z', drafts_left: 0 } }) });
  expect(screen.getByText(/No AI is included on this server/)).toBeInTheDocument();
});

test('the allowance says about how many reply drafts are left', () => {
  page({ settings: settings({ server, allowance: { limit_micro_usd: 9_000_000, used_micro_usd: 2_000_000, resets_at: '2026-11-01T00:00:00Z', drafts_left: 1240 } }) });
  expect(screen.getByText('About 1,240 reply drafts left')).toBeInTheDocument();
});

test('an admin turns on automatic triage only through a dialog that says what it does', async () => {
  const onSetSlot = vi.fn();
  const on = slot({ source: 'provider', provider_id: providerID, model: 'claude-haiku-5-5', enabled: true, active: true });
  page({ settings: settings({ providers: [provider({ used_by: ['triage'] })], slots: [on, slot({ slot: 'draft' })] }), onSetSlot });
  const toggle = screen.getByRole('switch', { name: 'Apply suggestions automatically' });
  expect(toggle).not.toBeChecked();
  expect(screen.getByRole('checkbox', { name: 'Tags' })).toBeDisabled();
  fireEvent.click(toggle);
  const dialog = await screen.findByRole('dialog', { name: 'Apply triage suggestions automatically?' });
  expect(within(dialog).getByText(/tags and priority as soon as they arrive/)).toBeInTheDocument();
  expect(onSetSlot).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Turn on' }));
  expect(onSetSlot).toHaveBeenCalledWith('triage', { source: 'provider', provider_id: providerID, model: 'claude-haiku-5-5', effort: 'low', enabled: true, automatic: true });
});

test('with automatic triage on, an admin picks what it applies, sees how it went, and turns it off at once', () => {
  const onSetSlot = vi.fn();
  const on = slot({ source: 'provider', provider_id: providerID, model: 'claude-haiku-5-5', enabled: true, active: true, automatic: true, auto_parts: ['tags', 'priority'] });
  page({ settings: settings({ providers: [provider({ used_by: ['triage'] })], slots: [on, slot({ slot: 'draft' })], auto_triage: { applied: 212, undone: 9 } }), onSetSlot });
  expect(screen.getByText('AI applied 212, undone 9')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Assignee' }));
  expect(onSetSlot).toHaveBeenCalledWith('triage', expect.objectContaining({ auto_parts: ['tags', 'priority', 'assignee'] }));
  fireEvent.click(screen.getByRole('switch', { name: 'Apply suggestions automatically' }));
  expect(onSetSlot).toHaveBeenLastCalledWith('triage', expect.objectContaining({ automatic: false }));
});

test('automatic triage waits for the feature to be on, and says when a provider change turned it off', () => {
  const off = slot({ source: 'provider', provider_id: providerID, model: 'claude-haiku-5-5' });
  page({ settings: settings({ providers: [provider()], slots: [off, slot({ slot: 'draft' })] }) });
  expect(screen.getByText('Turn on Triage suggestions first.')).toBeInTheDocument();
  expect(screen.getByRole('switch', { name: 'Apply suggestions automatically' })).toBeDisabled();
  cleanup();
  const moved = { id: '33333333-3333-4333-8333-333333333333', kind: 'slot_changed' as const, actor_agent_id: null, actor_name: 'Mai Tran', via_token: false,
    provider_id: providerID, provider_label: 'Team key', slot: 'triage' as const, created_at: '2026-10-22T10:00:00Z',
    detail: { from: { enabled: true, automatic: true, provider_label: 'Colliedesk AI' }, to: { enabled: true, automatic: false, provider_label: 'Team key' } } };
  page({ settings: settings({ providers: [provider()], slots: [{ ...off, enabled: true, active: true }, slot({ slot: 'draft' })], events: [moved] }) });
  expect(screen.getByText(/Apply suggestions automatically turned off: Triage suggestions moved to Team key/)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Turn on again' })).toBeInTheDocument();
});

test('an admin turns on automatic drafts only after a dialog estimates what they cost', async () => {
  const onSetSlot = vi.fn();
  const drafts = slot({ slot: 'draft', source: 'server', model: 'claude-sonnet-5-5', enabled: true, active: true });
  page({ settings: settings({ server, slots: [slot(), drafts], allowance: { limit_micro_usd: 9_000_000, used_micro_usd: 0, resets_at: '2026-11-01T00:00:00Z', drafts_left: 140 },
    auto_draft_estimate: { customer_messages: 412, draft_cost_micro_usd: 64_000 } }), onSetSlot });
  fireEvent.click(screen.getByRole('switch', { name: 'Write a draft when a customer writes' }));
  const dialog = await screen.findByRole('dialog', { name: 'Write a draft for every customer message?' });
  expect(within(dialog).getByText('412 customer messages')).toBeInTheDocument();
  expect(within(dialog).getByText('$26.37 a month · $0.06 a draft')).toBeInTheDocument();
  expect(within(dialog).getByText(/Automatic drafts may use half: \$4\.50, about 70 drafts/)).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Turn on' }));
  expect(onSetSlot).toHaveBeenCalledWith('draft', expect.objectContaining({ source: 'server', enabled: true, automatic: true }));
});

test('automatic drafts paused for the day say so and offer to raise the limit', () => {
  const drafts = slot({ slot: 'draft', source: 'server', model: 'claude-sonnet-5-5', enabled: true, active: true, automatic: true });
  const onSetAutomatic = vi.fn().mockResolvedValue(undefined);
  page({ settings: settings({ server, slots: [slot(), drafts],
    automatic: { per_day: 300, per_minute: 30, used_today: 100, paused_until: null, draft_per_day: 100, drafts_today: 100, drafts_paused_until: '2026-10-24T00:00:00Z', drafts_reserved_until: null } }),
  onSetAutomatic });
  expect(screen.getByText('100 of 100 automatic drafts today.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Raise the limit' }));
  expect(screen.getByRole('dialog', { name: 'Automatic runs' })).toBeInTheDocument();
  expect(screen.getByLabelText('Automatic drafts a day')).toHaveValue(100);
});

test('OpenAI and OpenRouter need only a key: their address is filled in', async () => {
  const { onAdd, onListModels } = page();
  onListModels.mockResolvedValueOnce(['openai/gpt-5', 'anthropic/claude-sonnet-5-5']);
  fireEvent.click(screen.getAllByRole('button', { name: /Add provider/ })[0]);
  const dialog = screen.getByRole('dialog', { name: 'Add a provider' });
  fireEvent.click(within(dialog).getByRole('radio', { name: /OpenRouter/ }));
  expect(within(dialog).queryByLabelText('Base URL')).not.toBeInTheDocument();
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Router' } });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'sk-or-1' } });
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Load models' })); });
  expect(onListModels).toHaveBeenCalledWith({ kind: 'openai_compatible', base_url: 'https://openrouter.ai/api/v1', api_key: 'sk-or-1' });
  fireEvent.click(within(dialog).getByRole('checkbox', { name: /openai\/gpt-5/ }));
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Check and save' })); });
  expect(onAdd).toHaveBeenCalledWith({ kind: 'openai_compatible', label: 'Router', base_url: 'https://openrouter.ai/api/v1', api_key: 'sk-or-1', models: ['openai/gpt-5'] });
});
