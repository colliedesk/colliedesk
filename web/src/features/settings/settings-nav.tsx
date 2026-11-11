import { createContext, useContext, useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { QueryClientContext } from '@tanstack/react-query';
import { can } from '../../lib/can';
import type { Permissions } from '../../lib/can';
import { track } from '../../lib/product-analytics';
import type { SettingsSection } from '../../lib/product-analytics';
import { messages } from '../../messages';
import { ChevronLeftIcon } from '../../ui/icons';
import { autofixPreviewOn } from '../autofix/autofix-preview';

// Settings is one page per item of this menu (#528). '/settings' is the menu itself, a page of its own on a
// phone; '/settings/roles' is a role's page, under Members.
export type SettingsPath = '/settings' | '/settings/account' | '/settings/general' | '/settings/members' | '/settings/saved-replies' | '/settings/data'
  | '/settings/billing' | '/settings/ai' | '/settings/autofix' | '/settings/channels'
  | '/settings/channels/type/email' | '/settings/channels/type/web_form' | '/settings/channels/type/help_chat'
  | '/settings/channels/type/zalo' | '/settings/targets' | '/settings/roles';
// sub: the pages under an item, listed below it (Channels: one per channel type).
export interface NavItem { label: string; to: SettingsPath; hash?: string; current?: boolean; sub?: NavItem[] }
interface NavGroup { label?: string; items: NavItem[] }

// hrefFor links a page by its path, and a section of one by its anchor as well.
export function hrefFor(item: NavItem): string {
  return item.hash ? `${item.to}#${item.hash}` : item.to;
}

const sectionByPage: Partial<Record<SettingsPath, SettingsSection>> = {
  '/settings/account': 'account', '/settings/general': 'general', '/settings/members': 'members', '/settings/channels': 'channels', '/settings/targets': 'targets',
  '/settings/ai': 'ai', '/settings/saved-replies': 'saved_replies', '/settings/data': 'data', '/settings/billing': 'billing',
};
const sectionOf = (item: NavItem): SettingsSection | undefined => sectionByPage[item.to] ?? (item.to.startsWith('/settings/channels') ? 'channels' : undefined);

type RenderLink = (item: NavItem, page: SettingsPath) => ReactNode;
const plainLink: RenderLink = (item) =>
  <a href={hrefFor(item)} aria-current={item.current ? 'page' : undefined}>{item.label}</a>;

// The app provides router links here; without a router (unit tests) the nav uses plain anchors.
export const SettingsNavLinkContext = createContext<RenderLink>(plainLink);

export interface Shown { general: boolean; team: boolean; channels: boolean; savedReplies: boolean; helpCenter?: boolean; exportData: boolean; billing: boolean; importData?: boolean; ai?: boolean; agents?: boolean; autofix?: boolean; roles?: boolean; invites?: boolean; conversations?: boolean }

// byPermission shows the workspace pages the session may use, whatever a page passed: a person sees a
// page when their roles hold what it needs.
export function byPermission(shown: Shown, me: Permissions): Shown {
  if (!me) return shown;
  return { ...shown, general: can(me, 'workspace:update'), team: can(me, 'team:read'), roles: can(me, 'team:read'), invites: can(me, 'team:read'),
    exportData: can(me, 'workspace:export'), importData: can(me, 'imports:run'), agents: shown.agents !== false && can(me, 'team:create_tokens'),
    channels: shown.channels && can(me, 'channels:read'), conversations: can(me, 'conversations:read'), ai: shown.ai !== false && can(me, 'ai_settings:read'),
    savedReplies: shown.savedReplies && can(me, 'saved_replies:read'), helpCenter: shown.helpCenter !== false && can(me, 'help_center:read') };
}

// settingsNavGroups lists the pages this person can see: their own account, then the shared workspace,
// then its data and billing. A page shows when the person can see one of its sections at least.
export function settingsNavGroups(shown: Shown, page: SettingsPath): NavGroup[] {
  const item = (label: string, to: SettingsPath, visible = true, current = page === to): NavItem[] => (visible ? [{ label, to, current }] : []);
  const groups: NavGroup[] = [
    { items: item(messages.account, '/settings/account') },
    { label: messages.settingsWorkspace, items: [
      // Conversation numbers read conversations, not the workspace's own settings: General shows for them too.
      ...item(messages.general, '/settings/general', shown.general || Boolean(shown.conversations)),
      ...item(messages.members, '/settings/members', shown.team || shown.invites === true || shown.roles === true, page === '/settings/members' || page === '/settings/roles'),
      ...(shown.channels ? [{ label: messages.channels, to: '/settings/channels' as const, current: page === '/settings/channels', sub: [
        { label: messages.emailNav, to: '/settings/channels/type/email' as const, current: page === '/settings/channels/type/email' },
        { label: messages.webFormNav, to: '/settings/channels/type/web_form' as const, current: page === '/settings/channels/type/web_form' },
        { label: messages.helpChatNav, to: '/settings/channels/type/help_chat' as const, current: page === '/settings/channels/type/help_chat' },
        { label: messages.zaloNav, to: '/settings/channels/type/zalo' as const, current: page === '/settings/channels/type/zalo' },
      ] }] : []),
      // Response targets read conversations, not channels.
      ...item(messages.responseTargets, '/settings/targets', shown.conversations ?? shown.channels),
      ...item(messages.aiAndAgents, '/settings/ai', Boolean(shown.ai) || Boolean(shown.agents)),
      ...item(messages.savedReplies, '/settings/saved-replies', shown.savedReplies),
      // Autofix shows even on a server without its GitHub App: the page names the settings the operator still needs.
      // While in preview it shows only in a browser that turned it on (autofix-preview.ts).
      ...item(messages.autofix, '/settings/autofix', shown.autofix !== false && autofixPreviewOn()),
    ] },
    { label: messages.settingsDataAndBilling, items: [
      ...item(messages.dataPage, '/settings/data', Boolean(shown.importData) || shown.exportData),
      // Plan and billing exists only on the hosted cloud, for those who read the billing.
      ...item(messages.planAndBilling, '/settings/billing', shown.billing),
    ] },
  ];
  return groups.filter((group) => group.items.length > 0);
}

export function SettingsNav({ shown: given, page }: { shown: Shown; page: SettingsPath }) {
  const renderLink = useContext(SettingsNavLinkContext);
  // The session is in the cache on every settings page, which reads it itself and so renders again when it
  // changes; without a cache (unit tests of a page alone) the page's own flags stand.
  const me = useContext(QueryClientContext)?.getQueryData<{ permissions: string[] }>(['me']);
  const shown = byPermission(given, me);
  const nav = useRef<HTMLElement>(null);
  // Between a phone and a wide screen the links are one row that scrolls sideways: the current page is
  // brought into it, again when links appear before it.
  const links = JSON.stringify(shown);
  useEffect(() => {
    nav.current?.querySelector('[aria-current="page"]')?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' });
  }, [page, links]);
  // On a phone the menu is a page of its own (/settings), as the inbox list is: every other settings page
  // shows the way back to it where the menu would be (layout.css).
  const index = page === '/settings';
  return <>
    {!index && <a className="settings-back" href="/settings"><ChevronLeftIcon />{messages.allSettings}</a>}
    <nav ref={nav} className={index ? 'settings-nav settings-nav-index' : 'settings-nav'} aria-label={messages.settingsSections}>
      {settingsNavGroups(shown, page).map((group, at) => <div key={group.label ?? at}>{group.label && <p className="settings-group-label">{group.label}</p>}
        <ul>{group.items.map((item) => <li key={item.label} className={item.sub ? 'settings-nav-parent' : undefined} onClick={() => { const section = sectionOf(item); if (section) track('settings_section_opened', { section }); }}>{renderLink(item, page)}
          {item.sub && <ul className="settings-nav-sub" aria-label={item.label}>{item.sub.map((sub) => <li key={sub.label}>{renderLink(sub, page)}</li>)}</ul>}</li>)}</ul>
      </div>)}
    </nav>
  </>;
}
