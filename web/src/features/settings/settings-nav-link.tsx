import { createContext } from 'react';
import type { ReactNode } from 'react';

// The pieces of the Settings menu the app shell draws outside Settings (the sidebar's menu links and slot), apart from
// the menu itself, so the first screen does not load the Settings strings with them.

// Settings is one page per item of this menu, with no sub-items. '/settings' is the menu itself, a page of its own
// on a phone; '/settings/roles' is a role's page, under Members, and a channel type's home is under Channels.
export type SettingsPath = '/settings' | '/settings/account' | '/settings/general' | '/settings/members' | '/settings/saved-replies' | '/settings/tags' | '/settings/data'
  | '/settings/billing' | '/settings/ai' | '/settings/autofix' | '/settings/channels' | '/settings/fields'
  | '/settings/channels/type/email' | '/settings/channels/type/web_form' | '/settings/channels/type/help_chat'
  | '/settings/channels/type/zalo' | '/settings/targets' | '/settings/roles';
// tag: a word after the label (Coming, on a page that is reserved). dot: the name of a warning dot after it.
export interface NavItem { label: string; to: SettingsPath; hash?: string; current?: boolean; tag?: string; dot?: string }

// hrefFor links a page by its path, and a section of one by its anchor as well.
export function hrefFor(item: NavItem): string {
  return item.hash ? `${item.to}#${item.hash}` : item.to;
}

// What a menu link holds: the label, then its tag or its dot. The dot is a picture with a name, so the link's
// text stays the label and a screen reader still hears why it is there.
export function NavItemContent({ item }: { item: NavItem }) {
  return <>
    <span className="settings-nav-label">{item.label}</span>
    {item.tag && <>{' '}<span className="settings-nav-tag">{item.tag}</span></>}
    {item.dot && <>{' '}<span className="settings-nav-dot" role="img" aria-label={item.dot} /></>}
  </>;
}

type RenderLink = (item: NavItem, page: SettingsPath) => ReactNode;
const plainLink: RenderLink = (item) =>
  <a href={hrefFor(item)} aria-current={item.current ? 'page' : undefined}><NavItemContent item={item} /></a>;

// The app provides router links here; without a router (unit tests) the nav uses plain anchors.
export const SettingsNavLinkContext = createContext<RenderLink>(plainLink);

// Where the app's sidebar shows the menu while Settings has it (app-shell.tsx): an element once it is on screen, null
// until then. Each page draws its own menu, from what its roles let it show, and this is where it goes. Without a
// sidebar to hold it (a phone, where the list is the /settings page, or a page tested alone) the menu stays in the page.
export const SettingsMenuSlot = createContext<HTMLElement | null | undefined>(undefined);
