import { z } from 'zod';
import { request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated } from '../../api/schemas';

// The Autofix calls and their shapes (contracts/openapi.yaml, the Fix* and Autofix* schemas). They load with the
// Autofix chunk (autofix-chunk.ts), so none of this is in the first bundle.

const date = z.string().datetime();
const uuid = z.string().uuid();

export const fixStateSchema = z.enum(['sending', 'sent', 'working', 'pr_open', 'merged', 'deployed', 'failed', 'cancelled']) satisfies z.ZodType<Generated<'FixState'>>;
export const connectorSchema = z.enum(['claude_issue', 'copilot_issue', 'cursor_api']) satisfies z.ZodType<Generated<'FixConnector'>>;
export const reasonSchema = z.enum(['vendor_auth', 'vendor_repo_access', 'vendor_rejected', 'provider_unavailable', 'no_repo_access', 'copilot_unavailable',
  'repo_public', 'pr_closed', 'agent_error', 'timed_out', 'interrupted', 'cancelled_by_person', 'authorization_abandoned', 'authorization_denied',
  'repo_disconnected', 'conversation_deleted', 'issue_closed', 'connector_disabled']) satisfies z.ZodType<Generated<'FixOutcomeReason'>>;
export const fixGroupSchema = z.enum(['needs_person', 'running', 'done']) satisfies z.ZodType<Generated<'FixGroup'>>;

export const fixSchema = z.object({
  id: uuid, conversation_id: uuid, attempt_no: z.number().int(), previous_attempt_id: uuid.nullable(), repo_id: uuid.nullable(), repo_full_name: z.string(),
  connector: connectorSchema, state: fixStateSchema, outcome_reason: reasonSchema.nullable(), state_changed_at: date, row_version: z.number().int(),
  stale: z.boolean(), group: fixGroupSchema, sent_title: z.string(), sent_message_ids: z.array(uuid), sent_notes: z.boolean(),
  created_by_kind: z.enum(['person', 'token']), created_by_agent_id: uuid.nullable(), created_by_name: z.string(), approved_request_id: uuid.nullable(),
  github_login: z.string().nullable(), issue_number: z.number().int().nullable(), issue_url: z.string().nullable(), vendor_url: z.string().nullable(),
  branch: z.string().nullable(), pr_number: z.number().int().nullable(), pr_url: z.string().nullable(), pr_linked_by: z.enum(['auto', 'person']).nullable(),
  merge_sha: z.string().nullable(), merged_at: date.nullable(), merged_by_login: z.string().nullable(), deployed_at: date.nullable(),
  deployed_by_name: z.string().nullable(), created_at: date,
  conversation_subject: z.string(), contact_name: z.string().nullable(),
  // Copilot assigned by Colliedesk, or waiting for a person to assign it on GitHub (the workspace's fallback mode).
  copilot_assignment: z.enum(['assigned', 'on_github']).nullable(),
}) satisfies z.ZodType<Generated<'Fix'>>;

export const fixEventSchema = z.object({
  id: z.number().int(), kind: z.string(), from_state: fixStateSchema.nullable(), to_state: fixStateSchema.nullable(),
  actor_kind: z.enum(['person', 'token', 'github', 'cursor', 'system']), actor_name: z.string().nullable(), data: z.record(z.string(), z.unknown()), created_at: date,
}) satisfies z.ZodType<Generated<'FixEvent'>>;
export const fixDetailSchema = fixSchema.extend({ sent_body: z.string(), events: z.array(fixEventSchema) }) satisfies z.ZodType<Generated<'FixDetail'>>;

export const statusSchema = z.object({ connected_repos: z.number().int(), needs_person: z.number().int(), running: z.number().int() }) satisfies z.ZodType<Generated<'AutofixStatus'>>;
const fixPageSchema = z.object({ items: z.array(fixSchema), next_cursor: z.string().nullable() });
const fixListSchema = z.object({ items: z.array(fixSchema) });

