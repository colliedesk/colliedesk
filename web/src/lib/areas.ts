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
  { prefix: '/knowledge', permission: 'kb_articles:read', label: messages.knowledge },
  { prefix: '/results', permission: 'results:read', label: messages.results },
  { prefix: '/review', permission: 'messages:read', label: messages.review },
  { prefix: '/fixes', permission: 'autofix:read', label: messages.autofix },
  { prefix: '/settings/roles', permission: 'team:read', label: messages.roles },
  { prefix: '/settings/channels', permission: 'channels:read', label: messages.channels },
  { prefix: '/settings/ai', permission: 'ai_settings:read', label: messages.aiSettings },
  { prefix: '/settings/targets', permission: 'conversations:read', label: messages.responseTargets },
  { prefix: '/settings/numbers', permission: 'conversations:read', label: messages.conversationNumbers },
  { prefix: '/settings/import-freshdesk', permission: 'imports:read', label: messages.importFreshdesk },
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

// The rail's destinations in order, for a session: each shows when its permission is held. Knowledge opens on
// its articles, or on the media library for a role that reads only media.
export type RailArea = { to: '/' | '/knowledge' | '/knowledge/media' | '/results' | '/settings'; label: string };
export function railAreasFor(session: Session): RailArea[] {
  const reads = (permission: TokenPermission) => can(session, permission);
  return [
    ...(reads('conversations:read') ? [{ to: '/' as const, label: messages.inbox }] : []),
    ...(reads('kb_articles:read') ? [{ to: '/knowledge' as const, label: messages.knowledge }]
      : reads('kb_media:read') ? [{ to: '/knowledge/media' as const, label: messages.knowledge }] : []),
    ...(reads('results:read') ? [{ to: '/results' as const, label: messages.results }] : []),
    { to: '/settings' as const, label: messages.settings },
  ];
}

// firstOpenArea is where a session lands when the inbox is closed to it.
export function firstOpenArea(session: Session): RailArea['to'] {
  return railAreasFor(session)[0].to;
}
