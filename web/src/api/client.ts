import type { z } from 'zod';
import { AttachmentsTooLargeError, AttachmentStorageError } from './attachments-error';
import { NotAnAgentError, signUpCloud } from './cloud-signup';
import { OutboundRefusedError } from './outbound-error';
import { requestExportLink } from './workspace-export';
import { recordFailedCall } from '../lib/diagnostics';
import type { operations } from './generated';
import {
  agentListSchema, agentSchema, contactDetailSchema, contactListSchema, contactSchema, conversationPageSchema, conversationSchema, inboxActivitySchema,
  cloudWorkspaceChoicesSchema, emailChannelSettingsSchema, inboundSecretSchema, webFormSettingsSchema,
  inviteCreatedSchema, inviteListSchema, meSchema, messageListSchema, messageSchema, timeEntryListSchema,
  agentRequestListSchema, agentRequestSchema, approvedAgentRequestSchema, billingLinkSchema, bulkUpdateResultSchema, billingSchema, publicConfigSchema, redeemedSchema, savedReplyListSchema, savedReplySchema, savedReplyVersionListSchema, trashedSavedReplyListSchema, sessionListSchema, tagListSchema, tagSchema, teamMemberSchema, workspaceSchema,
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

// ConflictError is the same refusal for a resource whose state does not allow the action (HTTP 409),
// for example removing the last admin.
export class ConflictError extends InvalidRequestError {
  constructor(message: string) {
    super(message);
    this.name = 'ConflictError';
  }
}

// ChangedElsewhereError means someone saved the same saved reply after this client read it (HTTP 409).
export class ChangedElsewhereError extends ConflictError {
  constructor(message: string) {
    super(message);
    this.name = 'ChangedElsewhereError';
  }
}

async function invalidRequest(response: Response): Promise<Error> {
  try {
    const body = await response.json() as { error?: { code?: unknown; message?: unknown } };
    if (typeof body.error?.message === 'string' && body.error.message) {
      if (response.status === 409 && body.error.code === 'changed_elsewhere') return new ChangedElsewhereError(body.error.message);
      return response.status === 409 ? new ConflictError(body.error.message) : new InvalidRequestError(body.error.message);
    }
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
    const body = await response.json() as { error?: { code?: unknown; message?: unknown; retry_after_seconds?: unknown; refused_cc?: unknown } };
    const { code, message, retry_after_seconds: retry, refused_cc: refused } = body.error ?? {};
    if (code === 'storage_limit_reached' && typeof message === 'string' && message) return new AttachmentStorageError(message);
    if (code === 'trial_outbound_restricted' || code === 'trial_outbound_limit') {
      const refusedCc = Array.isArray(refused) ? refused.filter((item): item is string => typeof item === 'string') : [];
      return new OutboundRefusedError(code, typeof message === 'string' ? message : '', typeof retry === 'number' ? retry : undefined, refusedCc);
    }
  } catch {
    // An unreadable body is an ordinary failure.
  }
  return failure(response.status);
}

async function tooLarge(response: Response): Promise<Error> {
  const error = await invalidRequest(response);
  return error instanceof InvalidRequestError ? new AttachmentsTooLargeError(error.message) : error;
}

async function diskFull(response: Response): Promise<Error> {
  const error = await invalidRequest(response);
  return error instanceof InvalidRequestError ? new AttachmentStorageError(error.message) : error;
}

// noteFailure keeps a failed call, with the id the server gave it, for "Report a problem".
function noteFailure(path: string, init: RequestInit | undefined, response: Response): void {
  recordFailedCall(init?.method ?? 'GET', path, response.status, response.headers.get('X-Request-Id') ?? '');
}

// request is exported for the API modules that load with a lazy part of the app (saved-views.ts).
export async function request<T>(path: string, schema: z.ZodType<T>, init?: RequestInit): Promise<T> {
  // A FormData body sets its own multipart Content-Type, with the boundary.
  const headers = init?.body instanceof FormData ? init.headers : { 'Content-Type': 'application/json', ...init?.headers };
  const response = await fetch(path, { credentials: 'same-origin', ...init, headers });
  if (!response.ok) noteFailure(path, init, response);
  if (response.status === 402) throw await planLimitFailure(response);
  if (response.status === 413) throw await tooLarge(response);
  if (response.status === 507) throw await diskFull(response);
  if (response.status === 400 || response.status === 409) throw await invalidRequest(response);
  if (response.status === 403 || response.status === 429) throw await outboundRefusal(response);
  if (!response.ok) throw failure(response.status);
  return schema.parse(await response.json());
}

async function requestNoContent(path: string, init: RequestInit): Promise<void> {
  const response = await fetch(path, { credentials: 'same-origin', ...init });
  if (!response.ok) noteFailure(path, init, response);
  if (!response.ok) throw failure(response.status);
}

export type RequestBody<Operation extends keyof operations> =
  operations[Operation] extends { requestBody: { content: { 'application/json': infer Body } } } ? Body : never;
export type UpdateConversationRequest = RequestBody<'updateConversation'>;
export type BulkConversationChanges = RequestBody<'bulkUpdateConversations'>['changes'];

const json = <Operation extends keyof operations>(value: RequestBody<Operation>): string => JSON.stringify(value);

// The JSON body of an operation whose body is optional.
type OptionalBody<Operation extends keyof operations> =
  NonNullable<operations[Operation]['requestBody']> extends { content: { 'application/json': infer Body } } ? Body : never;
const optionalJson = <Operation extends keyof operations>(value: OptionalBody<Operation>): string => JSON.stringify(value);

function messageForm(kind: string, body: string, files: File[], cc: string[]): FormData {
  const form = new FormData();
  form.append('kind', kind);
  form.append('body', body);
  // The server reads the copied addresses before the files, so a refusal costs no upload.
  for (const address of cc) form.append('cc', address);
  for (const file of files) form.append('files', file, file.name);
  return form;
}

export class CloudWorkspaceChoiceError extends Error {
  readonly workspaces: WorkspaceChoice[];

  constructor(workspaces: WorkspaceChoice[]) {
    super('Choose a workspace');
    this.name = 'CloudWorkspaceChoiceError';
    this.workspaces = workspaces;
  }
}

async function exchangeCloudSession(token: string, workspaceSlug?: string) {
  // The user agent names this device in Settings → Sessions, as it does for a pairing link.
  const body: RequestBody<'exchangeCloudSession'> = { label: navigator.userAgent, ...(workspaceSlug ? { workspace_slug: workspaceSlug } : {}) };
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
  requestExportLink,
  logout: () => requestNoContent('/api/v1/auth/logout', { method: 'POST' }),
  redeem: (token: string, label: string) => request('/api/v1/auth/redeem', redeemedSchema, {
    method: 'POST', body: json<'redeemLink'>({ token, label, session_type: 'cookie' }),
  }),
  me: () => request('/api/v1/auth/me', meSchema),
  updateWorkspaceTimezone: (timezone: RequestBody<'updateWorkspaceTimezone'>['timezone']) => request('/api/v1/workspace/timezone', workspaceSchema, { method: 'PATCH', body: json<'updateWorkspaceTimezone'>({ timezone }) }),
  updateWorkspaceSlug: (slug: RequestBody<'updateWorkspaceSlug'>['slug']) => request('/api/v1/workspace/slug', workspaceSchema, { method: 'PATCH', body: json<'updateWorkspaceSlug'>({ slug }) }),
  updateAgentTimezone: (timezone: RequestBody<'updateAgentTimezone'>['timezone']) => request('/api/v1/agents/me/timezone', agentSchema, { method: 'PATCH', body: json<'updateAgentTimezone'>({ timezone }) }),
  updateAgentDisplayName: (display_name: RequestBody<'updateAgentDisplayName'>['display_name']) => request('/api/v1/agents/me/display-name', agentSchema, { method: 'PATCH', body: json<'updateAgentDisplayName'>({ display_name }) }),
  updateAgent: (id: string, value: RequestBody<'updateAgent'>) => request(`/api/v1/agents/${id}`, teamMemberSchema, { method: 'PATCH', body: json<'updateAgent'>(value) }),
  sessions: () => request('/api/v1/auth/sessions', sessionListSchema),
  revokeSession: (id: string) => requestNoContent(`/api/v1/auth/sessions/${id}`, { method: 'DELETE' }),
  invites: () => request('/api/v1/auth/invites', inviteListSchema),
  createInvite: (email: string, role_ceiling: RequestBody<'createInvite'>['role_ceiling']) => request('/api/v1/auth/invites', inviteCreatedSchema, { method: 'POST', body: json<'createInvite'>({ email, role_ceiling }) }),
  revokeInvite: (id: string) => requestNoContent(`/api/v1/auth/invites/${id}`, { method: 'DELETE' }),
  // signal lets a newer search cancel the request of the one it replaced.
  conversations: (query: string, signal?: AbortSignal) => request(`/api/v1/conversations?${query}`, conversationPageSchema, { signal }),
  inboxActivity: (since?: string) => request(`/api/v1/inbox/activity${since ? `?since=${encodeURIComponent(since)}` : ''}`, inboxActivitySchema),
  createConversation: (contact_id: string, subject: string) => request('/api/v1/conversations', conversationSchema, { method: 'POST', body: json<'createConversation'>({ contact_id, subject }) }),
  conversation: (id: string) => request(`/api/v1/conversations/${id}`, conversationSchema),
  updateConversation: (id: string, value: UpdateConversationRequest) => request(`/api/v1/conversations/${id}`, conversationSchema, { method: 'PATCH', body: json<'updateConversation'>(value) }),
  bulkUpdateConversations: (ids: string[], changes: BulkConversationChanges) => request('/api/v1/conversations', bulkUpdateResultSchema, { method: 'PATCH', body: json<'bulkUpdateConversations'>({ ids, changes }) }),
  addConversationTag: (id: string, tagId: string) => request(`/api/v1/conversations/${id}/tags/${tagId}`, conversationSchema, { method: 'PUT' }),
  removeConversationTag: (id: string, tagId: string) => request(`/api/v1/conversations/${id}/tags/${tagId}`, conversationSchema, { method: 'DELETE' }),
  messages: (id: string) => request(`/api/v1/conversations/${id}/messages`, messageListSchema),
  timeEntries: (id: string) => request(`/api/v1/conversations/${id}/time-entries`, timeEntryListSchema),
  createMessage: (id: string, kind: RequestBody<'createMessage'>['kind'], body: string, files: File[] = [], cc: string[] = []) =>
    request(`/api/v1/conversations/${id}/messages`, messageSchema, { method: 'POST',
      body: files.length > 0 ? messageForm(kind, body, files, cc) : json<'createMessage'>(cc.length > 0 ? { kind, body, cc } : { kind, body }) }),
  contacts: (search = '') => request(`/api/v1/contacts?search=${encodeURIComponent(search)}`, contactListSchema),
  contact: (id: string) => request(`/api/v1/contacts/${id}`, contactDetailSchema),
  createContact: (name: string, email: string) => request('/api/v1/contacts', contactSchema, { method: 'POST', body: json<'createContact'>({ name, email }) }),
  agents: () => request('/api/v1/agents', agentListSchema),
  tags: () => request('/api/v1/tags', tagListSchema),
  createTag: (name: string) => request('/api/v1/tags', tagSchema, { method: 'POST', body: json<'createTag'>({ name }) }),
  savedReplies: () => request('/api/v1/saved-replies', savedReplyListSchema),
  createSavedReply: (title: string, body: string) => request('/api/v1/saved-replies', savedReplySchema, { method: 'POST', body: json<'createSavedReply'>({ title, body }) }),
  updateSavedReply: (id: string, value: RequestBody<'updateSavedReply'>) => request(`/api/v1/saved-replies/${id}`, savedReplySchema, { method: 'PATCH', body: json<'updateSavedReply'>(value) }),
  // updatedAt is the version the person saw; the server refuses the delete if the reply was saved since.
  deleteSavedReply: (id: string, updatedAt: string) => requestNoContent(`/api/v1/saved-replies/${id}?${new URLSearchParams({ updated_at: updatedAt })}`, { method: 'DELETE' }),
  trashedSavedReplies: (cursor?: string) => request(`/api/v1/saved-replies/trash${cursor ? `?${new URLSearchParams({ cursor })}` : ''}`, trashedSavedReplyListSchema),
  restoreSavedReply: (id: string, updatedAt: string) => request(`/api/v1/saved-replies/${id}/restore`, savedReplySchema,
    { method: 'POST', body: json<'restoreSavedReply'>({ updated_at: updatedAt }) }),
  savedReplyVersions: (id: string, beforeVersion?: number) => request(`/api/v1/saved-replies/${id}/versions${beforeVersion ? `?before_version=${beforeVersion}` : ''}`, savedReplyVersionListSchema),
  restoreSavedReplyVersion: (id: string, version: number, updatedAt: string) => request(`/api/v1/saved-replies/${id}/versions/${version}/restore`, savedReplySchema,
    { method: 'POST', body: json<'restoreSavedReplyVersion'>({ updated_at: updatedAt }) }),
  // Without full a request carries no body: the poll every tab runs stays small.
  agentRequests: (status?: NonNullable<operations['listAgentRequests']['parameters']['query']>['status'], full = false) => {
    const query = new URLSearchParams();
    if (status) query.set('status', status);
    if (full) query.set('detail', 'full');
    const search = query.toString();
    return request(`/api/v1/agent-requests${search ? `?${search}` : ''}`, agentRequestListSchema);
  },
  agentRequest: (id: string) => request(`/api/v1/agent-requests/${id}`, agentRequestSchema),
  // Approving runs the agent's call as the signed-in person. A reply draft may come with its edited
  // text and the copies to keep (never new ones).
  approveAgentRequest: (id: string, edit: OptionalBody<'approveAgentRequest'> = {}) => request(`/api/v1/agent-requests/${id}/approve`, approvedAgentRequestSchema,
    { method: 'POST', body: optionalJson<'approveAgentRequest'>(edit) }),
  rejectAgentRequest: (id: string, reason?: string) => request(`/api/v1/agent-requests/${id}/reject`, agentRequestSchema,
    { method: 'POST', body: optionalJson<'rejectAgentRequest'>(reason ? { reason } : {}) }),
  cancelAgentRequest: (id: string) => request(`/api/v1/agent-requests/${id}/cancel`, agentRequestSchema, { method: 'POST' }),
};
