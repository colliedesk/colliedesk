import type { TokenPermission } from '../api/schemas';
import { can } from './can';
import { messages } from '../messages';

type Session = Parameters<typeof can>[0];

// Every screen needs a read permission to show anything; a session without it is told so instead of a page
// that waits for data the server would refuse. The first rule whose prefix matches the address wins.
const screens: Array<{ prefix: string; exact?: boolean; permission: TokenPermission; label: string }> = [
  { prefix: '/', exact: true, permission: 'conversations:read', label: messages.inbox },
  { prefix: '/conversations/', permission: 'conversations:read', label: messages.inbox },
  { prefix: '/contacts/', permission: 'contacts:read', label: messages.contactArea },
  { prefix: '/knowledge/media', permission: 'kb_media:read', label: messages.knowledge },
  // The help center's settings were a section of Settings: they open with help_center:read, as they did there.
  { prefix: '/knowledge/settings', permission: 'help_center:read', label: messages.helpCenter },
  { prefix: '/knowledge', permission: 'kb_articles:read', label: messages.knowledge },
  { prefix: '/results', permission: 'results:read', label: messages.results },
  { prefix: '/review', permission: 'messages:read', label: messages.review },
  { prefix: '/fixes', permission: 'autofix:read', label: messages.autofix },
  { prefix: '/settings/roles', permission: 'team:read', label: messages.roles },
  { prefix: '/settings/members', permission: 'team:read', label: messages.members },
  { prefix: '/settings/saved-replies', permission: 'saved_replies:read', label: messages.savedReplies },
  { prefix: '/settings/channels', permission: 'channels:read', label: messages.channels },
  // AI & agents itself also holds the agent tokens; a chat's transcript under it is the built-in AI's.
  { prefix: '/settings/ai/', permission: 'ai_settings:read', label: messages.aiSettings },
  { prefix: '/settings/targets', permission: 'conversations:read', label: messages.responseTargets },
  { prefix: '/settings/billing', permission: 'billing:read', label: messages.planAndBilling },
  { prefix: '/settings/autofix', permission: 'autofix_settings:read', label: messages.autofix },
];

// closedScreen is the screen at path the session may not open, with what it lacks; undefined when it may.
export function closedScreen(path: string, session: Session): { label: string; permission: TokenPermission } | undefined {
  const matches = (item: (typeof screens)[number]) => (item.exact ? path === item.prefix
    : item.prefix.endsWith('/') ? path.startsWith(item.prefix) : path === item.prefix || path.startsWith(`${item.prefix}/`));
  const screen = screens.find(matches);
  return screen && !can(session, screen.permission) ? { label: screen.label, permission: screen.permission } : undefined;
}

// knowledgeHome is where Knowledge opens for a session: its articles, else the media library, else the help
// center's settings; undefined when the session reads none of them.
export type KnowledgeHome = '/knowledge' | '/knowledge/media' | '/knowledge/settings';
export function knowledgeHome(session: Session): KnowledgeHome | undefined {
  if (can(session, 'kb_articles:read')) return '/knowledge';
  if (can(session, 'kb_media:read')) return '/knowledge/media';
  return can(session, 'help_center:read') ? '/knowledge/settings' : undefined;
}

// The rail's destinations in order, for a session: each shows when its permission is held.
export type RailArea = { to: '/' | KnowledgeHome | '/results' | '/settings'; label: string };
export function railAreasFor(session: Session): RailArea[] {
  const reads = (permission: TokenPermission) => can(session, permission);
  const knowledge = knowledgeHome(session);
  return [
    ...(reads('conversations:read') ? [{ to: '/' as const, label: messages.inbox }] : []),
    ...(knowledge ? [{ to: knowledge, label: messages.knowledge }] : []),
    ...(reads('results:read') ? [{ to: '/results' as const, label: messages.results }] : []),
    { to: '/settings' as const, label: messages.settings },
  ];
}

// firstOpenArea is where a session lands when the inbox is closed to it.
export function firstOpenArea(session: Session): RailArea['to'] {
  return railAreasFor(session)[0].to;
}
