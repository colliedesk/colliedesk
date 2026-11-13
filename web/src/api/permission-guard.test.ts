import { afterEach, expect, test, vi } from 'vitest';
import { api, forgetPermissions, forgetPermissionsKeepingMemory, PermissionError } from './client';
import { permissionsOf, rolesOf } from '../test-roles';

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);
afterEach(() => { fetchMock.mockReset(); forgetPermissions(); });

const me = (permissions: string[]) => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'kb@example.com', display_name: '', roles: rolesOf('agent'), timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'acme', name: 'Acme', timezone: 'UTC' }, timezone: 'UTC', token: null, permissions, plan: null,
});
const answer = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
const paths = () => fetchMock.mock.calls.map(([path]) => String(path));

// A read the session's roles do not allow is not sent: the app gets the refusal at once, the server no 403.
test('a read the session may not make is skipped once the session is known', async () => {
  fetchMock.mockImplementation(async (path: string) => (path === '/api/v1/auth/me' ? answer(me(['conversations:read'])) : answer({ items: [] })));
  await api.me();
  const refused = await api.tags().catch((error: unknown) => error);
  expect(refused).toBeInstanceOf(PermissionError);
  expect((refused as PermissionError).detail).toMatchObject({ code: 'permission_missing', missing_permissions: ['tags:read'], missing_from: 'person' });
  expect(paths()).not.toContain('/api/v1/tags');
  await api.agents().catch(() => undefined);
  expect(paths()).not.toContain('/api/v1/agents');
});

test('a read made before the session is known waits for it, then is skipped or sent', async () => {
  forgetPermissions();
  let release!: () => void;
  const meAnswered = new Promise<void>((resolve) => { release = resolve; });
  fetchMock.mockImplementation(async (path: string) => {
    if (path === '/api/v1/auth/me') { await meAnswered; return answer(me(permissionsOf('agent').filter((name) => name !== 'tags:read'))); }
    return answer({ items: [] });
  });
  const tags = api.tags().catch((error: unknown) => error);
  const conversations = api.conversations('status=open').catch((error: unknown) => error);
  const session = api.me();
  release();
  await session;
  expect(await tags).toBeInstanceOf(PermissionError);
  await conversations;
  expect(paths().filter((path) => path.startsWith('/api/v1/conversations'))).toHaveLength(1);
  expect(paths()).not.toContain('/api/v1/tags');
});

test('a change is never skipped: the server decides it and says why', async () => {
  fetchMock.mockImplementation(async (path: string) => (path === '/api/v1/auth/me' ? answer(me(['conversations:read'])) : answer({ id: '33333333-3333-4333-8333-333333333333', name: 'x', created_at: '2026-10-01T00:00:00Z' })));
  await api.me();
  await api.createTag('x', 'key-1').catch(() => undefined);
  expect(paths()).toContain('/api/v1/tags');
});

// A page load knows the permissions the session had last time: a read they allowed leaves with the session
// request instead of a round trip after it; a read they did not allow still waits for the session.
test('a read the remembered permissions allow is sent before the session answers', async () => {
  fetchMock.mockImplementation(async (path: string) => (path === '/api/v1/auth/me' ? answer(me(['conversations:read'])) : answer({ items: [] })));
  await api.me();
  forgetPermissionsKeepingMemory();
  let release!: () => void;
  const meAnswered = new Promise<void>((resolve) => { release = resolve; });
  fetchMock.mockReset().mockImplementation(async (path: string) => {
    if (path === '/api/v1/auth/me') { await meAnswered; return answer(me(['conversations:read'])); }
    return answer({ items: [] });
  });
  const session = api.me();
  const conversations = api.conversations('status=open').catch((error: unknown) => error);
  const tags = api.tags().catch((error: unknown) => error);
  await vi.waitFor(() => expect(paths().some((path) => path.startsWith('/api/v1/conversations'))).toBe(true));
  expect(paths()).not.toContain('/api/v1/tags');
  release();
  await session;
  await conversations;
  expect(await tags).toBeInstanceOf(PermissionError);
  expect(paths()).not.toContain('/api/v1/tags');
});

test('signing out forgets the remembered permissions', async () => {
  fetchMock.mockImplementation(async (path: string) => (path === '/api/v1/auth/me' ? answer(me(['conversations:read'])) : answer({ items: [] })));
  await api.me();
  forgetPermissions();
  expect(localStorage.getItem('colliedesk:permissions')).toBeNull();
});

// A page that goes away while its read still waits for the session cancels that read: it never reaches the network,
// even when the wait ends later (the session becomes known, or is dropped at sign-out).
test('a read cancelled while it waits for the session is never sent', async () => {
  forgetPermissions();
  fetchMock.mockImplementation(async () => answer({ items: [], next_cursor: null }));
  const leaving = new AbortController();
  const read = api.conversations('status=open', leaving.signal).catch((error: unknown) => error);
  leaving.abort();
  forgetPermissions();
  const outcome = await read;
  expect((outcome as Error).name).toBe('AbortError');
  expect(paths()).not.toContain('/api/v1/conversations?status=open');
});
