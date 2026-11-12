import { routePermissions } from './route-permissions';
import type { z } from 'zod';
import { AttachmentsTooLargeError, AttachmentStorageError } from './attachments-error';
import { NotAnAgentError, signUpCloud } from './cloud-signup';
import { OutboundRefusedError } from './outbound-error';
import type { OutboundRefusalReason } from './outbound-error';
import { requestExportLink } from './workspace-export';
import { recordFailedCall } from '../lib/diagnostics';
import { browserTimeZone } from '../lib/format-date';
import type { operations } from './generated';
import {
  agentListSchema, agentSchema, contactListSchema, contactSchema, conversationPageSchema, conversationSchema, inboxActivitySchema,
  cloudWorkspaceChoicesSchema, consentLinkSchema, channelCountsSchema, emailChannelSettingsSchema, inboundSecretSchema, testMessageResultSchema, webFormSettingsSchema,
  inviteCreatedSchema, inviteListSchema, meSchema, oauthAllowedSchema, oauthClientSchema, setupGuideHiddenSchema, setupProgressSchema, messageListSchema, messageSchema, timeEntryListSchema,
  agentTokenSchema, roleListSchema, roleSchema, billingLinkSchema, bulkUpdateResultSchema, spamResultSchema, billingSchema, foundingSlotsSchema, migrationRequestSchema, trialOfferSchema, trialOfferAcceptedSchema, publicConfigSchema, invitePreviewSchema, redeemedSchema, renamedWorkspaceSchema, savedReplyListSchema, savedReplySchema, savedReplyVersionListSchema, trashedSavedReplyListSchema, sessionListSchema, tagListSchema, tagSchema, teamMemberSchema, workspaceSchema,
} from './schemas';
import type { Message, WorkspaceChoice } from './schemas';

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

// SeatPausedError is the 402 `seat_paused` answer: the workspace moved to a plan with fewer agents
// than it has and this agent is one of the most recently added, so the app shows why instead.
export class SeatPausedError extends PlanLimitError {
  constructor(message: string) {
    super(message);
    this.name = 'SeatPausedError';
  }
}

// InvalidRequestError means the server refused the input (HTTP 400, 409 or 422); its message says what to fix.
// detail (the whole error object, with its code) lets a caller read a refusal of its own, such as
// contact_exists with the contact to use, or the message of each field of a form.
export class InvalidRequestError extends Error {
  readonly detail?: { code?: unknown; [field: string]: unknown };

  constructor(message: string, detail?: { code?: unknown; [field: string]: unknown }) {
    super(message);
    this.name = 'InvalidRequestError';
    this.detail = detail;
  }
}

// PermissionError is a 403 that says which permission the caller lacks (permission_missing), or a role or
// a member beyond theirs; its message says how to get it. It is an InvalidRequestError so forms show it.
export class PermissionError extends InvalidRequestError {
  override name = 'PermissionError';
}

// ConflictError is the same refusal for a resource whose state does not allow the action (HTTP 409),
// for example removing the last admin.
export class ConflictError extends InvalidRequestError {
  override name = 'ConflictError';
}

// ChangedElsewhereError means someone saved the same saved reply after this client read it (HTTP 409).
export class ChangedElsewhereError extends ConflictError {
  override name = 'ChangedElsewhereError';
}

async function invalidRequest(response: Response): Promise<Error> {
  try {
    const body = await response.json() as { error?: { code?: unknown; message?: unknown }; current?: unknown };
    if (typeof body.error?.message === 'string' && body.error.message) {
      // The version saved meanwhile comes with it where the route sends it (response targets), so the page can show it.
      if (response.status === 409 && body.error.code === 'changed_elsewhere') return new ChangedElsewhereError(body.error.message, { ...body.error, current: body.current });
      // The replies are only shown, as text, so they are taken as sent.
      if (response.status === 409 && body.error.code === 'reply_conflict') {
        const { latest_reply_number: latest, newer_replies: newer } = body.error as { latest_reply_number?: number; newer_replies?: Message[] };
        return new ReplyConflictError(body.error.message, latest ?? 0, newer ?? []);
      }
      // AI provider checks and form refusals both retain the server's field details.
      return new (response.status === 409 || body.error.code === 'ai_check_failed' ? ConflictError : InvalidRequestError)(body.error.message, body.error);
    }
  } catch {
    // An unreadable body is an ordinary failure.
  }
  return failure(response.status);
}

