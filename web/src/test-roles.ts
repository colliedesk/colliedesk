import type { Role, RoleRef, TokenPermission } from './api/schemas';
import { allPermissions } from './features/settings/token-permissions';

// Test helpers: the two built-in roles as GET /auth/me and the team list name them, and what each holds.
export const adminRoleRef: RoleRef = { id: '00000000-0000-4000-8000-0000000000a1', name: 'Admin', system: 'admin' };
export const agentRoleRef: RoleRef = { id: '00000000-0000-4000-8000-0000000000a2', name: 'Agent', system: 'agent' };

const agentPermissions: TokenPermission[] = allPermissions.map((item) => item.name).filter((name) =>
  !['kb_media:purge', 'help_center:update', 'results:share', 'team:invite', 'team:revoke', 'team:manage_roles', 'channels:update', 'channels:manage_credentials',
    'workspace:update', 'workspace:export', 'billing:read', 'billing:manage', 'imports:read', 'imports:run', 'imports:manage_credentials', 'imports:undo',
    'ai_settings:update', 'ai_settings:manage_credentials', 'autofix_settings:update', 'autofix_settings:manage_credentials'].includes(name));

export const rolesOf = (role: 'admin' | 'agent'): RoleRef[] => [role === 'admin' ? adminRoleRef : agentRoleRef];
export const permissionsOf = (role: 'admin' | 'agent'): TokenPermission[] => (role === 'admin' ? allPermissions.map((item) => item.name) : agentPermissions);

// roleOf is a whole role as GET /roles lists it.
export const roleOf = (ref: RoleRef, permissions: readonly string[], extra: Partial<Role> = {}): Role => ({ ...ref, description: '', permissions: permissions as Role['permissions'],
  default_permissions: ref.system === 'agent' ? agentPermissions : null, editable: true, deletable: false, assignable: true, members: [], invites: 0, tokens: [], updated_by: null, deleted_at: null, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z', ...extra });
export const builtInRoles: Role[] = [roleOf(adminRoleRef, permissionsOf('admin')), roleOf(agentRoleRef, permissionsOf('agent'))];
