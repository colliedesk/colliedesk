import type { TokenPermission, TokenPreset } from '../../api/schemas';

// What an agent token can hold, as the token form shows it: one row per resource, one chip per action.
// The server's list (auth.TokenPermissions) is the source; this copy adds the words people read, and
// token-permissions.test.ts holds it to the contract's TokenPermission enum, in order.

export type PermissionInfo = {
  name: TokenPermission;
  // The lowest tier that holds it: choosing a tier ticks every permission of its preset and below.
  preset: TokenPreset;
  // Reaches outside the workspace: a customer, the public help center, another service.
  outside?: boolean;
  // Every use waits for a person's approval, whatever the token holds.
  asks?: boolean;
};

export type ResourceInfo = { resource: string; label: string; permissions: PermissionInfo[] };

const p = (name: TokenPermission, preset: TokenPreset, flags: { outside?: boolean; asks?: boolean } = {}): PermissionInfo => ({ name, preset, ...flags });

export const tokenResources: ResourceInfo[] = [
  { resource: 'conversations', label: 'Conversations', permissions: [p('conversations:read', 'read'), p('conversations:update', 'draft')] },
  { resource: 'messages', label: 'Messages', permissions: [p('messages:read', 'read'), p('messages:note', 'draft'), p('messages:send', 'send', { outside: true })] },
  { resource: 'ai_assist', label: 'AI drafts and triage', permissions: [p('ai_assist:read', 'read'), p('ai_assist:create', 'draft')] },
  { resource: 'contacts', label: 'Contacts', permissions: [p('contacts:read', 'read'), p('contacts:create', 'send'), p('contacts:update', 'send')] },
  { resource: 'tags', label: 'Tags', permissions: [p('tags:read', 'read'), p('tags:create', 'draft'), p('tags:update', 'send'), p('tags:delete', 'send')] },
  { resource: 'saved_replies', label: 'Saved replies', permissions: [p('saved_replies:read', 'read'), p('saved_replies:create', 'send'), p('saved_replies:update', 'send'), p('saved_replies:delete', 'send')] },
  { resource: 'saved_views', label: 'Saved views', permissions: [p('saved_views:read', 'read'), p('saved_views:create', 'draft'), p('saved_views:update', 'draft'), p('saved_views:delete', 'draft')] },
  { resource: 'kb_articles', label: 'Knowledge base articles', permissions: [p('kb_articles:read', 'read'), p('kb_articles:create', 'send'), p('kb_articles:update', 'send'), p('kb_articles:delete', 'send'), p('kb_articles:publish', 'send', { outside: true })] },
  { resource: 'kb_media', label: 'Knowledge base media', permissions: [p('kb_media:read', 'read'), p('kb_media:create', 'send'), p('kb_media:update', 'send'), p('kb_media:delete', 'send'), p('kb_media:purge', 'admin')] },
  { resource: 'help_center', label: 'Help center', permissions: [p('help_center:read', 'read'), p('help_center:update', 'admin', { outside: true })] },
  { resource: 'results', label: 'Results', permissions: [p('results:read', 'read'), p('results:share', 'admin', { outside: true })] },
  { resource: 'agent_requests', label: 'Its own requests', permissions: [p('agent_requests:read', 'read'), p('agent_requests:cancel', 'read')] },
  { resource: 'profile', label: 'Your profile', permissions: [p('profile:update', 'draft')] },
  { resource: 'team', label: 'Team and sessions', permissions: [p('team:read', 'read'), p('team:invite', 'admin', { asks: true }), p('team:revoke', 'admin'), p('team:manage_roles', 'admin'), p('team:create_tokens', 'admin', { asks: true })] },
  { resource: 'channels', label: 'Email and web form', permissions: [p('channels:read', 'read'), p('channels:update', 'admin'), p('channels:manage_credentials', 'admin', { outside: true, asks: true })] },
  { resource: 'workspace', label: 'Workspace', permissions: [p('workspace:update', 'admin'), p('workspace:export', 'admin', { outside: true, asks: true })] },
  { resource: 'billing', label: 'Billing', permissions: [p('billing:read', 'admin'), p('billing:manage', 'admin', { asks: true })] },
  { resource: 'imports', label: 'Imports', permissions: [p('imports:read', 'admin'), p('imports:run', 'admin'), p('imports:manage_credentials', 'admin', { outside: true }), p('imports:undo', 'admin', { asks: true })] },
  { resource: 'ai_settings', label: 'AI settings', permissions: [p('ai_settings:read', 'read'), p('ai_settings:update', 'admin'), p('ai_settings:manage_credentials', 'admin', { outside: true })] },
  { resource: 'autofix', label: 'Autofix', permissions: [p('autofix:read', 'read')] },
  { resource: 'autofix_settings', label: 'Autofix settings', permissions: [p('autofix_settings:read', 'read'), p('autofix_settings:update', 'admin')] },
];

export const actionLabels: Record<string, string> = {
  read: 'View', create: 'Create', update: 'Edit', delete: 'Delete', note: 'Write notes', send: 'Send to customers', publish: 'Publish',
  purge: 'Delete for good', share: 'Share links', cancel: 'Cancel', invite: 'Invite', revoke: 'Revoke', manage_roles: 'Change roles',
  create_tokens: 'Create tokens', manage_credentials: 'Keys and connections', export: 'Export', manage: 'Change plan', run: 'Run', undo: 'Undo',
};

export const allPermissions: PermissionInfo[] = tokenResources.flatMap((item) => item.permissions);

const rank: Record<TokenPreset, number> = { read: 1, draft: 2, send: 3, admin: 4 };

// presetPermissions lists what a tier holds: every permission of its own preset and below.
export function presetPermissions(scope: TokenPreset): TokenPermission[] {
  return allPermissions.filter((item) => rank[item.preset] <= rank[scope]).map((item) => item.name);
}

// The name people read for a permission, for example "Knowledge base articles: Publish".
export function permissionLabel(name: string): string {
  const [resource, action] = name.split(':');
  const label = tokenResources.find((item) => item.resource === resource)?.label ?? resource;
  return `${label}: ${actionLabels[action] ?? action}`;
}
