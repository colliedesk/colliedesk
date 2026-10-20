import { afterEach, expect, test, vi } from 'vitest';
import { api, ConflictError, PlanLimitError, SeatPausedError, WorkspaceLockedError } from './client';
import { AttachmentsTooLargeError, AttachmentStorageError } from './attachments-error';
import { NotAnAgentError, SignupError } from './cloud-signup';
import { ExportRefusedError } from './workspace-export';
import { OutboundRefusedError } from './outbound-error';

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

test('export asks for a one-time link and hands back its url', async () => {
  fetchMock.mockResolvedValue(reply(201, { url: '/api/v1/export/download/abc', expires_in_seconds: 60 }));
  expect(await api.requestExportLink()).toBe('/api/v1/export/download/abc');
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(url).toBe('/api/v1/export/link');
  expect(init.method).toBe('POST');
});

test('export 429 carries the wait the server asked for; 409 and 403 are their own refusals', async () => {
  fetchMock.mockResolvedValueOnce(reply(429, apiError('rate_limited'), { 'Retry-After': '120' }));
  await expect(api.requestExportLink()).rejects.toMatchObject({ refusal: 'server_busy', retryAfterSeconds: 120 });
  fetchMock.mockResolvedValueOnce(reply(409, apiError('conflict')));
  await expect(api.requestExportLink()).rejects.toMatchObject({ refusal: 'busy' });
  fetchMock.mockResolvedValueOnce(reply(403, apiError('forbidden')));
  await expect(api.requestExportLink()).rejects.toMatchObject({ refusal: 'forbidden' });
});

test('a network failure or an unreadable answer is a failed export, never a navigation', async () => {
  fetchMock.mockRejectedValueOnce(new TypeError('offline'));
  await expect(api.requestExportLink()).rejects.toBeInstanceOf(ExportRefusedError);
  fetchMock.mockResolvedValueOnce(reply(201, { nope: true }));
  await expect(api.requestExportLink()).rejects.toMatchObject({ refusal: 'failed' });
});

test('updateAgent sends only the given change and parses the team member', async () => {
  const member = { id: '22222222-2222-4222-8222-222222222222', email: 'ann@example.com', display_name: '', role: 'agent', timezone_override: null, deactivated_at: '2026-10-15T10:00:00Z', last_active_at: null, open_conversations: 0 };
  fetchMock.mockResolvedValue(reply(200, member));
  const result = await api.updateAgent(member.id, { active: false, reassign_to: '33333333-3333-4333-8333-333333333333' });
  expect(result.deactivated_at).toBe('2026-10-15T10:00:00Z');
  const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(path).toBe(`/api/v1/agents/${member.id}`);
  expect(init.method).toBe('PATCH');
  expect(JSON.parse(init.body as string)).toEqual({ active: false, reassign_to: '33333333-3333-4333-8333-333333333333' });
});

test('a 409 from the team keeps the server explanation as a ConflictError', async () => {
  fetchMock.mockResolvedValue(reply(409, apiError('last_admin', 'A workspace needs at least one active admin.')));
  const error = await api.updateAgent('22222222-2222-4222-8222-222222222222', { active: false }).catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(ConflictError);
  expect((error as Error).message).toBe('A workspace needs at least one active admin.');
});

