import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { render, translate } from './codes';
import table from './codes.en.json';
import { codes as viCodes } from './vi/codes';

// The console's message codes (codes.ts). codes.en.json is written from the server's templates with one example
// each (TestTheConsoleTableOfCodesIsTheServersEnglish): the console renders each example as the server wrote it.
const en = table as Record<string, { text: string; params?: Record<string, string | number>; message: string }>;
const names = (template: string) => [...new Set([...template.matchAll(/\{"?([a-z_]+)/g)].map((match) => match[1]))].sort();

beforeEach(() => { vi.resetModules(); });
afterEach(() => { vi.restoreAllMocks(); localStorage.clear(); document.documentElement.lang = 'en'; });

test('the English table renders every example as the server wrote it', () => {
  for (const [code, entry] of Object.entries(en)) expect([code, render(entry.text, entry.params)]).toEqual([code, entry.message]);
});

test('every code has its Vietnamese, with the same placeholders', () => {
  expect(Object.keys(viCodes).sort()).toEqual(Object.keys(en).sort());
  for (const [code, entry] of Object.entries(en)) expect([code, names(viCodes[code as keyof typeof viCodes])]).toEqual([code, names(entry.text)]);
});

test('a text without a code, or with a code the table lacks, stays as the server wrote it', () => {
  const body = { errors: { 'smtp.host': 'plain words', 'ai.server.models': 'x' }, field_errors: { 'ai.server.models': { code: 'nope' } } };
  translate(body, viCodes);
  expect(body.errors).toEqual({ 'smtp.host': 'plain words', 'ai.server.models': 'x' });
});

test('in Vietnamese, the coded answers of the server arrive translated; English keeps the server text', async () => {
  const answer = {
    ok: false,
    errors: { 'ai.server.models': 'ai.server.models lists triage twice', 'smtp.host': 'an error without a code' },
    field_errors: { 'ai.server.models': { code: 'ai_server_models_twice', params: { slot: 'triage' } } },
    test: { name: 'ai', ok: false, message: 'The provider refused claude-haiku-5-5: 401', code: 'test_ai_refused', params: { model: 'claude-haiku-5-5', detail: '401' } },
  };
  const respond = () => Promise.resolve(new Response(JSON.stringify(answer), { status: 200, headers: { 'Content-Type': 'application/json' } }));
  vi.spyOn(globalThis, 'fetch').mockImplementation(respond);
  localStorage.setItem('colliedesk.operator.lang', 'vi');
  await (await import('./lang')).loadLang();
  const viResult = await (await import('./api')).api.check({});
  expect(viResult.errors).toEqual({ 'ai.server.models': 'ai.server.models liệt kê triage hai lần', 'smtp.host': 'an error without a code' });
  expect(viResult.test?.message).toBe('Nhà cung cấp từ chối claude-haiku-5-5: 401');

  vi.resetModules();
  localStorage.setItem('colliedesk.operator.lang', 'en');
  await (await import('./lang')).loadLang();
  const enResult = await (await import('./api')).api.check({});
  expect(enResult.errors['ai.server.models']).toBe('ai.server.models lists triage twice');
  expect(enResult.test?.message).toBe('The provider refused claude-haiku-5-5: 401');
});

test('a refused request is translated by its message code and its fields by their codes', () => {
  const body = { error: { code: 'config_invalid', message: 'whole text', fields: { 'ai.server.provider': 'en words' },
    field_errors: { 'ai.server.provider': { code: 'ai_server_provider_invalid' } }, message_code: 'secret_not_valid', params: { keys: 'smtp.password' } } };
  translate(body, viCodes);
  expect(body.error.fields['ai.server.provider']).toBe('ai.server.provider phải là anthropic hoặc openai_compatible');
  expect(body.error.message).toBe('smtp.password: không hợp lệ');
});
