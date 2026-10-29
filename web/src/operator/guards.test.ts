import { describe, expect, test } from 'vitest';
import { confirmMatches, mayAskToType, missing, needs, validCode } from './guards';
import type { Setting } from './api';

const setting = (key: string, extra: Partial<Setting> = {}): Setting => ({ key, group: 'email', secret: false, hot: false, guards: [], set: false, source: 'default', editable: true, ...extra });
const settings = [
  setting('smtp.host', { guards: ['reauth', 'reenter_secret'], reenter: 'smtp.password' }),
  setting('smtp.password', { secret: true, guards: ['reauth'], set: true }),
  setting('internal_signup.emails', { group: 'signups', hot: true }),
  setting('mail_from.domain', { guards: ['type_to_confirm'] }),
];

describe('what saving asks for', () => {
  test('a relay change asks for the code and the password again; a hot setting asks nothing', () => {
    const asked = needs({ set: { 'smtp.host': 'smtp.example.net' }, unset: [], secrets: {} }, settings);
    expect(asked.reauth).toBe(true);
    expect(asked.reenter).toEqual(['smtp.password']);
    const hot = needs({ set: { 'internal_signup.emails': '@a.example' }, unset: [], secrets: {} }, settings);
    expect(hot).toMatchObject({ reauth: false, reenter: [], confirm: {} });
  });
  test('a password typed in the change is not asked again', () => {
    expect(needs({ set: { 'smtp.host': 'x' }, unset: [], secrets: { 'smtp.password': 'pw' } }, settings).reenter).toEqual([]);
  });
  test('typing to confirm ignores case and spaces around, as the server', () => {
    expect(confirmMatches('  Mail.Example.COM ', 'mail.example.com')).toBe(true);
    expect(confirmMatches('mail.example.co', 'mail.example.com')).toBe(false);
  });
  test('the phrase to type and what a change does come from the server, never from the change itself', () => {
    const draft = { set: { 'mail_from.domain': 'reply.example.com' }, unset: [], secrets: {} };
    expect(needs(draft, settings).confirm).toEqual({});
    expect(mayAskToType(draft, settings)).toBe(true);
    const asked = needs(draft, settings, [{ key: 'mail_from.domain', phrase: 'reply.example.com', preview: 'Replies to mail sent before go to mail.example.com' }]);
    expect(asked).toMatchObject({ reauth: true, confirm: { 'mail_from.domain': 'reply.example.com' }, preview: ['Replies to mail sent before go to mail.example.com'], trial: false });
    expect(needs({ set: { trusted_proxies: '10.0.0.0/8' }, unset: [], secrets: {} }, settings, [{ key: 'trusted_proxies', trial: true }]).trial).toBe(true);
  });
  test('the review lists what is missing until every check is filled', () => {
    const asked = needs({ set: { 'smtp.host': 'x', 'mail_from.domain': 'mail.example.com' }, unset: [], secrets: {} }, settings, [{ key: 'mail_from.domain', phrase: 'mail.example.com' }]);
    expect(missing(asked, '', {}, {}, true)).toEqual(['enter smtp.password again', 'type mail.example.com', 'your 6-digit code']);
    expect(missing(asked, '123 456', { 'mail_from.domain': 'MAIL.example.com' }, { 'smtp.password': 'pw' }, true)).toEqual([]);
    expect(validCode('12345')).toBe(false);
  });
});
