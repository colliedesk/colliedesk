import type { z } from 'zod';
import { NotAnAgentError, signUpCloud } from './cloud-signup';
import { OutboundRefusedError } from './outbound-error';
import { exportWorkspace } from './workspace-export';
import type { operations } from './generated';
import {
  agentListSchema, agentSchema, contactDetailSchema, contactListSchema, contactSchema, conversationPageSchema, conversationSchema,
  cloudWorkspaceChoicesSchema, emailChannelSettingsSchema, inboundSecretSchema, webFormSettingsSchema,
  inviteCreatedSchema, inviteListSchema, meSchema, messageListSchema, messageSchema, timeEntryListSchema,
  billingLinkSchema, billingSchema, publicConfigSchema, redeemedSchema, sessionListSchema, tagListSchema, tagSchema, workspaceSchema,
} from './schemas';
import type { WorkspaceChoice } from './schemas';

// UnauthorizedError means the browser has no valid session: never signed in, expired, or revoked.
export class UnauthorizedError extends Error {
  constructor() {
    super('API 401');
    this.name = 'UnauthorizedError';
  }
}

// PlanLimitError means the workspace's plan does not allow the action (HTTP 402). Its message is
// the server's explanation, written for the person who sees it.
export class PlanLimitError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'PlanLimitError';
  }
}

// WorkspaceLockedError is the 402 `workspace_locked` answer: the trial or subscription ended, so the
// app shows the locked screen instead of the view that asked.
export class WorkspaceLockedError extends PlanLimitError {
  constructor(message: string) {
    super(message);
    this.name = 'WorkspaceLockedError';
  }
}

// InvalidRequestError means the server refused the input (HTTP 400); its message says what to fix.
export class InvalidRequestError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'InvalidRequestError';
  }
}

async function invalidRequest(response: Response): Promise<Error> {
  try {
    const body = await response.json() as { error?: { message?: unknown } };
    if (typeof body.error?.message === 'string' && body.error.message) return new InvalidRequestError(body.error.message);
  } catch {
    // An unreadable body is an ordinary failure.
  }
  return failure(response.status);
}

function failure(status: number): Error {
  return status === 401 ? new UnauthorizedError() : new Error(`API ${status}`);
}

async function planLimitFailure(response: Response): Promise<PlanLimitError> {
  try {
    const body = await response.json() as { error?: { code?: unknown; message?: unknown } };
    if (typeof body.error?.message === 'string' && body.error.message) {
      return body.error.code === 'workspace_locked' ? new WorkspaceLockedError(body.error.message) : new PlanLimitError(body.error.message);
    }
    if (body.error?.code === 'workspace_locked') return new WorkspaceLockedError('This workspace is locked.');
  } catch {
    // An unreadable body still means the plan refused the action.
  }
  return new PlanLimitError('This action is not included in your plan.');
}

async function outboundRefusal(response: Response): Promise<Error> {
  try {
    const body = await response.json() as { error?: { code?: unknown; message?: unknown; retry_after_seconds?: unknown } };
    const { code, message, retry_after_seconds: retry } = body.error ?? {};
    if (code === 'trial_outbound_restricted' || code === 'trial_outbound_limit') {
      return new OutboundRefusedError(code, typeof message === 'string' ? message : '', typeof retry === 'number' ? retry : undefined);
    }
  } catch {
    // An unreadable body is an ordinary failure.
  }
  return failure(response.status);
}

async function request<T>(path: string, schema: z.ZodType<T>, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  });
  if (response.status === 402) throw await planLimitFailure(response);
  if (response.status === 400) throw await invalidRequest(response);
  if (response.status === 403 || response.status === 429) throw await outboundRefusal(response);
  if (!response.ok) throw failure(response.status);
  return schema.parse(await response.json());
}

async function requestNoContent(path: string, init: RequestInit): Promise<void> {
  const response = await fetch(path, { credentials: 'same-origin', ...init });
  if (!response.ok) throw failure(response.status);
}

export type RequestBody<Operation extends keyof operations> =
  operations[Operation] extends { requestBody: { content: { 'application/json': infer Body } } } ? Body : never;
export type UpdateConversationRequest = RequestBody<'updateConversation'>;

const json = <Operation extends keyof operations>(value: RequestBody<Operation>): string => JSON.stringify(value);

export class CloudWorkspaceChoiceError extends Error {
  readonly workspaces: WorkspaceChoice[];

  constructor(workspaces: WorkspaceChoice[]) {
    super('Choose a workspace');
    this.name = 'CloudWorkspaceChoiceError';
    this.workspaces = workspaces;
  }
}

