import type { TokenPermission } from '../../api/schemas';

// Every permission a role or an agent token can hold, as the permission matrix shows them: one row per
// resource, one chip per action. The server's list (auth.TokenPermissions) is the source; this copy adds
// the words people read, and token-permissions.test.ts holds it to the contract's TokenPermission enum, in order.

export type PermissionInfo = {
  name: TokenPermission;
  // One sentence on what it allows, shown under the matrix for the chip in focus.
  describe: string;
  // Reaches outside the workspace: a customer, the public help center, another service.
  outside?: boolean;
};

export type ResourceInfo = { resource: string; label: string; permissions: PermissionInfo[] };

const p = (name: TokenPermission, describe: string, outside = false): PermissionInfo => ({ name, describe, ...(outside ? { outside } : {}) });

export const tokenResources: ResourceInfo[] = [
  { resource: 'conversations', label: 'Conversations', permissions: [
    p('conversations:read', 'See the inbox, conversations, who is viewing, time spent, response targets.'),
    p('conversations:update', 'Change status, priority, assignee and tags; move messages; decide intake cards.')] },
  { resource: 'messages', label: 'Messages', permissions: [
    p('messages:read', 'Read messages, attachments, chat threads and AI chats.'),
    p('messages:note', 'Write private notes.'),
    p('messages:send', 'Reply to customers, start conversations, retry failed replies.', true)] },
  { resource: 'ai_assist', label: 'AI drafts and triage', permissions: [
    p('ai_assist:read', 'See AI drafts and triage suggestions.'),
    p('ai_assist:create', 'Ask for, leave and discard drafts; accept or dismiss triage suggestions.')] },
  { resource: 'contacts', label: 'Contacts', permissions: [
    p('contacts:read', 'See contacts.'), p('contacts:create', 'Add contacts.'), p('contacts:update', 'Edit, merge and split contacts.')] },
  { resource: 'tags', label: 'Tags', permissions: [
    p('tags:read', 'See tags.'), p('tags:create', 'Create tags.'), p('tags:update', 'Rename tags.'), p('tags:delete', 'Delete tags.')] },
  { resource: 'saved_replies', label: 'Saved replies', permissions: [
    p('saved_replies:read', 'See saved replies, their trash and their versions.'), p('saved_replies:create', 'Create saved replies.'),
    p('saved_replies:update', 'Edit saved replies and restore older versions.'), p('saved_replies:delete', 'Delete saved replies and bring them back.')] },
  { resource: 'saved_views', label: 'Saved views', permissions: [
    p('saved_views:read', 'See saved inbox views.'), p('saved_views:create', 'Create views.'),
    p('saved_views:update', 'Edit views.'), p('saved_views:delete', 'Delete views and bring them back.')] },
  { resource: 'macros', label: 'Macros', permissions: [
    p('macros:read', 'See macros and their trash.'), p('macros:create', 'Create macros.'),
    p('macros:update', 'Edit macros.'), p('macros:delete', 'Delete macros and bring them back.')] },
  { resource: 'kb_articles', label: 'Knowledge base articles', permissions: [
    p('kb_articles:read', 'See articles, collections, their trash and versions.'), p('kb_articles:create', 'Create articles and collections.'),
    p('kb_articles:update', 'Edit and order articles, restore versions, keep an article from AI.'),
    p('kb_articles:delete', 'Delete articles and collections and bring them back.'),
    p('kb_articles:publish', 'Make any change that reaches the public help center.', true)] },
  { resource: 'kb_media', label: 'Knowledge base media', permissions: [
    p('kb_media:read', 'See images, videos and uploads.'), p('kb_media:create', 'Upload images and videos.'),
    p('kb_media:update', 'Edit and replace media, change a video poster.'), p('kb_media:delete', 'Move media to the trash and back.'),
    p('kb_media:purge', 'Delete media for good.')] },
  { resource: 'help_center', label: 'Help center', permissions: [
    p('help_center:read', 'See the help center settings, theme and domain.'),
    p('help_center:update', 'Turn the help center on or off, change its theme, domain and Search Console.', true)] },
  { resource: 'results', label: 'Reports', permissions: [
    p('results:read', 'See results.'), p('results:share', 'See every number and make share links.', true)] },
  { resource: 'profile', label: 'Your profile', permissions: [p('profile:update', 'Change your own name, time zone and emails.')] },
  { resource: 'team', label: 'Team and sessions', permissions: [
    p('team:read', 'See the team, roles and invites.'), p('team:invite', 'Invite people with roles you hold.'),
    p('team:revoke', 'Sign other people out and revoke invites.'), p('team:manage_roles', 'Create and change roles, and give them to people.'),
    p('team:create_tokens', 'Create agent tokens in Settings.')] },
  { resource: 'channels', label: 'Channels', permissions: [
    p('channels:read', 'See channels and their settings.'), p('channels:update', 'Change channels and their held messages.'),
    p('channels:manage_credentials', 'Connect Gmail and Microsoft 365, change secrets, remove channels.', true)] },
  { resource: 'workspace', label: 'Workspace', permissions: [
    p('workspace:update', 'Change the workspace name, address, time zone, numbers and response targets.'),
    p('workspace:export', 'Export every record of the workspace.', true)] },
  { resource: 'billing', label: 'Billing', permissions: [p('billing:read', 'See the plan and usage.'), p('billing:manage', 'Change the plan and pay.')] },
  { resource: 'imports', label: 'Imports', permissions: [
    p('imports:read', 'See imports.'), p('imports:run', 'Run imports and link imported people.'),
    p('imports:manage_credentials', 'Give an import the other helpdesk\'s key.', true), p('imports:undo', 'Undo an import.')] },
  { resource: 'ai_settings', label: 'AI settings', permissions: [
    p('ai_settings:read', 'See the AI settings.'), p('ai_settings:update', 'Turn AI features on, set levels and what data AI may read.'),
    p('ai_settings:manage_credentials', 'Add AI providers and their keys.', true)] },
  { resource: 'autofix', label: 'Autofix', permissions: [
    p('autofix:read', 'See fixes.'), p('autofix:update', 'Preview, cancel and mark fixes.'),
    p('autofix:send', 'Send a conversation to a coding agent.', true)] },
  { resource: 'autofix_settings', label: 'Autofix settings', permissions: [
    p('autofix_settings:read', 'See the Autofix settings.'), p('autofix_settings:update', 'Connect GitHub, repositories and coding agents.'),
    p('autofix_settings:manage_credentials', 'Set a coding agent\'s key.', true)] },
];

