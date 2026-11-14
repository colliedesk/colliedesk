import { expect, test } from 'vitest';
import { settingsAddress } from './settings-anchors';

// Settings was one page with a section per anchor; each anchor now names the page that holds the section.
// Links to the old anchors are in bookmarks, sent emails, MCP answers and older guides, so they keep working.
test('every old anchor of the one Settings page opens the page that now holds its section', () => {
  const opened = (hash: string) => settingsAddress(hash, '');
  expect(opened('#profile-heading')).toBe('/settings/account#profile-heading');
  expect(opened('#preferences-heading')).toBe('/settings/account#preferences-heading');
  expect(opened('#sessions-heading')).toBe('/settings/account#sessions-heading');
  expect(opened('#connected-apps-heading')).toBe('/settings/account#connected-apps-heading');
  expect(opened('#general-heading')).toBe('/settings/general#general-heading');
  expect(opened('#team-heading')).toBe('/settings/members#team-heading');
  expect(opened('#invites-heading')).toBe('/settings/members?tab=invites#invites-heading');
  expect(opened('#roles-heading')).toBe('/settings/members?tab=advanced#roles-heading');
  expect(opened('#agents-heading')).toBe('/settings/ai?tab=external#agents-heading');
  expect(opened('#agent-tokens-heading')).toBe('/settings/ai?tab=external#agent-tokens-heading');
  expect(opened('#saved-replies-heading')).toBe('/settings/saved-replies#saved-replies-heading');
  expect(opened('#export-heading')).toBe('/settings/data#export-heading');
  expect(opened('#help-center-heading')).toBe('/knowledge/settings#help-center-heading');
  expect(opened('#email-channel-heading')).toBe('/settings/channels/type/email');
  expect(opened('#web-form-heading')).toBe('/settings/channels/type/web_form');
});

test('the anchor may come without its #, as the router gives it', () => {
  expect(settingsAddress('invites-heading', '')).toBe('/settings/members?tab=invites#invites-heading');
});

test('Settings without a known anchor opens Account', () => {
  expect(settingsAddress('', '')).toBe('/settings/account');
  expect(settingsAddress('#no-such-section', '')).toBe('/settings/account');
});

// A refused export link sends the browser to /settings?export_error=…; the reason is shown where Export all data now is.
test('a refused export lands on Data with its reason', () => {
  expect(settingsAddress('', '?export_error=link_invalid')).toBe('/settings/data?export_error=link_invalid');
  expect(settingsAddress('', 'export_error=link_expired')).toBe('/settings/data?export_error=link_expired');
});
