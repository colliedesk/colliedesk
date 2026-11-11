import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { clearPlanIntent, parsePlanIntent, planIntentFromNext, readPlanIntent, rememberPlanIntent, signInPlanIntent } from './plan-intent';

const hour = 60 * 60_000;
beforeEach(() => window.sessionStorage.clear());
afterEach(() => vi.restoreAllMocks());

test('only team25 is a plan a visitor can come for; every other value is ignored', () => {
  expect(parsePlanIntent('team25')).toBe('team25');
  for (const other of ['Team25', 'team', 'solo', 'founding', 'team25_monthly', ' team25', '', undefined, null, 25, ['team25']]) {
    expect(parsePlanIntent(other)).toBeUndefined();
  }
});

test('an intent is readable for a day and gone after that', () => {
  const start = Date.parse('2026-10-30T10:00:00Z');
  const now = vi.spyOn(Date, 'now').mockReturnValue(start);
  rememberPlanIntent('team25');
  now.mockReturnValue(start + 23 * hour);
  expect(readPlanIntent()).toBe('team25');
  now.mockReturnValue(start + 25 * hour);
  expect(readPlanIntent()).toBeUndefined();
});

test('clearing removes the intent, and stored values this code did not write count as none', () => {
  rememberPlanIntent('team25');
  clearPlanIntent();
  expect(readPlanIntent()).toBeUndefined();
  for (const stored of ['not json', '{"plan":"solo","at":1}', `{"plan":"team25","at":"yesterday"}`, '["team25"]', 'null']) {
    window.sessionStorage.setItem('colliedesk.plan-intent', stored);
    expect(readPlanIntent()).toBeUndefined();
  }
});

test('storage that throws is no intent and breaks nothing', () => {
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
  expect(() => rememberPlanIntent('team25')).not.toThrow();
  expect(readPlanIntent()).toBeUndefined();
});

test('a sign-in address naming the plan remembers it, and a plain sign-in forgets it', () => {
  expect(signInPlanIntent({ plan: 'team25' })).toBe('team25');
  expect(readPlanIntent()).toBe('team25');
  expect(signInPlanIntent({})).toBeUndefined();
  expect(readPlanIntent()).toBeUndefined();
});

test('a one-time link keeps the plan remembered before it, and an unknown plan in the address is not remembered', () => {
  rememberPlanIntent('team25');
  expect(signInPlanIntent({ token: 'pairing-token' })).toBe('team25');
  expect(readPlanIntent()).toBe('team25');
  expect(signInPlanIntent({ plan: 'solo' })).toBeUndefined();
  expect(readPlanIntent()).toBeUndefined();
});

test('the plan rides in the page a hand-off opens, and only team25 is taken from it', () => {
  expect(planIntentFromNext('/?plan=team25')).toEqual({ plan: 'team25', path: '/' });
  expect(planIntentFromNext('/settings?tab=x&plan=team25')).toEqual({ plan: 'team25', path: '/settings?tab=x' });
  expect(planIntentFromNext('/?plan=solo')).toEqual({ path: '/?plan=solo' });
  expect(planIntentFromNext('/settings')).toEqual({ path: '/settings' });
  expect(planIntentFromNext(undefined)).toEqual({ path: '/' });
  expect(planIntentFromNext('//evil.example/?plan=team25')).toEqual({ path: '/' });
});