async function exchangeCloudSession(token: string, workspaceSlug?: string) {
  const body: RequestBody<'exchangeCloudSession'> = workspaceSlug ? { workspace_slug: workspaceSlug } : {};
  const response = await fetch('/api/v1/auth/cloud/exchange', {
    method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify(body),
  });
  if (response.status === 409) {
    throw new CloudWorkspaceChoiceError(cloudWorkspaceChoicesSchema.parse(await response.json()).workspaces);
  }
  if (response.status === 403 && (await errorCode(response)) === 'forbidden') throw new NotAnAgentError();
  if (!response.ok) throw new Error(`API ${response.status}`);
  return redeemedSchema.parse(await response.json());
}

async function errorCode(response: Response): Promise<unknown> {
  try {
    return ((await response.json()) as { error?: { code?: unknown } }).error?.code;
  } catch {
    return undefined;
  }
}

export const api = {
  emailChannel: () => request('/api/v1/channels/email', emailChannelSettingsSchema),
  saveEmailChannel: (support_address: string) => request('/api/v1/channels/email', emailChannelSettingsSchema, { method: 'PUT', body: json<'putEmailChannel'>({ support_address }) }),
  disconnectEmailChannel: () => requestNoContent('/api/v1/channels/email', { method: 'DELETE' }),
  webForm: () => request('/api/v1/channels/web-form', webFormSettingsSchema),
  saveWebForm: (enabled: boolean, allowed_origins: string[]) => request('/api/v1/channels/web-form', webFormSettingsSchema, { method: 'PUT', body: json<'putWebForm'>({ enabled, allowed_origins }) }),
  verifyEmailDomain: () => request('/api/v1/channels/email/domain-verification', emailChannelSettingsSchema, { method: 'POST' }),
  rotateEmailSecret: () => request('/api/v1/channels/email/secret', inboundSecretSchema, { method: 'POST' }),
  retryDelivery: (conversationId: string, messageId: string) => request(`/api/v1/conversations/${conversationId}/messages/${messageId}/retry`, messageSchema, { method: 'POST' }),
  config: () => request('/api/v1/config', publicConfigSchema),
  billing: () => request('/api/v1/billing', billingSchema),
  startCheckout: (plan: RequestBody<'createBillingCheckout'>['plan']) => request('/api/v1/billing/checkout', billingLinkSchema, { method: 'POST', body: json<'createBillingCheckout'>({ plan }) }),
  openBillingPortal: () => request('/api/v1/billing/portal', billingLinkSchema, { method: 'POST' }),
  exchangeCloudSession,
  signUpCloud,
  exportWorkspace,
  logout: () => requestNoContent('/api/v1/auth/logout', { method: 'POST' }),
  redeem: (token: string, label: string) => request('/api/v1/auth/redeem', redeemedSchema, {
    method: 'POST', body: json<'redeemLink'>({ token, label, session_type: 'cookie' }),
  }),
  me: () => request('/api/v1/auth/me', meSchema),
  updateWorkspaceTimezone: (timezone: RequestBody<'updateWorkspaceTimezone'>['timezone']) => request('/api/v1/workspace/timezone', workspaceSchema, { method: 'PATCH', body: json<'updateWorkspaceTimezone'>({ timezone }) }),
  updateWorkspaceSlug: (slug: RequestBody<'updateWorkspaceSlug'>['slug']) => request('/api/v1/workspace/slug', workspaceSchema, { method: 'PATCH', body: json<'updateWorkspaceSlug'>({ slug }) }),
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
  timeEntries: (id: string) => request(`/api/v1/conversations/${id}/time-entries`, timeEntryListSchema),
  createMessage: (id: string, kind: RequestBody<'createMessage'>['kind'], body: string) => request(`/api/v1/conversations/${id}/messages`, messageSchema, { method: 'POST', body: json<'createMessage'>({ kind, body }) }),
  contacts: (search = '') => request(`/api/v1/contacts?search=${encodeURIComponent(search)}`, contactListSchema),
  contact: (id: string) => request(`/api/v1/contacts/${id}`, contactDetailSchema),
  createContact: (name: string, email: string) => request('/api/v1/contacts', contactSchema, { method: 'POST', body: json<'createContact'>({ name, email }) }),
  agents: () => request('/api/v1/agents', agentListSchema),
  tags: () => request('/api/v1/tags', tagListSchema),
  createTag: (name: string) => request('/api/v1/tags', tagSchema, { method: 'POST', body: json<'createTag'>({ name }) }),
};
