import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, expect, test, vi } from 'vitest';
import type { ConsoleData } from './app';
import type { Setting } from './api';
import { parseAllowance, parseModels, parsePrices, writeAllowance, writeModels, writePrices } from './ai-provider';
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
    status: { desired_version: 4, warnings: [] },
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

test('the models, prices and allowance read and write back as they were', () => {
  expect(writeModels(parseModels(production))).toBe(production);
  expect(writeModels(parseModels('triage=a,intake=b|c'))).toBe('triage=a,intake=b|c');
  for (const prices of ['claude-haiku-5-5=0.1:0.5:0.01', 'a=1:2,b:free=0.1:0.2:0.01:0.125']) expect(writePrices(parsePrices(prices))).toBe(prices);
  for (const allowance of ['', '10', 'workspace=2,trial=1', 'agent=3,trial=2', 'agent=3']) expect(writeAllowance(parseAllowance(allowance))).toBe(allowance);
});

test('the saved provider opens on its service and model, with nothing changed', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ ok: true, errors: {}, test: { name: 'ai_models', ok: true, message: '', models: ['claude-haiku-5-5'], prices: {} } }))));
  page(anthropic());
  expect(await screen.findByRole('switch', { name: "The server's provider" })).toBeChecked();
  expect(screen.getByRole('radio', { name: /Anthropic/ })).toBeChecked();
  expect(screen.getByRole('combobox', { name: 'Model for every feature' })).toHaveValue('claude-haiku-5-5');
  expect(screen.queryByRole('region', { name: /unsaved change/ })).not.toBeInTheDocument();
});

test('off keeps every other setting, and on again needs nothing typed', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ ok: true, errors: {}, test: { name: 'ai_models', ok: true, message: '', models: [], prices: {} } }))));
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

test('the models list by themselves; typing searches them, and a model picked gets its listed price', async () => {
  const fetch = vi.fn(async () => new Response(JSON.stringify({ ok: true, errors: {}, test: { name: 'ai_models', ok: true, message: '',
    models: ['z-ai/glm-5', 'z-ai/glm-5.3', 'google/gemini-3.8-flash'], prices: { 'z-ai/glm-5.3': { input: 0.0699, output: 1.4, cache_read: 0.0689, cache_write: 0 } } } })));
  vi.stubGlobal('fetch', fetch);
  page(anthropic());
  fireEvent.click(await screen.findByRole('radio', { name: /^OpenRouter/ }));
  expect(shown.set).toMatchObject({ 'ai.server.provider': 'openai_compatible', 'ai.server.base_url': 'https://openrouter.ai/api/v1' });
  await screen.findByText(/3 models available/, {}, { timeout: 3000 });
  expect(JSON.parse(String((fetch.mock.calls.at(-1) as unknown as [string, RequestInit])[1].body)).test).toBe('ai_models');
  const box = screen.getByRole('combobox', { name: 'Model for every feature' });
  fireEvent.focus(box);
  fireEvent.change(box, { target: { value: 'glm 5.3' } });
  expect(screen.getAllByRole('option').map((option) => option.textContent)).toEqual(['z-ai/glm-5.3$0.0699 in · $1.4 out per 1M tokens', 'Use “glm 5.3”']);
  fireEvent.keyDown(box, { key: 'Enter' });
  expect(shown.set['ai.server.models']).toBe('triage=z-ai/glm-5.3,draft=z-ai/glm-5.3,chat=z-ai/glm-5.3,intake=z-ai/glm-5.3');
  expect(shown.set['ai.server.prices']).toBe('z-ai/glm-5.3=0.0699:1.4:0.0689');
  expect(screen.getByRole('textbox', { name: 'Input z-ai/glm-5.3' })).toHaveValue('0.0699');
  fireEvent.click(screen.getByRole('button', { name: 'Choose a model per feature' }));
  const fallback = screen.getByRole('combobox', { name: 'Chat intake, when its model fails' });
  fireEvent.focus(fallback);
  fireEvent.change(fallback, { target: { value: 'gemini' } });
  fireEvent.keyDown(fallback, { key: 'Enter' });
  expect(shown.set['ai.server.models']).toBe('triage=z-ai/glm-5.3,draft=z-ai/glm-5.3,chat=z-ai/glm-5.3,intake=z-ai/glm-5.3|google/gemini-3.8-flash');
  expect(screen.getByText(/Needs a price/)).toBeInTheDocument();
});

test('the allowance is picked as a mode and amounts', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ ok: true, errors: {}, test: { name: 'ai_models', ok: true, message: '', models: [], prices: {} } }))));
  page([...anthropic().filter((each) => each.key !== 'ai.server.monthly_allowance'), saved('ai.server.monthly_allowance', 'workspace=2,trial=1')]);
  expect(await screen.findByRole('radio', { name: 'Same for every workspace' })).toBeChecked();
  fireEvent.change(screen.getByRole('textbox', { name: 'Each workspace' }), { target: { value: '5' } });
  expect(shown.set['ai.server.monthly_allowance']).toBe('workspace=5,trial=1');
  fireEvent.click(screen.getByRole('radio', { name: 'Per active agent' }));
  expect(shown.set['ai.server.monthly_allowance']).toBe('agent=5,trial=1');
  fireEvent.click(screen.getByRole('radio', { name: 'No limit' }));
  expect(shown.unset).toContain('ai.server.monthly_allowance');
});
