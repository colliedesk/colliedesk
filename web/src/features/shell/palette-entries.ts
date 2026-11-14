import { createElement } from 'react';
import type { ReactNode } from 'react';
import type { Me } from '../../api/schemas';
import { can } from '../../lib/can';
import { knowledgeHome } from '../../lib/areas';
import { messages } from '../../messages';
import { AtIcon, BookIcon, ChartIcon, InboxIcon, PenIcon, SettingsIcon, UserPlusIcon } from '../../ui/icons';
import { mentionMessages } from '../inbox/mention-messages';
import { frameMessages } from './frame-messages';

// A row the palette offers without searching: a page to go to (href) or an action to run (run).
export interface PaletteEntry { key: string; label: string; detail?: string; icon: ReactNode; href?: string; run?: () => void }

// PaletteSource is what the search palette offers a session: its places, its actions, which searches it may run,
// and how it opens an address in the app.
export interface PaletteSource {
  places: PaletteEntry[];
  actions: PaletteEntry[];
  searchConversations: boolean;
  searchCustomers: boolean;
  onNavigate: (href: string) => void;
}

interface Counts {
  inboxOpen?: number;
  mentionsUnread?: number;
  // There for someone who may start a conversation, as the sidebar's pen is.
  onNewConversation?: () => void;
}

const icon = (component: () => ReactNode) => createElement(component);

// paletteEntries lists the places and actions of a session by the same rules as the sidebar: a place shows when
// its read permission is held, Settings always, New conversation where the pen is, Invite teammates with team:invite.
export function paletteEntries(me: Me, { inboxOpen = 0, mentionsUnread = 0, onNewConversation }: Counts): Omit<PaletteSource, 'onNavigate'> {
  const reads = (permission: Parameters<typeof can>[1]) => can(me, permission);
  const knowledge = knowledgeHome(me);
  const places: PaletteEntry[] = [
    ...(reads('conversations:read') ? [{ key: 'inbox', label: messages.inbox, detail: inboxOpen > 0 ? frameMessages.openCount(inboxOpen) : undefined, icon: icon(InboxIcon), href: '/' }] : []),
    ...(reads('messages:read') ? [{ key: 'mentions', label: mentionMessages.mentions, detail: mentionsUnread > 0 ? frameMessages.unreadCount(mentionsUnread) : undefined, icon: icon(AtIcon), href: '/mentions' }] : []),
    ...(knowledge ? [{ key: 'knowledge', label: messages.knowledge, icon: icon(BookIcon), href: knowledge }] : []),
    ...(reads('results:read') ? [{ key: 'results', label: messages.results, icon: icon(ChartIcon), href: '/results' }] : []),
    { key: 'settings', label: messages.settings, detail: frameMessages.settingsDetail, icon: icon(SettingsIcon), href: '/settings' },
  ];
  const actions: PaletteEntry[] = [
    ...(onNewConversation ? [{ key: 'new-conversation', label: messages.newConversation, icon: icon(PenIcon), run: onNewConversation }] : []),
    ...(reads('team:invite') ? [{ key: 'invite', label: messages.inviteTeammates, icon: icon(UserPlusIcon), href: '/settings/members?tab=invites' }] : []),
  ];
  return { places, actions, searchConversations: reads('conversations:read'), searchCustomers: reads('contacts:read') };
}
