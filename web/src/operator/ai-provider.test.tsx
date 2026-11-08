import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import type { ConsoleData } from './app';
import type { Setting } from './api';
import { parseModels, writeModels } from './ai-provider';
import { ConfigPage } from './config-page';
import type { Draft } from './guards';
import { emptyDraft } from './guards';

const setting = (key: string, extra: Partial<Setting> = {}): Setting => ({ key, group: 'ai', secret: false, hot: true, guards: [], set: false, source: 'default', editable: true, ...extra });
const saved = (key: string, value: string, extra: Partial<Setting> = {}) => setting(key, { set: true, value, source: 'console', ...extra });
const production = 'triage=claude-haiku-5-5,draft=claude-haiku-5-5,chat=claude-haiku-5-5,intake=claude-haiku-5-5';

let shown: Draft = emptyDraft();
function page(settings: Setting[]) {
  const data = {
    me: { operator: { id: 'o1', email: 'ops@example.com', totp_enabled: true }, session: { id: 's1' }, console: { origin: '', path_mode: true } },
    status: { desired_version: 4 },
    config: { version: 4, settings, bootstrap: [], key_ring: { configured: true, key_ids: ['k1'] }, features: [] },
    reload: async () => {},
  } as unknown as ConsoleData;
  function Page() {
    const [draft, setDraft] = useState<Draft>(emptyDraft());
    shown = draft;
    return <ConfigPage group="ai" data={data} draft={draft} setDraft={setDraft} />;
  }
  return render(<Page />);
}

const anthropic = () => [saved('ai.server.provider', 'anthropic'), saved('ai.server.label', 'Colliedesk AI'), setting('ai.server.base_url'),
  saved('ai.server.api_key', '', { secret: true, value: null as unknown as string }), saved('ai.server.models', production),
  setting('ai.server.prices'), setting('ai.server.monthly_allowance')];

afterEach(() => { vi.unstubAllGlobals(); });

test('the models string reads and writes back as it was', () => {
  expect(writeModels(parseModels(production))).toBe(production);
  expect(writeModels(parseModels('triage=a,intake=b|c'))).toBe('triage=a,intake=b|c');
});

test('the saved provider opens on its service and model, with nothing changed', async () => {
  page(anthropic());
  expect(await screen.findByRole('switch', { name: "The server's provider" })).toBeChecked();
  expect(screen.getByRole('radio', { name: /Anthropic/ })).toBeChecked();
  expect(screen.getByRole('combobox', { name: 'Model for every feature' })).toHaveValue('claude-haiku-5-5');
  expect(screen.queryByRole('region', { name: /unsaved change/ })).not.toBeInTheDocument();
});

test('off keeps every other setting, and on again needs nothing typed', async () => {
  page(anthropic());
  const on = await screen.findByRole('switch', { name: "The server's provider" });
  fireEvent.click(on);
  expect(shown.unset).toEqual(['ai.server.provider']);
  expect(Object.keys(shown.set)).toEqual([]);
  expect(screen.queryByRole('radio', { name: /^OpenAI/ })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('switch', { name: "The server's provider" }));
  expect(shown.unset).toEqual([]);
  expect(screen.queryByRole('region', { name: /unsaved change/ })).not.toBeInTheDocument();
});

test('a service fills in its address, and the models loaded for its key are picked for every feature or each one', async () => {
  const fetch = vi.fn(async () => new Response(JSON.stringify({ ok: true, errors: {}, test: { name: 'ai_models', ok: true, message: '', models: ['gpt-5-mini', 'gpt-5'] } })));
  vi.stubGlobal('fetch', fetch);
  page(anthropic());
  fireEvent.click(await screen.findByRole('radio', { name: /^OpenAI/ }));
  expect(shown.set).toMatchObject({ 'ai.server.provider': 'openai_compatible', 'ai.server.base_url': 'https://api.openai.com/v1' });
  fireEvent.click(screen.getByRole('button', { name: 'Load models' }));
  await screen.findByText('2 models listed.');
  const body = JSON.parse(String((fetch.mock.calls[0] as unknown as [string, RequestInit])[1].body));
  expect(body.test).toBe('ai_models');
  fireEvent.change(screen.getByRole('combobox', { name: 'Model for every feature' }), { target: { value: 'gpt-5-mini' } });
  expect(shown.set['ai.server.models']).toBe('triage=gpt-5-mini,draft=gpt-5-mini,chat=gpt-5-mini,intake=gpt-5-mini');
  fireEvent.click(screen.getByRole('button', { name: 'Choose a model per feature' }));
  fireEvent.change(screen.getByRole('combobox', { name: 'Reply drafts' }), { target: { value: 'gpt-5' } });
  fireEvent.change(screen.getByRole('combobox', { name: 'AI chat' }), { target: { value: '' } });
  fireEvent.change(screen.getByRole('combobox', { name: 'Chat intake, when its model fails' }), { target: { value: 'gpt-5' } });
  expect(shown.set['ai.server.models']).toBe('triage=gpt-5-mini,draft=gpt-5,intake=gpt-5-mini|gpt-5');
});