export const previewSchema = z.object({
  title: z.string(), body: z.string(), message_ids: z.array(uuid),
  notes: z.array(z.object({ message_id: uuid, author_name: z.string().nullable(), created_at: date, excerpt: z.string(), included: z.boolean() })),
  notes_allowed: z.boolean().optional(), attachments_left_out: z.number().int(),
  destination: z.object({ repo_full_name: z.string(), connector: connectorSchema, sends_as: z.enum(['person', 'workspace_key']), vendor_retention: z.string() }),
  removed: z.object({ emails: z.number().int(), phones: z.number().int(), cards: z.number().int(), secrets: z.number().int(), mentions: z.number().int(), references: z.number().int() }),
}) satisfies z.ZodType<Generated<'FixPreview'>>;
export const fixSentSchema = z.object({ fix: fixSchema, authorize_url: z.string().nullable() }) satisfies z.ZodType<Generated<'FixSent'>>;

export const installationSchema = z.object({
  id: uuid, installation_id: z.number().int(), account_login: z.string(), account_type: z.enum(['User', 'Organization']),
  repository_selection: z.enum(['selected', 'all']), status: z.enum(['active', 'blocked_all_repositories', 'suspended']),
  connected_by_name: z.string(), connected_at: date, settings_url: z.string(),
}) satisfies z.ZodType<Generated<'GitHubInstallation'>>;
export const visibilitySchema = z.enum(['private', 'internal', 'public']);
export const repoSchema = z.object({
  id: uuid, installation_id: z.number().int().nullable(), github_repo_id: z.number().int(), full_name: z.string(), visibility: visibilitySchema,
  visibility_checked_at: date, default_branch: z.string(), include_notes: z.boolean(), default_connector: connectorSchema.nullable(),
  paused_reason: z.enum(['became_public', 'removed_from_app', 'installation_suspended']).nullable(), paused_at: date.nullable(),
  connected_by_name: z.string(), connected_at: date,
}) satisfies z.ZodType<Generated<'AutofixRepo'>>;
export const connectorStateSchema = z.object({
  kind: connectorSchema, enabled: z.boolean(),
  consent: z.object({ version: z.string(), name: z.string(), at: date }).nullable(),
  credential: z.object({ set_by_name: z.string().nullable(), set_at: date }).nullable(),
  last_check: z.object({ at: date, result: z.string(), message: z.string().nullable() }).nullable(),
  // Copilot: assigned by Colliedesk, or (fallback) a person assigns it on GitHub; null for the other agents.
  mode: z.enum(['assign_api', 'assign_on_github']).nullable(),
}) satisfies z.ZodType<Generated<'AutofixConnector'>>;
export const settingsSchema = z.object({
  available: z.boolean(), consent_version: z.string(), app_slug: z.string().nullable(),
  installations: z.array(installationSchema), repos: z.array(repoSchema), connectors: z.array(connectorStateSchema),
}) satisfies z.ZodType<Generated<'AutofixSettings'>>;
export const githubRepoSchema = z.object({ github_repo_id: z.number().int(), full_name: z.string(), visibility: visibilitySchema, connected_repo_id: uuid.nullable() }) satisfies z.ZodType<Generated<'GitHubRepository'>>;
const githubRepoListSchema = z.object({ items: z.array(githubRepoSchema) });
const authorizeSchema = z.object({ authorize_url: z.string() });

export type Fix = z.infer<typeof fixSchema>;
export type FixDetail = z.infer<typeof fixDetailSchema>;
export type FixEvent = z.infer<typeof fixEventSchema>;
export type FixState = z.infer<typeof fixStateSchema>;
export type FixConnector = z.infer<typeof connectorSchema>;
export type FixReason = z.infer<typeof reasonSchema>;
export type FixGroup = z.infer<typeof fixGroupSchema>;
export type FixPreview = z.infer<typeof previewSchema>;
export type FixSent = z.infer<typeof fixSentSchema>;
export type AutofixSettings = z.infer<typeof settingsSchema>;
export type Installation = z.infer<typeof installationSchema>;
export type Repo = z.infer<typeof repoSchema>;
export type ConnectorState = z.infer<typeof connectorStateSchema>;
export type GitHubRepo = z.infer<typeof githubRepoSchema>;
export type FixSendInput = RequestBody<'sendFix'>;
export type FixPreviewInput = RequestBody<'previewFix'>;
export interface FixFilter { group?: FixGroup; repo_id?: string; connector?: FixConnector; cursor?: string }

