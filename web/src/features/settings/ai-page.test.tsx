import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { ConflictError } from '../../api/client';
import type { AIProvider, AISettings, AISlot } from '../../api/ai';
import { AIPage } from './ai-page';
import type { AIPageProps } from './ai-page';
import { refusal } from './ai-route';

const providerID = '11111111-1111-4111-8111-111111111111';

function slot(change: Partial<AISlot> = {}): AISlot {
  return { slot: 'triage', source: 'none', provider_id: null, model: null, effort: 'low', enabled: false, active: false, off_reason: null,
    consented_by: null, consented_at: null, updated_by: null, updated_at: null, ...change };
}

function provider(change: Partial<AIProvider> = {}): AIProvider {
  return { id: providerID, kind: 'anthropic', label: 'Team key', base_url: null, auth_kind: 'api_key', key_hint: '7Qx2',
    models: ['claude-sonnet-5-5', 'claude-haiku-5-5'], check_status: 'ok', check_error_code: null, checked_at: '2026-10-22T08:00:00Z',
    created_by: null, created_at: '2026-10-22T08:00:00Z', updated_at: '2026-10-22T08:00:00Z', used_by: [], ...change };
}

function settings(change: Partial<AISettings> = {}): AISettings {
  return { server: null, providers: [], slots: [slot(), slot({ slot: 'draft' })], events: [], provider_limit: 5, can_store_keys: true,
    allow_private_base_urls: false, ...change };
}

function page(props: Partial<AIPageProps> = {}) {
  const handlers = {
    onSetSlot: vi.fn(), onConsent: vi.fn(), onDismissConsent: vi.fn(), onCheck: vi.fn(),
    onAdd: vi.fn().mockResolvedValue(undefined), onSetKey: vi.fn().mockResolvedValue(undefined), onRemove: vi.fn().mockResolvedValue(undefined),
  };
  render(<AIPage isAdmin workspaceName="Northwind" timezone="UTC" shown={{ general: true, team: true, email: true, webForm: true, savedReplies: true, exportData: true, billing: false, ai: true }}
    showConsent={false} settings={settings()} {...handlers} {...props} />);
  return handlers;
}

const server = { kind: 'anthropic' as const, label: 'Colliedesk AI', models: { triage: ['claude-haiku-5-5'], draft: ['claude-sonnet-5-5', 'claude-opus-5-5'] } };

test('a server without any provider explains the two ways to start, and every feature is off', () => {
  page();
  expect(screen.getByRole('heading', { name: 'No AI provider on this server yet' })).toBeInTheDocument();
  expect(screen.getByText(/AI_SERVER_PROVIDER=anthropic/)).toBeInTheDocument();
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
  expect(banner).toHaveTextContent('go to Colliedesk AI');
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
  fireEvent.click(within(dialog).getByRole('radio', { name: /OpenAI-compatible/ }));
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Office' } });
  fireEvent.change(within(dialog).getByLabelText('Base URL'), { target: { value: 'https://10.0.0.12/v1' } });
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: 'sk-1' } });
  fireEvent.change(within(dialog).getByLabelText('Models'), { target: { value: 'llama, qwen ,' } });
  expect(within(dialog).getByLabelText('API key')).toHaveAttribute('type', 'password');
  onAdd.mockResolvedValueOnce({ field: 'base_url', text: 'This address is on a private network.' });
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Check and save' })); });
  expect(onAdd).toHaveBeenCalledWith({ kind: 'openai_compatible', label: 'Office', base_url: 'https://10.0.0.12/v1', api_key: 'sk-1', models: ['llama', 'qwen'] });
  expect(within(dialog).getByLabelText('Base URL')).toHaveAttribute('aria-invalid', 'true');
  expect(within(dialog).getByText('This address is on a private network.')).toBeInTheDocument();
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Check and save' })); });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('a server that cannot store keys says so before anyone types one', () => {
  page({ settings: settings({ can_store_keys: false }) });
  expect(screen.getAllByRole('button', { name: /Add provider/ })[0]).toBeDisabled();
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
  expect(refusal(new ConflictError('taken', { code: 'ai_provider_name_taken' }))).toEqual({ field: 'label', text: 'taken' });
  expect(refusal(new Error('API 500')).field).toBe('form');
});
