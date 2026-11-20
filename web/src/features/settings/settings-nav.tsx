import { useContext, useEffect, useRef } from 'react';
import type { MouseEvent } from 'react';
import { createPortal } from 'react-dom';
import { QueryClientContext, useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import type { Permissions } from '../../lib/can';
import { track } from '../../lib/product-analytics';
import type { SettingsSection } from '../../lib/product-analytics';
import { messages } from '../../messages';
import { ChevronLeftIcon } from '../../ui/icons';
import { autofixPreviewOn } from '../autofix/autofix-preview';
import { channelNeedsAction } from './channel-problem';
import { settingsMessages } from './settings-messages';
import { hrefFor, NavItemContent, SettingsMenuSlot, SettingsNavLinkContext } from './settings-nav-link';
import type { NavItem, SettingsPath } from './settings-nav-link';

interface NavGroup { label?: string; items: NavItem[] }

const sectionByPage: Partial<Record<SettingsPath, SettingsSection>> = {
  '/settings/account': 'account', '/settings/general': 'general', '/settings/members': 'members', '/settings/channels': 'channels', '/settings/targets': 'targets',
  '/settings/ai': 'ai', '/settings/saved-replies': 'saved_replies', '/settings/data': 'data', '/settings/billing': 'billing',
};
const sectionOf = (item: NavItem): SettingsSection | undefined => sectionByPage[item.to] ?? (item.to.startsWith('/settings/channels') ? 'channels' : undefined);

export interface Shown { general: boolean; team: boolean; channels: boolean; savedReplies: boolean; exportData: boolean; billing: boolean; importData?: boolean; ai?: boolean; agents?: boolean; autofix?: boolean; roles?: boolean; invites?: boolean; conversations?: boolean; tags?: boolean }

// byPermission shows the workspace pages the session may use, whatever a page passed: a person sees a
// page when their roles hold what it needs.
export function byPermission(shown: Shown, me: Permissions): Shown {
  if (!me) return shown;
  return { ...shown, general: can(me, 'workspace:update'), team: can(me, 'team:read'), roles: can(me, 'team:read'), invites: can(me, 'team:read'),
    exportData: can(me, 'workspace:export'), importData: can(me, 'imports:run'),
    // Someone who may revoke teammates' sessions finds their agent tokens on AI & agents, without making tokens.
    agents: shown.agents !== false && (can(me, 'team:create_tokens') || can(me, 'team:revoke')),
    channels: shown.channels && can(me, 'channels:read'), conversations: can(me, 'conversations:read'), ai: shown.ai !== false && can(me, 'ai_settings:read'),
    savedReplies: shown.savedReplies && can(me, 'saved_replies:read'), tags: can(me, 'tags:read') };
}

// Dots are the one thing in the menu that comes from data: whether a channel has a problem and whether the last
// payment failed.
export interface Dots { channels?: boolean; billing?: boolean }

// settingsNavGroups lists the pages this person can see: their own account, then the shared workspace,
// then its data and billing. A page shows when the person can see one of its sections at least.
export function settingsNavGroups(shown: Shown, page: SettingsPath, dots: Dots = {}): NavGroup[] {
  const item = (label: string, to: SettingsPath, visible = true, current = page === to, extra: Partial<NavItem> = {}): NavItem[] => (visible ? [{ label, to, current, ...extra }] : []);
  const groups: NavGroup[] = [
    { items: item(messages.account, '/settings/account') },
    { label: messages.settingsWorkspace, items: [
      // Conversation numbers read conversations, not the workspace's own settings: General shows for either too.
      // The help center's settings are in Knowledge base → Settings, not here.
      ...item(messages.general, '/settings/general', shown.general || Boolean(shown.conversations)),
      ...item(messages.members, '/settings/members', shown.team || shown.invites === true || shown.roles === true, page === '/settings/members' || page === '/settings/roles'),
      ...item(messages.channels, '/settings/channels', shown.channels, page.startsWith('/settings/channels'), dots.channels ? { dot: settingsMessages.navChannelProblem } : {}),
      // Response targets read conversations, not channels.
      ...item(messages.responseTargets, '/settings/targets', shown.conversations ?? shown.channels),
      ...item(messages.aiAndAgents, '/settings/ai', Boolean(shown.ai) || Boolean(shown.agents)),
      ...item(messages.savedReplies, '/settings/saved-replies', shown.savedReplies),
      ...item(messages.tags, '/settings/tags', Boolean(shown.tags)),
      // Reserved: the page says what is coming, so the place is in the menu before the feature is.
      ...item(settingsMessages.conversationFields, '/settings/fields', shown.conversations ?? shown.channels, page === '/settings/fields', { tag: settingsMessages.coming }),
      // Autofix shows even on a server without its GitHub App: the page names the settings the operator still needs.
      // While in preview it shows only in a browser that turned it on (autofix-preview.ts).
      ...item(messages.autofix, '/settings/autofix', shown.autofix !== false && autofixPreviewOn()),
    ] },
    { label: messages.settingsDataAndBilling, items: [
      ...item(messages.dataPage, '/settings/data', Boolean(shown.importData) || shown.exportData),
      // Plan and billing exists only on the hosted cloud, for those who read the billing.
      ...item(messages.planAndBilling, '/settings/billing', shown.billing, page === '/settings/billing', dots.billing ? { dot: settingsMessages.navPaymentFailed } : {}),
    ] },
  ];
  return groups.filter((group) => group.items.length > 0);
}

// Each settings page draws its own menu, so the link pressed is gone once the page it opens is drawn. The
// address it opened is kept here until that page's menu is drawn, which then gives keyboard focus back to
// its current item, or to the page's title where the menu is not shown (a page on a phone).
let opening: string | undefined;
function rememberOpening(event: MouseEvent<HTMLElement>, page: SettingsPath) {
  const to = (event.target as HTMLElement).closest('a')?.getAttribute('href')?.split(/[?#]/)[0];
  opening = to && to !== page ? to : undefined;
}
function focusOpened(nav: HTMLElement | null, page: SettingsPath) {
  if (!nav || opening !== page) return;
  opening = undefined;
  const current = nav.querySelector<HTMLElement>('[aria-current="page"]');
  if (current && getComputedStyle(nav).display !== 'none') { current.focus(); return; }
  const title = nav.closest('main')?.querySelector<HTMLElement>('h1');
  if (!title) return;
  title.tabIndex = -1;
  title.focus();
}

type NavProps = { shown: Shown; page: SettingsPath };

// With a query cache (the app) the menu asks what its dots need; without one (a page tested alone) it has none.
export function SettingsNav(props: NavProps) {
  return useContext(QueryClientContext) ? <LiveSettingsNav {...props} /> : <SettingsMenu {...props} dots={{}} />;
}

// The channel list is the sidebar's own request, so a channel that needs attention costs no more than the sidebar does.
// The billing read is made only where Plan and billing shows. Both stay fresh for a minute: the menu is drawn again by
// every page, and must not read again what the page that draws it has just read.
function LiveSettingsNav(props: NavProps) {
  const client = useContext(QueryClientContext);
  const shown = byPermission(props.shown, client?.getQueryData<{ permissions: string[] }>(['me']));
  const channels = useQuery({ queryKey: ['channels', 'counts'], queryFn: api.channelCounts, enabled: shown.channels, staleTime: 60_000 });
  const billing = useQuery({ queryKey: ['billing'], queryFn: api.billing, enabled: shown.billing, staleTime: 60_000 });
  return <SettingsMenu {...props} dots={{
    channels: shown.channels && channels.data?.items.some(channelNeedsAction),
    billing: shown.billing && (billing.data?.status === 'past_due' || billing.data?.status === 'unpaid'),
  }} />;
}

function SettingsMenu({ shown: given, page, dots }: NavProps & { dots: Dots }) {
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
  useEffect(() => focusOpened(nav.current, page), [page]);
  // On a phone the menu is a page of its own (/settings), as the inbox list is: every other settings page
  // shows the way back to it where the menu would be (layout.css).
  const index = page === '/settings';
  const slot = useContext(SettingsMenuSlot);
  const menu =
    <nav ref={nav} onClick={(event) => rememberOpening(event, page)} className={index ? 'settings-nav settings-nav-index' : 'settings-nav'} aria-label={messages.settingsSections}>
      {settingsNavGroups(shown, page, dots).map((group, at) => <div key={group.label ?? at}>{group.label && <p className="settings-group-label">{group.label}</p>}
        <ul>{group.items.map((item) => <li key={item.label} onClick={() => { const section = sectionOf(item); if (section) track('settings_section_opened', { section }); }}>{renderLink(item, page)}</li>)}</ul>
      </div>)}
    </nav>;
  return <>
    {!index && <a className="settings-back" href="/settings" aria-label={messages.allSettings} onClick={(event) => rememberOpening(event, page)}><ChevronLeftIcon />{messages.settings}</a>}
    {slot === undefined ? menu : slot && createPortal(menu, slot)}
  </>;
}
