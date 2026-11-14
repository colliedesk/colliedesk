import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { attributionFromSearch, clearSignupAttribution, readSignupAttribution, rememberSignupAttribution } from './signup-attribution';

const minute = 60_000;
beforeEach(() => window.sessionStorage.clear());
afterEach(() => vi.restoreAllMocks());

test.each([
  ['nothing', '', {}],
  ['every listed key', '?utm_source=tiktok&utm_medium=social&utm_campaign=launch_2026&utm_content=post-07&utm_term=help.desk&ttclid=E.C.P&fbclid=IwAR_x-1',
    { utm_source: 'tiktok', utm_medium: 'social', utm_campaign: 'launch_2026', utm_content: 'post-07', utm_term: 'help.desk', ttclid: 'E.C.P', fbclid: 'IwAR_x-1' }],
  ['other keys are dropped, a sign-in token above all', '?token=pairing-secret&intent=trial&gclid=abc&utm_source=x', { utm_source: 'x' }],
  ['a value with a space is dropped', '?utm_campaign=spring%20sale', {}],
  ['markup is dropped', '?utm_content=%3Cscript%3E', {}],
  ['an empty value is dropped', '?utm_term=', {}],
  ['301 characters are dropped', `?utm_term=${'a'.repeat(301)}`, {}],
  ['the click id keeps its case', '?fbclid=IwAR3xY', { fbclid: 'IwAR3xY' }],
])('%s', (_name, search, want) => {
  expect(attributionFromSearch(search)).toEqual(want);
});

test('a landing with a click id is kept with the time it was first seen, and sent with the sign-up', () => {
  const start = Date.parse('2026-11-01T10:00:00Z');
  const now = vi.spyOn(Date, 'now').mockReturnValue(start);
  rememberSignupAttribution('?utm_source=meta&fbclid=abc');
  // The identity provider returns to the same address: the click is not seen for the first time again.
  now.mockReturnValue(start + 5 * minute);
  rememberSignupAttribution('?utm_source=meta&fbclid=abc');
  expect(readSignupAttribution()).toEqual({ utm_source: 'meta', fbclid: 'abc', click_id_seen_at: '2026-11-01T10:00:00.000Z' });
});

test('an address without attribution keeps what an earlier landing in the tab remembered', () => {
  rememberSignupAttribution('?utm_source=tiktok&utm_content=post-07');
  rememberSignupAttribution('?intent=trial');
  expect(readSignupAttribution()).toEqual({ utm_source: 'tiktok', utm_content: 'post-07' });
});

test('a new landing replaces the old one, and a new click id is seen now', () => {
  const start = Date.parse('2026-11-01T10:00:00Z');
  const now = vi.spyOn(Date, 'now').mockReturnValue(start);
  rememberSignupAttribution('?utm_source=meta&fbclid=first');
  now.mockReturnValue(start + 10 * minute);
  rememberSignupAttribution('?utm_source=tiktok&fbclid=second');
  expect(readSignupAttribution()).toEqual({ utm_source: 'tiktok', fbclid: 'second', click_id_seen_at: '2026-11-01T10:10:00.000Z' });
});

test('what was remembered is gone after a day, after clearing, or when storage holds something else', () => {
  const start = Date.parse('2026-11-01T10:00:00Z');
  const now = vi.spyOn(Date, 'now').mockReturnValue(start);
  rememberSignupAttribution('?utm_source=tiktok');
  now.mockReturnValue(start + 25 * 60 * minute);
  expect(readSignupAttribution()).toBeUndefined();
  now.mockReturnValue(start);
  rememberSignupAttribution('?utm_source=tiktok');
  clearSignupAttribution();
  expect(readSignupAttribution()).toBeUndefined();
  window.sessionStorage.setItem('colliedesk.signup-attribution', '{"values":{"utm_source":"a b"},"at":1}');
  expect(readSignupAttribution()).toBeUndefined();
});

test('the site whose link first opened the sign-in page in this tab is sent with the sign-up, as a host name only', () => {
  // An AI assistant links straight to the app without utm_* keys; the browser names only the referring page.
  rememberSignupAttribution('', 'https://claude.ai/chat/abc?q=helpdesk', 'app.colliedesk.com');
  // The identity provider's return is a new page load whose referrer is the provider: the first site stands.
  rememberSignupAttribution('?__clerk_status=verified', 'https://accounts.google.com/', 'app.colliedesk.com');
  expect(readSignupAttribution()).toEqual({ referrer: 'claude.ai' });
});

test('a first landing without an outside referrer keeps none, so the identity provider is never taken for the source', () => {
  rememberSignupAttribution('', '', 'app.colliedesk.com');
  rememberSignupAttribution('', 'https://accounts.google.com/', 'app.colliedesk.com');
  expect(readSignupAttribution()).toBeUndefined();
  window.sessionStorage.clear();
  rememberSignupAttribution('', 'https://app.colliedesk.com/inbox', 'app.colliedesk.com');
  rememberSignupAttribution('', 'https://clerk.colliedesk.com/', 'app.colliedesk.com');
  expect(readSignupAttribution()).toBeUndefined();
});

test('the referring site goes with the ad keys, and is forgotten with them', () => {
  rememberSignupAttribution('?utm_source=x&utm_content=post-07', 'https://www.colliedesk.com/pricing', 'app.colliedesk.com');
  expect(readSignupAttribution()).toEqual({ utm_source: 'x', utm_content: 'post-07', referrer: 'www.colliedesk.com' });
  clearSignupAttribution();
  expect(readSignupAttribution()).toBeUndefined();
});

test('a referrer that is not a web address with a host name is not kept', () => {
  rememberSignupAttribution('', 'android-app://com.slack/', 'app.colliedesk.com');
  expect(readSignupAttribution()).toBeUndefined();
  window.sessionStorage.clear();
  window.sessionStorage.setItem('colliedesk.signup-referrer', '{"host":"https://evil.example/x","at":' + Date.now() + '}');
  expect(readSignupAttribution()).toBeUndefined();
});
