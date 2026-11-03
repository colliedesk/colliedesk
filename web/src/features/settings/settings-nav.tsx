import { createContext, useContext, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { QueryClientContext, QueryObserver } from '@tanstack/react-query';
import type { QueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import type { Permissions } from '../../lib/can';
import { track } from '../../lib/product-analytics';
import type { SettingsSection } from '../../lib/product-analytics';
import { messages } from '../../messages';
import { autofixPreviewOn } from '../autofix/autofix-preview';

export type SettingsPath = '/settings' | '/settings/billing' | '/settings/import-freshdesk' | '/settings/ai' | '/settings/autofix' | '/settings/channels'
  | '/settings/channels/type/email' | '/settings/channels/type/web_form' | '/settings/channels/type/help_chat'
  | '/settings/channels/type/zalo' | '/settings/numbers' | '/settings/targets' | '/settings/roles';
// sub: the pages under an item, listed below it (Channels: one per channel type).
export interface NavItem { label: string; to: SettingsPath; hash?: string; current?: boolean; sub?: NavItem[] }
interface NavGroup { label: string; items: NavItem[] }

// hrefFor links a section on the same page by its anchor alone, and one on the other settings page by path and anchor.
export function hrefFor(item: NavItem, page: SettingsPath): string {
  if (!item.hash) return item.to;
  return `${item.to === page ? '' : item.to}#${item.hash}`;
}

const sectionByHash: Record<string, SettingsSection> = {
  'profile-heading': 'profile', 'preferences-heading': 'preferences', 'general-heading': 'general', 'team-heading': 'team',
  'invites-heading': 'invites', 'agents-heading': 'agents', 'sessions-heading': 'sessions',
  'saved-replies-heading': 'saved_replies', 'help-center-heading': 'help_center', 'export-heading': 'export',
};
const sectionOf = (item: NavItem): SettingsSection | undefined => (item.hash ? sectionByHash[item.hash] : item.to === '/settings/billing' ? 'billing' : item.to === '/settings/ai' ? 'ai' : item.to.startsWith('/settings/channels') ? 'channels' : undefined);

type RenderLink = (item: NavItem, page: SettingsPath) => ReactNode;
const plainLink: RenderLink = (item, page) =>
  <a href={hrefFor(item, page)} aria-current={item.current ? 'page' : undefined}>{item.label}</a>;

// The app provides router links here; without a router (unit tests) the nav uses plain anchors.
export const SettingsNavLinkContext = createContext<RenderLink>(plainLink);

export interface Shown { general: boolean; team: boolean; channels: boolean; savedReplies: boolean; helpCenter?: boolean; exportData: boolean; billing: boolean; importData?: boolean; ai?: boolean; agents?: boolean; autofix?: boolean; roles?: boolean; invites?: boolean; conversations?: boolean;
  // hiddenChannelTypes: types the server does not offer this person and the workspace has none of (Zalo OA outside Vietnam).
  hiddenChannelTypes?: string[] }

// byPermission shows the workspace pages the session may use, whatever a page passed: a person sees a
// page when their roles hold what it needs.
export function byPermission(shown: Shown, me: Permissions): Shown {
  if (!me) return shown;
  return { ...shown, general: can(me, 'workspace:update'), team: can(me, 'team:read'), roles: can(me, 'team:read'), invites: can(me, 'team:read'),
    exportData: can(me, 'workspace:export'), importData: can(me, 'imports:run'), agents: shown.agents !== false && can(me, 'team:create_tokens'),
    channels: shown.channels && can(me, 'channels:read'), conversations: can(me, 'conversations:read'), ai: shown.ai !== false && can(me, 'ai_settings:read'),
    savedReplies: shown.savedReplies && can(me, 'saved_replies:read'), helpCenter: shown.helpCenter !== false && can(me, 'help_center:read') };
}

// The channel types' places under Channels, in the order of the Add channel list.
const channelPlaces: { type: string; label: string; to: SettingsPath }[] = [
  { type: 'email', label: messages.emailNav, to: '/settings/channels/type/email' },
  { type: 'web_form', label: messages.webFormNav, to: '/settings/channels/type/web_form' },
  { type: 'help_chat', label: messages.helpChatNav, to: '/settings/channels/type/help_chat' },
  { type: 'zalo', label: messages.zaloNav, to: '/settings/channels/type/zalo' },
];

type ChannelCounts = Awaited<ReturnType<typeof api.channelCounts>>;
const hiddenOf = (counts: ChannelCounts | undefined) => (counts?.types ?? []).filter((info) => info.available === false && info.count === 0).map((info) => info.type);

// useHiddenChannelTypes reads the types this person is not offered from the channel counts the inbox
// also reads (GET /api/v1/channels), fetched once per minute at most. Without a cache (unit tests of a
// page alone) or permission to read channels, nothing is hidden.
function useHiddenChannelTypes(client: QueryClient | undefined, enabled: boolean): string[] {
  const [hidden, setHidden] = useState(() => hiddenOf(client?.getQueryData<ChannelCounts>(['channels', 'counts'])));
  useEffect(() => {
    if (!client || !enabled) return undefined;
    const observer = new QueryObserver<ChannelCounts>(client, { queryKey: ['channels', 'counts'], queryFn: api.channelCounts, staleTime: 60_000 });
    const update = (result: { data?: ChannelCounts }) => setHidden((before) => {
      const next = hiddenOf(result.data);
      return next.join() === before.join() ? before : next;
    });
    update(observer.getCurrentResult());
    return observer.subscribe(update);
  }, [client, enabled]);
  return hidden;
}

// settingsNavGroups lists what this person can see: their own account, then the shared workspace.
export function settingsNavGroups(shown: Shown, page: SettingsPath): NavGroup[] {
  const section = (label: string, hash: string, visible = true): NavItem[] => (visible ? [{ label, to: '/settings', hash }] : []);
  return [
    { label: messages.settingsYourAccount, items: [...section(messages.profile, 'profile-heading'), ...section(messages.preferences, 'preferences-heading')] },
    { label: messages.settingsWorkspace, items: [
      ...section(messages.general, 'general-heading', shown.general),
      ...section(messages.team, 'team-heading', shown.team),
      ...(shown.roles ? [{ label: messages.roles, to: '/settings/roles' as const, current: page === '/settings/roles' }] : []),
      ...section(messages.invites, 'invites-heading', shown.invites !== false),
      ...(shown.ai ? [{ label: messages.aiSettings, to: '/settings/ai' as const, current: page === '/settings/ai' }] : []),
      // Response targets and conversation numbers read conversations, not channels.
      ...(shown.conversations ?? shown.channels ? [{ label: messages.responseTargets, to: '/settings/targets' as const, current: page === '/settings/targets' }] : []),
      // Autofix shows even on a server without its GitHub App: the page names the settings the operator still needs.
      // While in preview it shows only in a browser that turned it on (autofix-preview.ts).
      ...(shown.autofix !== false && autofixPreviewOn() ? [{ label: messages.autofix, to: '/settings/autofix' as const, current: page === '/settings/autofix' }] : []),
      ...section(messages.aiAgents, 'agents-heading', shown.agents),
      ...section(messages.sessions, 'sessions-heading'),
      ...(shown.channels ? [{ label: messages.channels, to: '/settings/channels' as const, current: page === '/settings/channels', sub: [
        ...channelPlaces.filter(({ type }) => !shown.hiddenChannelTypes?.includes(type))
          .map(({ label, to }) => ({ label, to, current: page === to })),
      ] }] : []),
      ...(shown.conversations ?? shown.channels ? [{ label: messages.conversationNumbers, to: '/settings/numbers' as const, current: page === '/settings/numbers' }] : []),
      ...section(messages.savedReplies, 'saved-replies-heading', shown.savedReplies),
      ...section(messages.helpCenter, 'help-center-heading', shown.helpCenter),
      ...section(messages.exportHeading, 'export-heading', shown.exportData),
      ...(shown.importData ? [{ label: messages.importFreshdesk, to: '/settings/import-freshdesk' as const, current: page === '/settings/import-freshdesk' }] : []),
      ...(shown.billing ? [{ label: messages.planAndBilling, to: '/settings/billing' as const, current: page === '/settings/billing' }] : []),
    ] },
  ];
}

export function SettingsNav({ shown: given, page }: { shown: Shown; page: SettingsPath }) {
  const renderLink = useContext(SettingsNavLinkContext);
  // The session is in the cache on every settings page, which reads it itself and so renders again when it
  // changes; without a cache (unit tests of a page alone) the page's own flags stand.
  const client = useContext(QueryClientContext);
  const me = client?.getQueryData<{ permissions: string[] }>(['me']);
  const permitted = byPermission(given, me);
  const hiddenChannelTypes = useHiddenChannelTypes(client, permitted.channels);
  const shown = { ...permitted, hiddenChannelTypes };
  const nav = useRef<HTMLElement>(null);
  // On a narrow screen the links are one row that scrolls sideways: the current page is brought into it,
  // again when links appear before it.
  const links = JSON.stringify(shown);
  useEffect(() => {
    nav.current?.querySelector('[aria-current="page"]')?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' });
  }, [page, links]);
  return (
    <nav ref={nav} className="settings-nav" aria-label={messages.settingsSections}>
      {settingsNavGroups(shown, page).map((group) => <div key={group.label}><p className="settings-group-label">{group.label}</p>
        <ul>{group.items.map((item) => <li key={item.label} className={item.sub ? 'settings-nav-parent' : undefined} onClick={() => { const section = sectionOf(item); if (section) track('settings_section_opened', { section }); }}>{renderLink(item, page)}
          {item.sub && <ul className="settings-nav-sub" aria-label={item.label}>{item.sub.map((sub) => <li key={sub.label}>{renderLink(sub, page)}</li>)}</ul>}</li>)}</ul>
      </div>)}
    </nav>
  );
}
