import { fireEvent, render, screen, within } from '@testing-library/react';
import { vi } from 'vitest';
import type { AIProvider, AISettings, AISlot } from '../../api/ai';
import { AIPage } from './ai-page';
import type { AIPageProps } from './ai-page';

// Builders and a page renderer for the tests of Settings → Built-in AI.
export const providerID = '11111111-1111-4111-8111-111111111111';

export function slot(change: Partial<AISlot> = {}): AISlot {
  return { slot: 'triage', source: 'none', provider_id: null, model: null, fallback_model: null, effort: 'low', enabled: false, default_level: 'off', automatic: false, auto_parts: [], active: false, off_reason: null,
    consented_by: null, consented_at: null, updated_by: null, updated_at: null, monthly_cap_micro_usd: null, ...change };
}

export function provider(change: Partial<AIProvider> = {}): AIProvider {
  return { id: providerID, kind: 'anthropic', label: 'Team key', base_url: null, auth_kind: 'api_key', key_hint: '7Qx2',
    models: ['claude-sonnet-5-5', 'claude-haiku-5-5'], check_status: 'ok', check_error_code: null, checked_at: '2026-10-22T08:00:00Z',
    created_by: null, created_at: '2026-10-22T08:00:00Z', updated_at: '2026-10-22T08:00:00Z', used_by: [], routing: null, ...change };
}

export function settings(change: Partial<AISettings> = {}): AISettings {
  return { server: null, server_off: null, providers: [], slots: [slot(), slot({ slot: 'draft' })], events: [], provider_limit: 5, can_store_keys: true,
    allow_private_base_urls: false, automatic: { per_day: 300, per_minute: 30, used_today: 0, paused_until: null, draft_per_day: 100, drafts_today: 0, drafts_paused_until: null, drafts_reserved_until: null, intake_per_day: 300, intake_today: 0, intake_paused_until: null }, allowance: null,
    auto_triage: { applied: 0, undone: 0 }, auto_draft_estimate: { customer_messages: 0, draft_cost_micro_usd: null }, channels: [], source_consents: [], ...change };
}

export function page(props: Partial<AIPageProps> = {}) {
  const handlers = {
    onSetSlot: vi.fn(), onConsent: vi.fn(), onDismissConsent: vi.fn(), onCheck: vi.fn(),
    onAdd: vi.fn().mockResolvedValue(undefined), onListModels: vi.fn().mockResolvedValue([]), onSetKey: vi.fn().mockResolvedValue(undefined), onRemove: vi.fn().mockResolvedValue(undefined),
    onSetAutomatic: vi.fn().mockResolvedValue(undefined), onUsageMonth: vi.fn(),
  };
  render(<AIPage isAdmin workspaceName="Northwind" timezone="UTC" shown={{ general: true, team: true, channels: true, savedReplies: true, exportData: true, billing: false, ai: true }}
    showConsent={false} settings={settings()} {...handlers} {...props} />);
  return handlers;
}

export const server = { kind: 'anthropic' as const, label: 'Colliedesk AI', models: { triage: ['claude-haiku-5-5'], draft: ['claude-sonnet-5-5', 'claude-opus-5-5'] } };

// openSheet opens a feature's sheet from its row and returns it.
export function openSheet(feature: string) {
  fireEvent.click(screen.getByRole('button', { name: `${feature} settings` }));
  return screen.getByRole('dialog', { name: feature });
}

// closeSheet presses Done in the open sheet.
export function closeSheet() {
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Done' }));
}
