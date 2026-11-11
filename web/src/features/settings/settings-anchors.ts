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
  'invites-heading': '/settings/members#invites-heading',
  'agents-heading': '/settings/ai#agents-heading',
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