// ReplyConflictError refuses a reply because someone replied after the sender started writing (409
// reply_conflict): nothing was sent; newer are those replies, latest the number to send again with.
export class ReplyConflictError extends Error {
  readonly latest: number;
  readonly newer: Message[];
  constructor(message: string, latest: number, newer: Message[]) {
    super(message);
    this.name = 'ReplyConflictError';
    this.latest = latest;
    this.newer = newer;
  }
}

function failure(status: number): Error {
  return status === 401 ? new UnauthorizedError() : new Error(`API ${status}`);
}

async function planLimitFailure(response: Response): Promise<PlanLimitError> {
  try {
    const body = await response.json() as { error?: { code?: unknown; message?: unknown } };
    if (typeof body.error?.message === 'string' && body.error.message) {
      if (body.error.code === 'workspace_locked') return new WorkspaceLockedError(body.error.message);
      return body.error.code === 'seat_paused' ? new SeatPausedError(body.error.message) : new PlanLimitError(body.error.message);
    }
    if (body.error?.code === 'workspace_locked') return new WorkspaceLockedError('This workspace is locked.');
  } catch {
    // An unreadable body still means the plan refused the action.
  }
  return new PlanLimitError('This action is not included in your plan.');
}

const refusalReasons: readonly OutboundRefusalReason[] = ['contact_address_differs', 'mail_not_via_hosted_address', 'sender_authentication_failed'];
const refusalReason = (value: unknown) => refusalReasons.find((known) => known === value);