const send = (method: string, body?: unknown): RequestInit => ({ method, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
const query = (values: Record<string, string | undefined>) => {
  const search = new URLSearchParams(Object.entries(values).filter((entry): entry is [string, string] => Boolean(entry[1]))).toString();
  return search ? `?${search}` : '';
};
// A change carries the row_version the person saw, so a fix that moved since answers 409 fix_changed.
const seen = (fix: Fix) => ({ row_version: fix.row_version });
const empty = z.undefined();

export const autofixApi = {
  status: () => request('/api/v1/autofix/status', statusSchema),
  list: (filter: FixFilter) => {
    const search = query({ ...filter });
    return request(`/api/v1/autofix/fixes${search}`, fixPageSchema);
  },
  get: (id: string) => request(`/api/v1/autofix/fixes/${id}`, fixDetailSchema),
  ofConversation: (conversation: string) => request(`/api/v1/conversations/${conversation}/fixes`, fixListSchema),
  preview: (conversation: string, body: FixPreviewInput) => request(`/api/v1/conversations/${conversation}/fixes/preview`, previewSchema, send('POST', body)),
  send: (conversation: string, body: FixSendInput) => request(`/api/v1/conversations/${conversation}/fixes`, fixSentSchema, send('POST', body)),
  retry: (fix: string, body: FixSendInput) => request(`/api/v1/autofix/fixes/${fix}/retry`, fixSentSchema, send('POST', body)),
  cancel: (fix: Fix) => request(`/api/v1/autofix/fixes/${fix.id}/cancel`, fixSchema, send('POST', seen(fix))),
  markDeployed: (fix: Fix) => request(`/api/v1/autofix/fixes/${fix.id}/deployed`, fixSchema, send('PUT', seen(fix))),
  unmarkDeployed: (fix: Fix) => request(`/api/v1/autofix/fixes/${fix.id}/deployed`, fixSchema, send('DELETE')),
  linkPR: (fix: Fix, url: string) => request(`/api/v1/autofix/fixes/${fix.id}/pull-request`, fixSchema, send('PUT', { url, ...seen(fix) })),
  unlinkPR: (fix: Fix) => request(`/api/v1/autofix/fixes/${fix.id}/pull-request`, fixSchema, send('DELETE')),

  settings: () => request('/api/v1/autofix/settings', settingsSchema),
  connectGitHub: () => request('/api/v1/autofix/github/installations', authorizeSchema, send('POST')),
  disconnectGitHub: (installation: string) => request(`/api/v1/autofix/github/installations/${installation}`, settingsSchema, send('DELETE')),
  githubRepos: (installation: string) => request(`/api/v1/autofix/github/installations/${installation}/repositories`, githubRepoListSchema),
  connectRepo: (installation: string, githubRepo: number, connector?: FixConnector) =>
    request('/api/v1/autofix/repos', repoSchema, send('POST', { installation_id: installation, github_repo_id: githubRepo, ...(connector ? { default_connector: connector } : {}) })),
  setDefaultConnector: (repo: string, connector: FixConnector | null) => request(`/api/v1/autofix/repos/${repo}`, repoSchema, send('PATCH', { default_connector: connector })),
  disconnectRepo: (repo: string) => request(`/api/v1/autofix/repos/${repo}`, empty, send('DELETE')),
  includeNotes: (repo: string, on: boolean) => request(`/api/v1/autofix/repos/${repo}/include-notes`, repoSchema, send(on ? 'PUT' : 'DELETE')),
  enableConnector: (kind: FixConnector, consentVersion: string) => request(`/api/v1/autofix/connectors/${kind}`, connectorStateSchema, send('PUT', { consent_version: consentVersion })),
  disableConnector: (kind: FixConnector) => request(`/api/v1/autofix/connectors/${kind}`, connectorStateSchema, send('DELETE')),
  checkConnector: (kind: FixConnector) => request(`/api/v1/autofix/connectors/${kind}/check`, connectorStateSchema, send('POST')),
  setCursorKey: (key: string) => request('/api/v1/autofix/connectors/cursor_api/key', connectorStateSchema, send('PUT', { key })),
  removeCursorKey: () => request('/api/v1/autofix/connectors/cursor_api/key', connectorStateSchema, send('DELETE')),
};

// The query keys every Autofix screen shares, so a change in one refreshes the others.
export const keys = {
  status: ['autofix-status'] as const,
  conversation: (id: string) => ['autofix-conversation', id] as const,
  fix: (id: string) => ['autofix-fix', id] as const,
  list: ['autofix-fixes'] as const,
  settings: ['autofix-settings'] as const,
};
