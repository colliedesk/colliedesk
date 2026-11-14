import { expect, test } from 'vitest';
import { hoursText, startingHours } from './business-hours';

const day = (name: string, open: boolean, from?: string, to?: string) => ({ day: name, open, from, to });
const week = (from: string, to: string) => [...['mon', 'tue', 'wed', 'thu', 'fri'].map((name) => day(name, true, from, to)), day('sat', false), day('sun', false)];

test('a Monday to Friday week with one set of hours reads as one phrase', () => {
  expect(hoursText(week('09:00', '18:00'))).toBe('Monday to Friday, 09:00 to 18:00');
});

test('days with different hours are listed one after the other, a single day by its name', () => {
  const hours = [...week('08:00', '17:00').slice(0, 4), day('fri', true, '08:00', '12:00'), day('sat', false), day('sun', true, '10:00', '14:00')];
  expect(hoursText(hours)).toBe('Monday to Thursday, 08:00 to 17:00; Friday, 08:00 to 12:00; Sunday, 10:00 to 14:00');
});

test('a day that closes at midnight reads as 00:00, and two days in a row read as a pair', () => {
  const hours = [day('mon', false), day('tue', false), day('wed', false), day('thu', false), day('fri', false), day('sat', true, '09:00', '24:00'), day('sun', true, '09:00', '24:00')];
  expect(hoursText(hours)).toBe('Saturday and Sunday, 09:00 to 00:00');
});

test('hours without an open day read as closed', () => {
  expect(hoursText(week('09:00', '18:00').map((item) => ({ ...item, open: false })))).toBe('closed every day');
});

const fallback = week('09:00', '18:00');

test('the hours already saved start the clock; with no open day the fallback does', () => {
  const saved = week('08:00', '17:00');
  expect(startingHours(saved, fallback)).toEqual({ hours: saved, kept: true });
  const closed = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].map((name) => day(name, false, '08:00', '17:00'));
  expect(startingHours(closed, fallback)).toEqual({ hours: fallback, kept: false });
  expect(startingHours([], fallback)).toEqual({ hours: fallback, kept: false });
});
