import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { api } from '../api/client';
import { collectDiagnostics, installDiagnostics, resetDiagnostics } from './diagnostics';
import { track } from './product-analytics';

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);

beforeEach(() => {
  resetDiagnostics();
  installDiagnostics();
});
afterEach(() => fetchMock.mockReset());

const failure = (status: number, requestID?: string) =>
  new Response('{}', { status, headers: requestID ? { 'X-Request-Id': requestID } : {} });

test('a failed API call is kept with its request id, and a successful one is not', async () => {
  fetchMock.mockResolvedValueOnce(failure(500, '9f3ac1d2b7e4'));
  await api.tags().catch(() => undefined);
  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ items: [] }), { status: 200, headers: { 'X-Request-Id': 'aaaaaaaaaaaa' } }));
  await api.tags();
  const { failed_api_calls: calls } = collectDiagnostics('1.2.3');
  expect(calls).toHaveLength(1);
  expect(calls?.[0]).toMatchObject({ method: 'GET', route: '/api/v1/tags', status: 500, request_id: '9f3ac1d2b7e4' });
  expect(Date.parse(calls?.[0].at ?? '')).not.toBeNaN();
});

test('a failed call without an X-Request-Id is kept with an empty id', async () => {
  fetchMock.mockResolvedValueOnce(failure(502));
  await api.tags().catch(() => undefined);
  expect(collectDiagnostics().failed_api_calls?.[0]).toMatchObject({ status: 502, request_id: '' });
});

test('the query string and record ids never reach the route', async () => {
  fetchMock.mockResolvedValueOnce(failure(500, 'bbbbbbbbbbbb'));
  await api.conversations('q=refund+for+zed&page_size=10').catch(() => undefined);
  fetchMock.mockResolvedValueOnce(failure(404, 'cccccccccccc'));
  await api.conversation('0b1e5d4a-7c1b-4a6e-9d53-1f2a3b4c5d6e').catch(() => undefined);
  const routes = collectDiagnostics().failed_api_calls?.map((call) => call.route);
  expect(routes).toEqual(['/api/v1/conversations', '/api/v1/conversations/{id}']);
  expect(JSON.stringify(collectDiagnostics())).not.toContain('refund');
});

test('a script error and an unhandled rejection are recorded with their source', () => {
  window.dispatchEvent(new ErrorEvent('error', { message: 'x is undefined', error: new TypeError('x is undefined') }));
  const rejection = new Event('unhandledrejection') as Event & { reason: unknown };
  rejection.reason = new Error('Promise broke');
  window.dispatchEvent(rejection);
  const { client_errors: errors } = collectDiagnostics();
  expect(errors).toHaveLength(2);
  expect(errors?.[0]).toMatchObject({ message: 'TypeError: x is undefined', source: 'window.error' });
  expect(errors?.[1]).toMatchObject({ message: 'Error: Promise broke', source: 'unhandledrejection' });
});

test('the rings keep the newest entries within their caps and cut long text', async () => {
  for (let index = 0; index < 35; index++) {
    window.dispatchEvent(new ErrorEvent('error', { message: `boom ${index}`, error: new Error(`boom ${index}`) }));
    fetchMock.mockResolvedValueOnce(failure(500, `req-${String(index).padStart(8, '0')}`));
    await api.tags().catch(() => undefined);
    track('saved_reply_inserted');
  }
  window.dispatchEvent(new ErrorEvent('error', { message: 'long', error: new Error('e'.repeat(5000)) }));
  const diagnostics = collectDiagnostics();
  expect(diagnostics.client_errors).toHaveLength(20);
  expect(diagnostics.client_errors?.at(-1)?.message.length).toBeLessThanOrEqual(520);
  expect(diagnostics.client_errors?.some((item) => item.message === 'Error: boom 0')).toBe(false);
  expect(diagnostics.failed_api_calls).toHaveLength(20);
  expect(diagnostics.failed_api_calls?.at(-1)?.request_id).toBe('req-00000034');
  expect(diagnostics.product_events).toHaveLength(30);
});

test('product event names are kept even when analytics never started, and carry no properties', () => {
  track('conversation_opened', { source: 'list' });
  track('inbox_searched', { has_results: true });
  expect(collectDiagnostics().product_events).toEqual(['conversation_opened', 'inbox_searched']);
});

test('the page, browser and window are read when the report is made, without a query string', () => {
  window.history.pushState({}, '', '/conversations/abc?q=zed#top');
  const diagnostics = collectDiagnostics('0.0.41');
  expect(diagnostics.page_path).toBe('/conversations/abc');
  expect(diagnostics.app_version).toBe('0.0.41');
  expect(diagnostics.user_agent).toBe(navigator.userAgent);
  expect(diagnostics.viewport).toEqual({ width: window.innerWidth, height: window.innerHeight });
});

test('installing twice records each error once', () => {
  installDiagnostics();
  window.dispatchEvent(new ErrorEvent('error', { message: 'once', error: new Error('once') }));
  expect(collectDiagnostics().client_errors).toHaveLength(1);
});
