import { afterEach, expect, expectTypeOf, test, vi } from 'vitest';
import { api, CloudWorkspaceChoiceError, PlanLimitError, UnauthorizedError, type UpdateConversationRequest } from './client';
import type { operations } from './generated';

afterEach(() => vi.unstubAllGlobals());

test('revoke operations reject non-success responses', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }));
  await expect(api.revokeSession('session-id')).rejects.toThrow('API 500');
  await expect(api.revokeInvite('invite-id')).rejects.toThrow('API 500');
});

test('conversation updates use the generated OpenAPI request body type', () => {
  type Generated = operations['updateConversation']['requestBody']['content']['application/json'];
  expectTypeOf<UpdateConversationRequest>().toEqualTypeOf<Generated>();
  expectTypeOf(api.updateConversation).parameter(1).toEqualTypeOf<Generated>();
});

test('cloud exchange exposes workspace choices from a conflict response', async () => {
	vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
		ok: false,
		status: 409,
		json: vi.fn().mockResolvedValue({ workspaces: [{ slug: 'alpha', name: 'Alpha' }, { slug: 'beta', name: 'Beta' }] }),
	}));

	await expect(api.exchangeCloudSession('clerk-session')).rejects.toEqual(expect.objectContaining({
		name: CloudWorkspaceChoiceError.name,
		workspaces: [{ slug: 'alpha', name: 'Alpha' }, { slug: 'beta', name: 'Beta' }],
	}));
});

test('a 401 is reported as a signed-out session, distinct from other failures', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 401, json: vi.fn() }));
  await expect(api.me()).rejects.toBeInstanceOf(UnauthorizedError);
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 403, json: vi.fn() }));
  await expect(api.me()).rejects.not.toBeInstanceOf(UnauthorizedError);
});

test('a 402 carries the server message so the page can say which limit was reached', async () => {
  const message = 'The Free plan allows 3 agents, pending invites included. Upgrade in Settings > Billing, or revoke a pending invite, to add another.';
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 402, json: vi.fn().mockResolvedValue({ error: { code: 'plan_limit_reached', message } }) }));
  await expect(api.createInvite('new@example.com', 'agent')).rejects.toEqual(expect.objectContaining({ name: PlanLimitError.name, message }));
});

test('a 402 without a readable body still reports a plan limit', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 402, json: vi.fn().mockRejectedValue(new Error('not json')) }));
  await expect(api.createInvite('new@example.com', 'agent')).rejects.toBeInstanceOf(PlanLimitError);
});
