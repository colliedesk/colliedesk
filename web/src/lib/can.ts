import type { TokenPermission } from '../api/schemas';

// What this session may do, from GET /auth/me: the person's roles (and for an agent token, what it was
// given). The app shows an action only when the call behind it would be let through; the server checks
// every call again. Never decide by a role's name.
export type Permissions = { permissions: readonly string[] } | undefined | null;

export function can(me: Permissions, permission: TokenPermission): boolean {
  // A session read before permissions existed (or a stub) holds none.
  return Boolean(me?.permissions?.includes(permission));
}
