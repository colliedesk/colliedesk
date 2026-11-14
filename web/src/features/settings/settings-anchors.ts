// Settings was one long page with a section per anchor (/settings#invites-heading); each section now lives on
// the page of its menu item. Links to the old anchors are in bookmarks, sent emails, MCP answers and older
// guides, so /settings sends each one to the page that holds its section. Small on purpose: the first bundle
// carries it, as the router decides before any settings chunk loads.
const movedSections: Record<string, string> = {
  'profile-heading': '/settings/account#profile-heading',
  'preferences-heading': '/settings/account#preferences-heading',
  'sessions-heading': '/settings/account#sessions-heading',
  'connected-apps-heading': '/settings/account#connected-apps-heading',
  'general-heading': '/settings/general#general-heading',
  'team-heading': '/settings/members#team-heading',
  'invites-heading': '/settings/members?tab=invites#invites-heading',
  'roles-heading': '/settings/members?tab=advanced#roles-heading',
  'agents-heading': '/settings/ai?tab=external#agents-heading',
  'agent-tokens-heading': '/settings/ai?tab=external#agent-tokens-heading',
  'saved-replies-heading': '/settings/saved-replies#saved-replies-heading',
  'export-heading': '/settings/data#export-heading',
  // The help center's settings moved to the Help center area.
  'help-center-heading': '/knowledge/settings#help-center-heading',
  // The email channel and the web form were sections too, before Channels had pages.
  'email-channel-heading': '/settings/channels/type/email',
  'web-form-heading': '/settings/channels/type/web_form',
};

// settingsAddress is where /settings with this hash and search opens: the page holding an old section, Data
// for a refused export link (/settings?export_error=…, which the server sends), else Account.
export function settingsAddress(hash: string, search: string): string {
  const moved = movedSections[hash.replace(/^#/, '')];
  if (moved) return moved;
  const exportError = new URLSearchParams(search).get('export_error');
  if (exportError) return `/settings/data?${new URLSearchParams({ export_error: exportError })}`;
  return '/settings/account';
}

// Pages whose address changed when the menu was merged. Each address is in bookmarks, emails and MCP answers, and
// opens the page that holds its content now, at the section it had (a hash) or on the tab that holds it.
export const movedPages: Record<string, string> = {
  '/settings/profile': '/settings/account#profile-heading',
  '/settings/preferences': '/settings/account#preferences-heading',
  '/settings/sessions': '/settings/account#sessions-heading',
  '/settings/numbers': '/settings/general#numbers-heading',
  '/settings/roles': '/settings/members?tab=advanced',
  '/settings/agents': '/settings/ai?tab=external',
  '/settings/import': '/settings/data',
  '/settings/export': '/settings/data#export-heading',
  '/settings/import-freshdesk': '/settings/data',
  // The help center's settings are beside the articles they publish.
  '/settings/help-center': '/knowledge/settings',
};

// movedAddress is where an old address opens, with what the link carried: its query goes on to the new page (the
// new address's own parameters, such as the tab, win) and so does its anchor, which names a section more exactly than the
// one the table gives. Without them the table's own section stays.
export function movedAddress(path: string, search: string, hash: string): string {
  const [target, section] = movedPages[path].split('#');
  const [pathname, own = ''] = target.split('?');
  const query = new URLSearchParams(own);
  for (const [key, value] of new URLSearchParams(search)) if (!query.has(key)) query.append(key, value);
  const text = query.toString();
  const anchor = hash.replace(/^#/, '') || section;
  return `${pathname}${text ? `?${text}` : ''}${anchor ? `#${anchor}` : ''}`;
}

// The tabs of Members and of AI & agents are in the address (?tab=). 'roles' is the old name of Advanced.
export const membersTabs = ['people', 'invites', 'groups', 'advanced'] as const;
export type MembersTab = (typeof membersTabs)[number];
export const membersTabOf = (value: unknown): MembersTab | undefined => (value === 'roles' ? 'advanced' : membersTabs.find((tab) => tab === value));
export const aiViews = ['built-in', 'external'] as const;
export type AIView = (typeof aiViews)[number];

// The reserved page's title is needed while its chunk loads, so it is here with the other addresses.
export const conversationFieldsTitle = 'Conversation fields';
