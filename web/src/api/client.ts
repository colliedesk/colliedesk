import type { z } from 'zod';
import type { operations } from './generated';
import {
  agentListSchema, agentSchema, contactDetailSchema, contactListSchema, contactSchema, conversationPageSchema, conversationSchema,
  inviteCreatedSchema, inviteListSchema, meSchema, messageListSchema, messageSchema,
  redeemedSchema, sessionListSchema, tagListSchema, tagSchema, workspaceSchema,
} from './schemas';

async function request<T>(path: string, schema: z.ZodType<T>, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  });
  if (!response.ok) throw new Error(`API ${response.status}`);
  return schema.parse(await response.json());
}

async function requestNoContent(path: string, init: RequestInit): Promise<void> {
  const response = await fetch(path, { credentials: 'same-origin', ...init });
  if (!response.ok) throw new Error(`API ${response.status}`);
}

export type RequestBody<Operation extends keyof operations> =
  operations[Operation] extends { requestBody: { content: { 'application/json': infer Body } } } ? Body : never;
export type UpdateConversationRequest = RequestBody<'updateConversation'>;

const json = <Operation extends keyof operations>(value: RequestBody<Operation>): string => JSON.stringify(value);

export const api = {
  redeem: (token: string, label: string) => request('/api/v1/auth/redeem', redeemedSchema, {
    method: 'POST', body: json<'redeemLink'>({ token, label, session_type: 'cookie' }),
  }),
  me: () => request('/api/v1/auth/me', meSchema),
  updateWorkspaceTimezone: (timezone: RequestBody<'updateWorkspaceTimezone'>['timezone']) => request('/api/v1/workspace/timezone', workspaceSchema, { method: 'PATCH', body: json<'updateWorkspaceTimezone'>({ timezone }) }),
  updateAgentTimezone: (timezone: RequestBody<'updateAgentTimezone'>['timezone']) => request('/api/v1/agents/me/timezone', agentSchema, { method: 'PATCH', body: json<'updateAgentTimezone'>({ timezone }) }),
  sessions: () => request('/api/v1/auth/sessions', sessionListSchema),
  revokeSession: (id: string) => requestNoContent(`/api/v1/auth/sessions/${id}`, { method: 'DELETE' }),
  invites: () => request('/api/v1/auth/invites', inviteListSchema),
  createInvite: (email: string, role_ceiling: RequestBody<'createInvite'>['role_ceiling']) => request('/api/v1/auth/invites', inviteCreatedSchema, { method: 'POST', body: json<'createInvite'>({ email, role_ceiling }) }),
  revokeInvite: (id: string) => requestNoContent(`/api/v1/auth/invites/${id}`, { method: 'DELETE' }),
  conversations: (query: string) => request(`/api/v1/conversations?${query}`, conversationPageSchema),
  createConversation: (contact_id: string, subject: string) => request('/api/v1/conversations', conversationSchema, { method: 'POST', body: json<'createConversation'>({ contact_id, subject }) }),
  conversation: (id: string) => request(`/api/v1/conversations/${id}`, conversationSchema),
  updateConversation: (id: string, value: UpdateConversationRequest) => request(`/api/v1/conversations/${id}`, conversationSchema, { method: 'PATCH', body: json<'updateConversation'>(value) }),
  addConversationTag: (id: string, tagId: string) => request(`/api/v1/conversations/${id}/tags/${tagId}`, conversationSchema, { method: 'PUT' }),
  removeConversationTag: (id: string, tagId: string) => request(`/api/v1/conversations/${id}/tags/${tagId}`, conversationSchema, { method: 'DELETE' }),
  messages: (id: string) => request(`/api/v1/conversations/${id}/messages`, messageListSchema),
  createMessage: (id: string, kind: RequestBody<'createMessage'>['kind'], body: string) => request(`/api/v1/conversations/${id}/messages`, messageSchema, { method: 'POST', body: json<'createMessage'>({ kind, body }) }),
  contacts: (search = '') => request(`/api/v1/contacts?search=${encodeURIComponent(search)}`, contactListSchema),
  contact: (id: string) => request(`/api/v1/contacts/${id}`, contactDetailSchema),
  createContact: (name: string, email: string) => request('/api/v1/contacts', contactSchema, { method: 'POST', body: json<'createContact'>({ name, email }) }),
  agents: () => request('/api/v1/agents', agentListSchema),
  tags: () => request('/api/v1/tags', tagListSchema),
  createTag: (name: string) => request('/api/v1/tags', tagSchema, { method: 'POST', body: json<'createTag'>({ name }) }),
};
