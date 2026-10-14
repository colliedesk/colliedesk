import { afterEach, expect, expectTypeOf, test, vi } from 'vitest';
import { api, type UpdateConversationRequest } from './client';
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
