import { afterEach, expect, test, vi } from 'vitest';
import { api, PlanLimitError, WorkspaceLockedError } from './client';
import { NotAnAgentError, SignupError } from './cloud-signup';
import { ExportRefusedError } from './workspace-export';

const reply = (status: number, body: unknown = {}, headers: Record<string, string> = {}) =>
  new Response(typeof body === 'string' ? body : JSON.stringify(body), { status, headers });
const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => fetchMock.mockReset());

const apiError = (code: string, message = 'Server words.') => ({ error: { code, message } });

test('402 workspace_locked becomes a WorkspaceLockedError that is still a plan limit', async () => {
  fetchMock.mockResolvedValue(reply(402, apiError('workspace_locked', 'This workspace is locked.')));
  const error = await api.tags().catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(WorkspaceLockedError);
  expect(error).toBeInstanceOf(PlanLimitError);
});

test('another 402 stays an ordinary plan limit', async () => {
  fetchMock.mockResolvedValue(reply(402, apiError('plan_limit_reached', 'Agent limit.')));
  const error = await api.tags().catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(PlanLimitError);
  expect(error).not.toBeInstanceOf(WorkspaceLockedError);
});

test('a verified identity that is nobody\'s agent is told apart from a refused origin', async () => {
  fetchMock.mockResolvedValueOnce(reply(403, apiError('forbidden')));
  await expect(api.exchangeCloudSession('t')).rejects.toBeInstanceOf(NotAnAgentError);
  fetchMock.mockResolvedValueOnce(reply(403, apiError('csrf_rejected')));
  await expect(api.exchangeCloudSession('t')).rejects.not.toBeInstanceOf(NotAnAgentError);
});

test.each([
  [409, 'trial_already_used', 'trial_already_used'],
  [409, 'slug_unavailable', 'slug_unavailable'],
  [422, 'name_not_allowed', 'name_not_allowed'],
  [429, 'rate_limited', 'rate_limited'],
  [400, 'invalid_request', 'invalid'],
  [503, 'signup_unavailable', 'signup_unavailable'],
  [500, 'internal_error', 'unavailable'],
])('sign-up %i %s is reported as %s', async (status, code, reason) => {
  fetchMock.mockResolvedValue(reply(status, apiError(code, 'Words from the server.')));
  const error = await api.signUpCloud('token', 'Acme').catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(SignupError);
  expect((error as SignupError).reason).toBe(reason);
});

test('sign-up sends the bearer token and the workspace name and returns the session', async () => {
  fetchMock.mockResolvedValue(reply(201, {
    agent: { id: '11111111-1111-4111-8111-111111111111', email: 'ana@example.com', display_name: '', role: 'admin', timezone_override: null },
    workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' },
  }));
  const redeemed = await api.signUpCloud('jwt', 'Acme');
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(url).toBe('/api/v1/auth/cloud/signup');
  expect((init.headers as Record<string, string>).Authorization).toBe('Bearer jwt');
  expect(JSON.parse(init.body as string)).toEqual({ workspace_name: 'Acme' });
  expect(redeemed.workspace.slug).toBe('acme');
});

test('export reads the whole stream, reports progress and names the file from the server', async () => {
  const chunks = [new Uint8Array([1, 2, 3]), new Uint8Array([4, 5])];
  fetchMock.mockResolvedValue(new Response(new ReadableStream({
    start(controller) { chunks.forEach((chunk) => controller.enqueue(chunk)); controller.close(); },
  }), { status: 200, headers: { 'Content-Disposition': 'attachment; filename="colliedesk-acme-20261015T100000Z.zip"' } }));
  const progress: number[] = [];
  const archive = await api.exportWorkspace((bytes) => progress.push(bytes));
  expect(archive.filename).toBe('colliedesk-acme-20261015T100000Z.zip');
  expect(archive.bytes).toBe(5);
  expect(progress).toEqual([3, 5]);
});

test('export 429 carries the wait the server asked for; 409 and 403 are their own refusals', async () => {
  fetchMock.mockResolvedValueOnce(reply(429, apiError('rate_limited'), { 'Retry-After': '120' }));
  await expect(api.exportWorkspace()).rejects.toMatchObject({ refusal: 'server_busy', retryAfterSeconds: 120 });
  fetchMock.mockResolvedValueOnce(reply(409, apiError('conflict')));
  await expect(api.exportWorkspace()).rejects.toMatchObject({ refusal: 'busy' });
  fetchMock.mockResolvedValueOnce(reply(403, apiError('forbidden')));
  await expect(api.exportWorkspace()).rejects.toMatchObject({ refusal: 'forbidden' });
});

test('an export that breaks off halfway is a failure, never a short file', async () => {
  fetchMock.mockResolvedValue(new Response(new ReadableStream({
    start(controller) { controller.enqueue(new Uint8Array([1])); controller.error(new Error('reset')); },
  }), { status: 200 }));
  const error = await api.exportWorkspace().catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(ExportRefusedError);
  expect((error as ExportRefusedError).refusal).toBe('failed');
});