async function outboundRefusal(response: Response): Promise<Error> {
  try {
    const body = await response.json() as { error?: { code?: unknown; message?: unknown; retry_after_seconds?: unknown; refused_cc?: unknown; reply_to?: unknown; reason?: unknown } };
    const { code, message, retry_after_seconds: retry, refused_cc: refused, reply_to: replyTo, reason } = body.error ?? {};
    // A permission the caller lacks, a role beyond theirs, a member who outranks them: the message says
    // what is missing and how to get it, and detail keeps missing_permissions and ask.
    if (['permission_missing', 'role_exceeds_yours', 'member_outranks_you', 'person_only'].includes(String(code)) && typeof message === 'string' && message)
      return new PermissionError(message, body.error as { code?: unknown; [field: string]: unknown });
    if (code === 'storage_limit_reached' && typeof message === 'string' && message) return new AttachmentStorageError(message);
    // The knowledge base's video uploads: the answer says which upload holds the place, or whose it is.
    if (['too_many_uploads', 'not_your_upload', 'server_busy', 'ai_checks_rate_limited', 'ai_drafts_rate_limited', 'rate_limited'].includes(String(code)) && typeof message === 'string' && message)
      return new ConflictError(message, { code });
    if (code === 'trial_outbound_restricted' || code === 'trial_outbound_limit') {
      const refusedCc = Array.isArray(refused) ? refused.filter((item): item is string => typeof item === 'string') : [];
      return new OutboundRefusedError(code, typeof message === 'string' ? message : '', typeof retry === 'number' ? retry : undefined, refusedCc,
        typeof replyTo === 'string' && replyTo ? replyTo : undefined, refusalReason(reason));
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

// request is exported for the API calls that live in a lazy part of the app (features/contacts/contact-api.ts, saved-views.ts).
// The session's permissions, from GET /api/v1/auth/me: a read they do not allow is not sent, so the server
// never answers it with a 403. undefined until the session is known; null when there is none to go by.
let held: ReadonlySet<string> | null | undefined;
let waiting: Array<() => void> = [];
// The permissions the session had when it was last known, kept across page loads: until the session
// answers, a read they allowed goes out at once, with the session request, instead of a round trip later.
// Only a read they did not allow (or none known) waits; a stale list costs at most one refused read.
const rememberedKey = 'colliedesk:permissions';
let remembered: ReadonlySet<string> | null = readRemembered();

function readRemembered(): ReadonlySet<string> | null {
  try {
    const stored = globalThis.localStorage?.getItem(rememberedKey);
    const list: unknown = stored ? JSON.parse(stored) : null;
    return Array.isArray(list) ? new Set(list.filter((name): name is string => typeof name === 'string')) : null;
  } catch {
    return null;
  }
}

function remember(list: readonly string[] | null) {
  remembered = list ? new Set(list) : null;
  try {
    if (list) globalThis.localStorage?.setItem(rememberedKey, JSON.stringify(list));
    else globalThis.localStorage?.removeItem(rememberedKey);
  } catch { /* storage refused (private mode, quota): the reads wait for the session instead */ }
}
const routeMatchers = routePermissions.map(([route, permission]) => [new RegExp(`^${route.replace(/\{[^}]+\}/g, '[^/]+')}$`), permission] as const);

// knowPermissions records the session's permissions; null means there is no session to go by.
export function knowPermissions(list: readonly string[] | null) {
  held = list ? new Set(list) : null;
  remember(list);
  for (const go of waiting.splice(0)) go();
}

// forgetPermissionsKeepingMemory drops the known session but keeps the remembered permissions, as a new
// page load starts (tests).
export function forgetPermissionsKeepingMemory() {
  held = undefined;
  for (const go of waiting.splice(0)) go();
}

// forgetPermissions drops what is known of the session, as when it signs out (and between tests).
export function forgetPermissions() {
  held = undefined;
  remember(null);
  for (const go of waiting.splice(0)) go();
}

// sessionPermissions waits, briefly, for the session to be known: a page asks for its data while the app
// asks who is signed in.
async function sessionPermissions(): Promise<ReadonlySet<string> | null> {
  if (held === undefined) await new Promise<void>((go) => { waiting.push(go); setTimeout(go, 3000); });
  return held ?? null;
}

function permissionOf(path: string): string | undefined {
  const route = path.split('?', 1)[0];
  return routeMatchers.find(([matcher]) => matcher.test(route))?.[1];
}

function refuseUnheld(mine: ReadonlySet<string> | null, permission: string) {
  if (mine && !mine.has(permission)) {
    throw new PermissionError(`Your roles do not include ${permission}.`, { code: 'permission_missing', missing_permissions: [permission], missing_from: 'person' });
  }
}

export async function request<T>(path: string, schema: z.ZodType<T>, init?: RequestInit): Promise<T> {
  // Only a read is skipped: a change goes to the server, which decides it and says why. A known session is
  // checked at once, so a read goes out in the same tick as before.
  const permission = (init?.method ?? 'GET').toUpperCase() === 'GET' ? permissionOf(path) : undefined;
  if (permission && !(held === undefined && remembered?.has(permission))) refuseUnheld(held === undefined ? await sessionPermissions() : held, permission);
  const parsed = await send(path, schema, init);
  if (path === '/api/v1/auth/me') knowPermissions((parsed as { permissions?: string[] }).permissions ?? null);
  return parsed;
}

async function send<T>(path: string, schema: z.ZodType<T>, init?: RequestInit): Promise<T> {
  // A FormData body sets its own multipart Content-Type, with the boundary.
  const headers = init?.body instanceof FormData ? init.headers : { 'Content-Type': 'application/json', ...init?.headers };
  const response = await fetch(path, { credentials: 'same-origin', ...init, headers });
  if (!response.ok) noteFailure(path, init, response);
  // Signed out: there is no session to go by, so nothing waits for one.
  if (response.status === 401 && path === '/api/v1/auth/me') knowPermissions(null);
  if (response.status === 402) throw await planLimitFailure(response);
  if (response.status === 413) throw await tooLarge(response);
  if (response.status === 507) throw await diskFull(response);
  // 422 is a name refused on the hosted cloud (name_not_allowed); its message says what to do.
  if (response.status === 400 || response.status === 409 || response.status === 422) throw await invalidRequest(response);
  if (response.status === 403 || response.status === 429 || response.status === 503) throw await outboundRefusal(response);
  if (!response.ok) throw failure(response.status);
  // 204 has no body: a call that expects one passes a schema that accepts undefined.
  return schema.parse(response.status === 204 ? undefined : await response.json());
}

// trialOfferCall is request for the trial offer page, which explains every refusal itself: a 403 or 404
// keeps its code and message instead of becoming a bare failure.
async function trialOfferCall<T>(path: string, schema: z.ZodType<T>, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { credentials: 'same-origin', ...init, headers: { 'Content-Type': 'application/json', ...init?.headers } });
  if (response.status === 401) throw new UnauthorizedError();
  if (!response.ok) {
    const refused = await invalidRequest(response);
    if (response.status === 403 && refused instanceof InvalidRequestError) throw new PermissionError(refused.message, refused.detail);
    throw refused;
  }
  return schema.parse(await response.json());
}

export async function requestNoContent(path: string, init: RequestInit): Promise<void> {
  const response = await fetch(path, { credentials: 'same-origin', ...init });
  if (!response.ok) noteFailure(path, init, response);
  if (!response.ok) throw failure(response.status);
}

export type RequestBody<Operation extends keyof operations> =
  operations[Operation] extends { requestBody: { content: { 'application/json': infer Body } } } ? Body : never;
export type UpdateConversationRequest = RequestBody<'updateConversation'>;
export type BulkConversationChanges = RequestBody<'bulkUpdateConversations'>['changes'];

export const json = <Operation extends keyof operations>(value: RequestBody<Operation>): string => JSON.stringify(value);

// The JSON body of an operation whose body is optional.
type OptionalBody<Operation extends keyof operations> =
  NonNullable<operations[Operation]['requestBody']> extends { content: { 'application/json': infer Body } } ? Body : never;
const optionalJson = <Operation extends keyof operations>(value: OptionalBody<Operation>): string => JSON.stringify(value);

function messageForm(kind: string, body: string, files: File[], cc: string[], seen?: number, aiDraft?: string, draftFiles?: string[]): FormData {
  const form = new FormData();
  form.append('kind', kind);
  form.append('body', body);
  if (aiDraft) form.append('ai_draft_id', aiDraft);
  // The draft's files the reply keeps; one empty field keeps none, no field keeps them all.
  if (draftFiles) for (const id of draftFiles.length > 0 ? draftFiles : ['']) form.append('ai_draft_file_ids', id);
  // Before the files: a reply refused for a newer one stores none of them.
  if (seen !== undefined) form.append('seen_reply_number', String(seen));
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

async function exchangeCloudSession(token: string, workspaceSlug?: string, next?: string) {
  // The user agent names this device in Settings → Sessions, as it does for a pairing link.
  // The browser's timezone starts a first-time agent on their own clock instead of UTC.
  const body: RequestBody<'exchangeCloudSession'> = { label: navigator.userAgent, ...(workspaceSlug ? { workspace_slug: workspaceSlug } : {}), timezone: browserTimeZone(), ...(next ? { next } : {}) };
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
  // One mailbox of several: the one a conversation's replies leave from.
  emailChannelOf: (id: string) => request(`/api/v1/channels/${encodeURIComponent(id)}/email`, emailChannelSettingsSchema),
  channelCounts: () => request('/api/v1/channels', channelCountsSchema),
  // A test email from the signed-in agent to an email channel (the default one without an id).
  // Every create takes the key of its press (Idempotency-Key, from api/use-create): a repeat with the same key
  // answers what the first one made and creates nothing.
  sendTestMessage: (channelId: string | undefined, key: string) => request(channelId ? `/api/v1/channels/${channelId}/email/test-message` : '/api/v1/channels/email/test-message', testMessageResultSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key } }),
  // Without sender_name the channel keeps its name; a new channel starts with the workspace name.
  saveEmailChannel: (support_address: string) => request('/api/v1/channels/email', emailChannelSettingsSchema, { method: 'PUT', body: json<'putEmailChannel'>({ support_address }) }),
  // Only the name: the server keeps the address, so a stale page cannot move the channel.
  saveEmailSenderName: (sender_name: string) => request('/api/v1/channels/email', emailChannelSettingsSchema, { method: 'PUT', body: json<'putEmailChannel'>({ sender_name }) }),
  webForm: () => request('/api/v1/channels/web-form', webFormSettingsSchema),
  saveWebForm: (enabled: boolean, allowed_origins: string[]) => request('/api/v1/channels/web-form', webFormSettingsSchema, { method: 'PUT', body: json<'putWebForm'>({ enabled, allowed_origins }) }),
  verifyEmailDomain: () => request('/api/v1/channels/email/domain-verification', emailChannelSettingsSchema, { method: 'POST' }),
  rotateEmailSecret: () => request('/api/v1/channels/email/secret', inboundSecretSchema, { method: 'POST' }),
  connectGmail: () => request('/api/v1/channels/email/gmail', consentLinkSchema, { method: 'POST' }),
  disconnectGmail: () => requestNoContent('/api/v1/channels/email/gmail', { method: 'DELETE' }),
  retryDelivery: (conversationId: string, messageId: string) => request(`/api/v1/conversations/${conversationId}/messages/${messageId}/retry`, messageSchema, { method: 'POST' }),
  config: () => request('/api/v1/config', publicConfigSchema),
  billing: () => request('/api/v1/billing', billingSchema),
  startCheckout: (plan: RequestBody<'createBillingCheckout'>['plan']) => request('/api/v1/billing/checkout', billingLinkSchema, { method: 'POST', body: json<'createBillingCheckout'>({ plan }) }),
  openBillingPortal: () => request('/api/v1/billing/portal', billingLinkSchema, { method: 'POST' }),
  foundingSlots: () => request('/api/v1/founding', foundingSlotsSchema),
  requestMigration: (input: RequestBody<'createMigrationRequest'>, key: string) => request('/api/v1/billing/migration-request', migrationRequestSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createMigrationRequest'>(input) }),
  // A trial offer link: what it offers (reading changes nothing), and accepting it. Every refusal keeps its
  // code (trial_offer_not_found, trial_offer_other_workspace, permission_missing, trial_offer_expired…).
  trialOffer: (token: string) => trialOfferCall(`/api/v1/trial-offer?${new URLSearchParams({ token }).toString()}`, trialOfferSchema),
  // key names one press: a retry of it answers the same acceptance instead of "already accepted".
  acceptTrialOffer: (token: string, key: string) => trialOfferCall('/api/v1/trial-offer/accept', trialOfferAcceptedSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'acceptTrialOffer'>({ token }) }),
  exchangeCloudSession,
  signUpCloud,
  requestExportLink,
  logout: () => requestNoContent('/api/v1/auth/logout', { method: 'POST' }),
  // next is the page a sign-in on APP_ORIGIN opens on the workspace's own host (continue_url).
  redeem: (token: string, label: string, next?: string) => request('/api/v1/auth/redeem', redeemedSchema, {
    method: 'POST', body: json<'redeemLink'>({ token, label, session_type: 'cookie', timezone: browserTimeZone(), ...(next ? { next } : {}) }),
  }),
  // What an invite link is, without using it; any other token answers 401 like an invalid link.
  previewInvite: (token: string) => request('/api/v1/auth/invite-preview', invitePreviewSchema, { method: 'POST', body: json<'previewInvite'>({ token }) }),
  me: () => request('/api/v1/auth/me', meSchema),
  // The AI app a consent page shows, and whether redirect_uri is its own (only then may the page go there).
  describeOAuthClient: (client_id: string, redirect_uri: string) =>
    request(`/api/v1/oauth/client?${new URLSearchParams({ client_id, redirect_uri }).toString()}`, oauthClientSchema),
  // Allow on the consent page: the address that takes the app's one-time code back to it.
  // key names one press: a retry of it never makes a second grant.
  allowOAuthClient: (input: RequestBody<'allowOAuthClient'>, key: string) => request('/api/v1/oauth/authorize', oauthAllowedSchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'allowOAuthClient'>(input) }),
  setupProgress: () => request('/api/v1/workspace/setup', setupProgressSchema),
  setSetupGuideHidden: (hidden: RequestBody<'setSetupGuideHidden'>['hidden']) => request('/api/v1/agents/me/setup-guide', setupGuideHiddenSchema, { method: 'PUT', body: json<'setSetupGuideHidden'>({ hidden }) }),
  updateWorkspaceTimezone: (timezone: RequestBody<'updateWorkspaceTimezone'>['timezone']) => request('/api/v1/workspace/timezone', workspaceSchema, { method: 'PATCH', body: json<'updateWorkspaceTimezone'>({ timezone }) }),
  updateWorkspaceName: (name: RequestBody<'updateWorkspaceName'>['name']) => request('/api/v1/workspace/name', workspaceSchema, { method: 'PATCH', body: json<'updateWorkspaceName'>({ name }) }),
  // On the workspace's own host the answer carries continue_url: the desk moved to the new host.
  updateWorkspaceSlug: (slug: RequestBody<'updateWorkspaceSlug'>['slug']) => request('/api/v1/workspace/slug', renamedWorkspaceSchema, {
    method: 'PATCH', body: json<'updateWorkspaceSlug'>({ slug, next: window.location.pathname + window.location.search }) }),
  updateAgentTimezone: (timezone: RequestBody<'updateAgentTimezone'>['timezone']) => request('/api/v1/agents/me/timezone', agentSchema, { method: 'PATCH', body: json<'updateAgentTimezone'>({ timezone }) }),
  updateAgentDisplayName: (display_name: RequestBody<'updateAgentDisplayName'>['display_name']) => request('/api/v1/agents/me/display-name', agentSchema, { method: 'PATCH', body: json<'updateAgentDisplayName'>({ display_name }) }),
  updateAgent: (id: string, value: RequestBody<'updateAgent'>) => request(`/api/v1/agents/${id}`, teamMemberSchema, { method: 'PATCH', body: json<'updateAgent'>(value) }),
  // The whole list of a member's roles.
  setAgentRoles: (id: string, roleIds: string[]) => request(`/api/v1/agents/${id}/roles`, teamMemberSchema, { method: 'PUT', body: json<'setAgentRoles'>({ role_ids: roleIds }) }),
  roles: (deleted = false) => request(`/api/v1/roles${deleted ? '?deleted=true' : ''}`, roleListSchema),
  role: (id: string) => request(`/api/v1/roles/${encodeURIComponent(id)}`, roleSchema),
  createRole: (value: RequestBody<'createRole'>, key: string) => request('/api/v1/roles', roleSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createRole'>(value) }),
  updateRole: (id: string, value: RequestBody<'updateRole'>) => request(`/api/v1/roles/${encodeURIComponent(id)}`, roleSchema, { method: 'PATCH', body: json<'updateRole'>(value) }),
  deleteRole: (id: string) => requestNoContent(`/api/v1/roles/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  // A press of Restore or Undo is named by its key, so a repeat of it restores once.
  restoreRole: (id: string, key: string) => request(`/api/v1/roles/${encodeURIComponent(id)}/restore`, roleSchema, { method: 'POST', headers: { 'Idempotency-Key': key } }),
  sessions: () => request('/api/v1/auth/sessions', sessionListSchema),
  // A token follows a role whose permissions the person all holds (role_id) or holds a fixed list of permissions, never both.
  createAgentToken: (label: string, choice: Omit<RequestBody<'createAgentToken'>, 'label'>, key: string) => request('/api/v1/auth/tokens', agentTokenSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createAgentToken'>({ label, ...choice }) }),
  revokeSession: (id: string) => requestNoContent(`/api/v1/auth/sessions/${id}`, { method: 'DELETE' }),
  invites: () => request('/api/v1/auth/invites', inviteListSchema),
  createInvite: (email: string, roleIds: string[], key: string) => request('/api/v1/auth/invites', inviteCreatedSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createInvite'>({ email, ...(roleIds.length > 0 ? { role_ids: roleIds } : {}) }) }),
  revokeInvite: (id: string) => requestNoContent(`/api/v1/auth/invites/${id}`, { method: 'DELETE' }),
  // signal lets a newer search cancel the request of the one it replaced.
  conversations: (query: string, signal?: AbortSignal) => request(`/api/v1/conversations?${query}`, conversationPageSchema, { signal }),
  inboxActivity: (since?: string) => request(`/api/v1/inbox/activity${since ? `?since=${encodeURIComponent(since)}` : ''}`, inboxActivitySchema),
  createConversation: (contact_id: string, subject: string, key: string) => request('/api/v1/conversations', conversationSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createConversation'>({ contact_id, subject }) }),
  // Read by id, a conversation says for sure which statuses would tell its customer: none is sent when none would.
  conversation: (id: string) => request(`/api/v1/conversations/${id}`, conversationSchema).then((read) => ({ ...read, status_notices: read.status_notices ?? [] })),
  updateConversation: (id: string, value: UpdateConversationRequest) => request(`/api/v1/conversations/${id}`, conversationSchema, { method: 'PATCH', body: json<'updateConversation'>(value) }),
  bulkUpdateConversations: (ids: string[], changes: BulkConversationChanges) => request('/api/v1/conversations', bulkUpdateResultSchema, { method: 'PATCH', body: json<'bulkUpdateConversations'>({ ids, changes }) }),
  // spam true files the conversations in Spam with their senders; false (Not spam) puts them back where they were.
  markSpam: (ids: string[], spam: boolean) => request('/api/v1/conversations/spam', spamResultSchema, { method: 'PUT', body: json<'markConversationsSpam'>({ ids, spam }) }),
  addConversationTag: (id: string, tagId: string) => request(`/api/v1/conversations/${id}/tags/${tagId}`, conversationSchema, { method: 'PUT' }),
  removeConversationTag: (id: string, tagId: string) => request(`/api/v1/conversations/${id}/tags/${tagId}`, conversationSchema, { method: 'DELETE' }),
  messages: (id: string) => request(`/api/v1/conversations/${id}/messages`, messageListSchema),
  // Whether a send under key stored its message in the conversation: a page reloaded mid-send asks before
  // offering the text again. The server waits for a send under the key that is still running.
  sentUnderKey: (id: string, key: string) => request(`/api/v1/conversations/${id}/messages?${new URLSearchParams({ idempotency_key: key })}`, messageListSchema)
    .then((page) => page.items.length > 0),
  timeEntries: (id: string) => request(`/api/v1/conversations/${id}/time-entries`, timeEntryListSchema),
  // seen is the newest reply number the sender had seen when they started writing (409 ReplyConflictError when older).
  // aiDraft names the AI draft a reply was written from.
  // key names this submit (Idempotency-Key): a retry with the same key returns the message the first stored.
  // draftFiles names the files of the draft (aiDraft) the reply keeps; left out, it keeps them all.
  createMessage: (id: string, kind: RequestBody<'createMessage'>['kind'], body: string, files: File[] = [], cc: string[] = [], seen?: number, aiDraft?: string, key?: string,
    draftFiles?: string[]) =>
    request(`/api/v1/conversations/${id}/messages`, messageSchema, { method: 'POST', headers: key ? { 'Idempotency-Key': key } : undefined,
      body: files.length > 0 ? messageForm(kind, body, files, cc, seen, aiDraft, draftFiles)
        : json<'createMessage'>({ kind, body, ...(cc.length > 0 ? { cc } : {}), ...(seen === undefined ? {} : { seen_reply_number: seen }), ...(aiDraft ? { ai_draft_id: aiDraft } : {}),
          ...(aiDraft && draftFiles ? { ai_draft_file_ids: draftFiles } : {}) }) }),
  contacts: (search = '') => request(`/api/v1/contacts?search=${encodeURIComponent(search)}`, contactListSchema),
  createContact: (name: string, email: string, key: string) => request('/api/v1/contacts', contactSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createContact'>({ name, email }) }),
  agents: () => request('/api/v1/agents', agentListSchema),
  tags: () => request('/api/v1/tags', tagListSchema),
  createTag: (name: string, key: string) => request('/api/v1/tags', tagSchema, { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createTag'>({ name }) }),
  savedReplies: () => request('/api/v1/saved-replies', savedReplyListSchema),
  createSavedReply: (title: string, body: string, key: string) => request('/api/v1/saved-replies', savedReplySchema,
    { method: 'POST', headers: { 'Idempotency-Key': key }, body: json<'createSavedReply'>({ title, body }) }),
  updateSavedReply: (id: string, value: RequestBody<'updateSavedReply'>) => request(`/api/v1/saved-replies/${id}`, savedReplySchema, { method: 'PATCH', body: json<'updateSavedReply'>(value) }),
  // updatedAt is the version the person saw; the server refuses the delete if the reply was saved since.
  deleteSavedReply: (id: string, updatedAt: string) => requestNoContent(`/api/v1/saved-replies/${id}?${new URLSearchParams({ updated_at: updatedAt })}`, { method: 'DELETE' }),
  trashedSavedReplies: (cursor?: string) => request(`/api/v1/saved-replies/trash${cursor ? `?${new URLSearchParams({ cursor })}` : ''}`, trashedSavedReplyListSchema),
  restoreSavedReply: (id: string, updatedAt: string) => request(`/api/v1/saved-replies/${id}/restore`, savedReplySchema,
    { method: 'POST', body: json<'restoreSavedReply'>({ updated_at: updatedAt }) }),
  savedReplyVersions: (id: string, beforeVersion?: number) => request(`/api/v1/saved-replies/${id}/versions${beforeVersion ? `?before_version=${beforeVersion}` : ''}`, savedReplyVersionListSchema),
  restoreSavedReplyVersion: (id: string, version: number, updatedAt: string) => request(`/api/v1/saved-replies/${id}/versions/${version}/restore`, savedReplySchema,
    { method: 'POST', body: json<'restoreSavedReplyVersion'>({ updated_at: updatedAt }) }),
};