test('a reply with files is posted as multipart without a JSON content type', async () => {
  fetchMock.mockResolvedValue(reply(201, { id: '55555555-5555-4555-8555-555555555555', conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null, agent_token: null, agent_request_id: null, kind: 'outgoing', body: 'Hi', body_html: null, origin: null, attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', delivery: null, email: null }));
  const pdf = new File(['%PDF'], 'invoice é.pdf', { type: 'application/pdf' });
  await api.createMessage('11111111-1111-4111-8111-111111111111', 'outgoing', 'Hi', [pdf]);
  const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(init.method).toBe('POST');
  expect(init.body).toBeInstanceOf(FormData);
  const form = init.body as FormData;
  expect(form.get('kind')).toBe('outgoing');
  expect(form.get('body')).toBe('Hi');
  expect((form.getAll('files')[0] as File).name).toBe('invoice é.pdf');
  expect(new Headers(init.headers).has('Content-Type')).toBe(false);
});

test('a reply without files stays a JSON request', async () => {
  fetchMock.mockResolvedValue(reply(201, { id: '55555555-5555-4555-8555-555555555555', conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null, agent_token: null, agent_request_id: null, kind: 'note', body: 'Hi', body_html: null, origin: null, attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', delivery: null, email: null }));
  await api.createMessage('11111111-1111-4111-8111-111111111111', 'note', 'Hi');
  const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(init.body).toBe(JSON.stringify({ kind: 'note', body: 'Hi' }));
  expect(new Headers(init.headers).get('Content-Type')).toBe('application/json');
});

test('413 on a message becomes an error carrying the server reason', async () => {
  fetchMock.mockResolvedValue(reply(413, apiError('attachments_too_large', 'The files of one message can be at most 10 MiB in total. Nothing was sent or stored.')));
  const error = await api.createMessage('11111111-1111-4111-8111-111111111111', 'outgoing', 'Hi', [new File(['x'], 'x')]).catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(AttachmentsTooLargeError);
  expect((error as Error).message).toMatch(/at most 10 MiB/);
});

test.each([
  [403, 'storage_limit_reached', 'This workspace has reached its 2 GiB attachment storage limit.'],
  [507, 'storage_full', 'The server is low on disk space for attachments. Nothing was sent or stored.'],
])('%i %s on a message becomes an error carrying the server reason', async (status, code, message) => {
  fetchMock.mockResolvedValue(reply(status, apiError(code, message)));
  const error = await api.createMessage('11111111-1111-4111-8111-111111111111', 'outgoing', 'Hi', [new File(['x'], 'x')]).catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(AttachmentStorageError);
  expect((error as Error).message).toBe(message);
});

test('a trial refusal on a message is still an outbound refusal, not a storage error', async () => {
  fetchMock.mockResolvedValue(reply(403, apiError('trial_outbound_restricted', 'Verify your domain.')));
  const error = await api.createMessage('11111111-1111-4111-8111-111111111111', 'outgoing', 'Hi').catch((caught: unknown) => caught);
  expect(error).not.toBeInstanceOf(AttachmentStorageError);
});

test('a trial refusal of copied addresses keeps the list of refused addresses', async () => {
  fetchMock.mockResolvedValue(reply(403, { error: { code: 'trial_outbound_restricted', message: 'Not allowed.', refused_cc: ['one@elsewhere.example', 'two@elsewhere.example'] } }));
  const error = await api.createMessage('11111111-1111-4111-8111-111111111111', 'outgoing', 'Hi', [], ['one@elsewhere.example']).catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(OutboundRefusedError);
  expect((error as OutboundRefusedError).refusedCc).toEqual(['one@elsewhere.example', 'two@elsewhere.example']);
});

test('a reply that copies people sends one JSON list, and a note sends none', async () => {
  fetchMock.mockResolvedValue(reply(201, { id: '55555555-5555-4555-8555-555555555555', conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null, agent_token: null, agent_request_id: null, kind: 'outgoing', body: 'Hi', body_html: null, origin: null, attachments: [], cc: ['boss@customer.example'], created_at: '2026-10-14T00:00:00Z', delivery: null, email: null }));
  const message = await api.createMessage('11111111-1111-4111-8111-111111111111', 'outgoing', 'Hi', [], ['boss@customer.example']);
  const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(init.body).toBe(JSON.stringify({ kind: 'outgoing', body: 'Hi', cc: ['boss@customer.example'] }));
  expect(message.cc).toEqual(['boss@customer.example']);
});

test('a reply with files and copies sends one cc field per address, before the files', async () => {
  fetchMock.mockResolvedValue(reply(201, { id: '55555555-5555-4555-8555-555555555555', conversation_id: '11111111-1111-4111-8111-111111111111', author_agent_id: null, author_name: null, agent_token: null, agent_request_id: null, kind: 'outgoing', body: 'Hi', body_html: null, origin: null, attachments: [], cc: [], created_at: '2026-10-14T00:00:00Z', delivery: null, email: null }));
  await api.createMessage('11111111-1111-4111-8111-111111111111', 'outgoing', 'Hi', [new File(['x'], 'x.txt')], ['a@customer.example', 'b@customer.example']);
  const form = ((fetchMock.mock.calls[0] as [string, RequestInit])[1]).body as FormData;
  expect(form.getAll('cc')).toEqual(['a@customer.example', 'b@customer.example']);
  expect([...form.keys()]).toEqual(['kind', 'body', 'cc', 'cc', 'files']);
});

test('the cloud exchange names this device by its user agent, like a pairing link', async () => {
  fetchMock.mockResolvedValueOnce(reply(403, apiError('forbidden')));
  await api.exchangeCloudSession('t', 'acme').catch(() => {});
  expect(JSON.parse(fetchMock.mock.calls[0][1].body as string)).toEqual({ label: navigator.userAgent, workspace_slug: 'acme' });
});


test('a trial refusal for a merged address carries the address the reply would go to', async () => {
  fetchMock.mockResolvedValue(reply(403, { error: { code: 'trial_outbound_restricted', message: 'Not allowed.', reason: 'contact_address_differs', reply_to: 'dana@work.example' } }));
  const error = await api.createMessage('33333333-3333-4333-8333-333333333333', 'outgoing', 'Hi').catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(OutboundRefusedError);
  expect((error as OutboundRefusedError).replyTo).toBe('dana@work.example');
});

test('402 seat_paused becomes a SeatPausedError with the server\'s explanation', async () => {
  fetchMock.mockResolvedValue(reply(402, apiError('seat_paused', 'Your access is paused.')));
  const error = await api.tags().catch((caught: unknown) => caught);
  expect(error).toBeInstanceOf(SeatPausedError);
  expect(error).not.toBeInstanceOf(WorkspaceLockedError);
  expect((error as Error).message).toBe('Your access is paused.');
});