export const actionLabels: Record<string, string> = {
  read: 'View', create: 'Create', update: 'Edit', delete: 'Delete', note: 'Write notes', send: 'Send to customers', publish: 'Publish',
  purge: 'Delete for good', share: 'Share links', invite: 'Invite', revoke: 'Revoke', manage_roles: 'Change roles',
  create_tokens: 'Create tokens', manage_credentials: 'Keys and connections', export: 'Export', manage: 'Change plan', run: 'Run', undo: 'Undo',
};

// An action whose usual words would mislead for one resource: Autofix sends to a coding agent, not a customer.
const actionOverrides: Partial<Record<TokenPermission, string>> = { 'autofix:send': 'Send to coding agents' };

// The words of one permission's action, for example "Publish".
export function actionLabel(name: string): string {
  const action = name.split(':')[1];
  return actionOverrides[name as TokenPermission] ?? actionLabels[action] ?? action;
}

export const allPermissions: PermissionInfo[] = tokenResources.flatMap((item) => item.permissions);

export const outsidePermissions = new Set(allPermissions.filter((item) => item.outside).map((item) => item.name));

// The name people read for a permission, for example "Knowledge base articles: Publish".
export function permissionLabel(name: string): string {
  const resource = name.split(':')[0];
  const label = tokenResources.find((item) => item.resource === resource)?.label ?? resource;
  return `${label}: ${actionLabel(name)}`;
}

// What a list holds, in words: "14 permissions, 3 reach outside the workspace".
export function permissionSummary(names: readonly string[]): string {
  const outside = names.filter((name) => outsidePermissions.has(name as TokenPermission)).length;
  const count = `${names.length} ${names.length === 1 ? 'permission' : 'permissions'}`;
  return outside > 0 ? `${count}, ${outside} ${outside === 1 ? 'reaches' : 'reach'} outside the workspace` : count;
}
