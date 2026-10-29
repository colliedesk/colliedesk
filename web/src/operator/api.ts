import type { components } from '../api/generated';

// The console's client: same-origin fetch with the operator cookie. It never imports the workspace app's
// client, messages or query cache, so the app's first bundle and the console's stay apart.
export type Schemas = components['schemas'];
export type Setting = Schemas['OperatorSetting'];
export type Config = Schemas['OperatorConfig'];
export type Status = Schemas['OperatorStatus'];
export type Me = Schemas['OperatorMe'];
export type Version = Schemas['OperatorConfigVersion'];
export type OperatorRequest = Schemas['OperatorRequest'];
export type AuditEntry = Schemas['OperatorAuditEntry'];
export type Operator = Schemas['Operator'];
export type OperatorSession = Schemas['OperatorSession'];
export type SignInStep = Schemas['OperatorSignInStep'];

export class ApiError extends Error {
  constructor(readonly status: number, readonly code: string, message: string, readonly body: Record<string, unknown> = {}) {
    super(message);
  }
  get fields(): Record<string, string> {
    return ((this.body.fields as Record<string, string> | undefined) ?? {});
  }
}

// sessionEnded is called on any 401 of a signed-in screen, so the shell keeps the form and asks for a new link.
let sessionEnded: () => void = () => {};
export function onSessionEnded(listener: () => void) { sessionEnded = listener; }

let keyCount = 0;
function idempotencyKey() {
  keyCount += 1;
  return `console-${Date.now().toString(36)}-${keyCount}`;
}

export async function call<T>(method: string, path: string, body?: unknown, headers: Record<string, string> = {}, signedIn = true): Promise<T> {
  const init: RequestInit = { method, credentials: 'same-origin', headers: { Accept: 'application/json', ...headers } };
  if (body !== undefined) {
    init.body = JSON.stringify(body);
    (init.headers as Record<string, string>)['Content-Type'] = 'application/json';
  }
  if (method === 'POST') (init.headers as Record<string, string>)['Idempotency-Key'] = idempotencyKey();
  const response = await fetch(path, init);
  const text = await response.text();
  const parsed = text ? JSON.parse(text) as Record<string, unknown> : {};
  if (!response.ok) {
    if (response.status === 401 && signedIn) sessionEnded();
    const error = (parsed.error ?? {}) as Record<string, unknown>;
    throw new ApiError(response.status, String(error.code ?? 'error'), String(error.message ?? response.statusText), error);
  }
  return parsed as T;
}

export const api = {
  me: () => call<Me>('GET', '/api/v1/operator/auth/me'),
  status: () => call<Status>('GET', '/api/v1/operator/status'),
  config: () => call<Config>('GET', '/api/v1/operator/config'),
  change: (body: Schemas['OperatorConfigChange']) => call<Schemas['OperatorConfigSaved']>('PATCH', '/api/v1/operator/config', body),
  check: (body: unknown) => call<Schemas['OperatorConfigCheck']>('POST', '/api/v1/operator/config/check', body),
  versions: () => call<{ running_version: number; items: Version[] }>('GET', '/api/v1/operator/config/versions'),
  restore: (id: number, body: { base_version: number; reauth_code?: string }) => call<Schemas['OperatorConfigSaved']>('POST', `/api/v1/operator/config/versions/${id}/restore`, body),
  audit: (query: string) => call<{ items: AuditEntry[] }>('GET', `/api/v1/operator/audit${query}`),
  requests: () => call<{ items: OperatorRequest[] }>('GET', '/api/v1/operator/requests'),
  decide: (id: string, action: 'approve' | 'reject', body: Record<string, unknown>) =>
    call<{ request: OperatorRequest; result: unknown } | OperatorRequest>('POST',
      action === 'approve' ? `/api/v1/agent-requests/${id}/approve` : `/api/v1/agent-requests/${id}/reject`, body, { 'X-Colliedesk-Realm': 'operator' }),
  team: () => call<{ operators: Operator[]; sessions: OperatorSession[] }>('GET', '/api/v1/operator/operators'),
  invite: (email: string, reauthCode: string) => call<{ email: string; link: string; expires_at: string }>('POST', '/api/v1/operator/operators', { email, reauth_code: reauthCode }),
  revokeOperator: (id: string, reauthCode: string) => call<void>('DELETE', `/api/v1/operator/operators/${id}`, undefined, { 'X-Colliedesk-Reauth': reauthCode }),
  revokeSession: (id: string, reauthCode: string) => call<void>('DELETE', `/api/v1/operator/sessions/${id}`, undefined, { 'X-Colliedesk-Reauth': reauthCode }),
  createToken: (label: string, permissions: string[], reauthCode: string) =>
    call<{ token: string; session: OperatorSession }>('POST', '/api/v1/operator/tokens', { label, permissions, reauth_code: reauthCode }),
  logout: () => call<void>('POST', '/api/v1/operator/auth/logout'),
  redeem: (token: string) => call<SignInStep>('POST', '/api/v1/operator/auth/redeem', { token }, {}, false),
  confirmCode: (ticket: string, code: string) => call<SignInStep>('POST', '/api/v1/operator/auth/totp', { ticket, code }, {}, false),
};
