import { render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';

// The console speaks the operator's choice, else the browser's language, else English. Vietnamese rewrites the
// English strings in place, so each test loads the console's modules afresh.
beforeEach(() => { vi.resetModules(); });
afterEach(() => { vi.restoreAllMocks(); localStorage.clear(); document.documentElement.lang = 'en'; window.history.replaceState(null, '', '/'); });

const languages = (tags: string[]) => vi.spyOn(navigator, 'languages', 'get').mockReturnValue(tags);

async function startConsole(path = '/operator/') {
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ error: { code: 'operator_session_required', message: 'no' } }),
    { status: 401, headers: { 'Content-Type': 'application/json' } })));
  window.history.replaceState(null, '', path);
  await (await import('./lang')).loadLang();
  const { App } = await import('./app');
  render(<App />);
}

test('a browser that prefers Vietnamese gets the console in Vietnamese, its lazy parts too', async () => {
  languages(['vi-VN', 'en-US']);
  await startConsole();
  expect(await screen.findByRole('heading', { name: 'Phiên của bạn đã kết thúc' })).toBeInTheDocument();
  expect(document.documentElement.lang).toBe('vi');
  expect((await import('./ai-provider-messages')).am.serverModels).toBe('Mô hình');
  expect((await import('./fields')).fields['smtp.host'].label).toBe('Máy chủ SMTP');
});

test('the sign-in page without a link speaks Vietnamese too', async () => {
  languages(['vi-VN']);
  await startConsole('/operator/sign-in');
  expect(await screen.findByRole('heading', { name: 'Mở bảng điều khiển bằng liên kết đăng nhập' })).toBeInTheDocument();
});

test('an operator who chose English keeps English in a Vietnamese browser', async () => {
  languages(['vi-VN']);
  localStorage.setItem('colliedesk.operator.lang', 'en');
  await startConsole();
  expect(await screen.findByRole('heading', { name: 'Your session ended' })).toBeInTheDocument();
  expect(document.documentElement.lang).toBe('en');
});

test('the browser picks the first of English or Vietnamese it prefers; anything else reads English', async () => {
  const { pickLang } = await import('./lang');
  languages(['fr-FR', 'vi']);
  expect(pickLang()).toBe('vi');
  languages(['en-GB', 'vi-VN']);
  expect(pickLang()).toBe('en');
  languages(['de-DE']);
  expect(pickLang()).toBe('en');
  localStorage.setItem('colliedesk.operator.lang', 'vi');
  expect(pickLang()).toBe('vi');
});

// TypeScript holds each translation to the English keys and arguments, except in maps keyed by any string
// (groups, warnings, fields, sections…): this walks every one.
function shape(value: unknown): unknown {
  if (typeof value === 'function') return `function/${value.length}`;
  if (Array.isArray(value)) return value.map(shape);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([key, item]) => [key, shape(item)]));
  return typeof value;
}

test('every English string has its Vietnamese, with the same keys, arguments and option values', async () => {
  const en = { m: (await import('./messages')).m, am: (await import('./ai-provider-messages')).am, cloud: (await import('./cloud-messages')).cloudMessages };
  const vn = { m: (await import('./vi/messages')).m, am: (await import('./vi/ai-provider-messages')).am, cloud: (await import('./vi/cloud-messages')).cloudMessages };
  expect(shape(vn)).toEqual(shape(en));
  const { fields, sections } = await import('./fields');
  const words = await import('./vi/fields');
  expect(Object.keys(words.fields).sort()).toEqual(Object.keys(fields).sort());
  for (const [key, info] of Object.entries(fields)) {
    const vi = words.fields[key];
    expect([key, Boolean(vi.help), Boolean(vi.other), Boolean(vi.placeholder)]).toEqual([key, Boolean(info.help), Boolean(info.other), Boolean(info.placeholder)]);
    expect([key, vi.options?.map(([value]) => value)]).toEqual([key, info.options?.map(([value]) => value)]);
  }
  expect(Object.fromEntries(Object.entries(words.sections).map(([group, list]) => [group, list.length])))
    .toEqual(Object.fromEntries(Object.entries(sections).map(([group, list]) => [group, list.length])));
});
