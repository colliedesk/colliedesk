import { expect, test } from 'vitest';
import { formatMinutes } from './format-minutes';

test('formats logged minutes as hours and minutes', () => {
  expect(formatMinutes(0)).toBe('0 min');
  expect(formatMinutes(30)).toBe('30 min');
  expect(formatMinutes(120)).toBe('2 h');
  expect(formatMinutes(105)).toBe('1 h 45 min');
});
