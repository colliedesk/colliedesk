import { expect, test } from 'vitest';
import { initials } from './avatar';

test('takes the first and last initial of a name or address', () => {
  expect(initials('Priya Raman')).toBe('PR');
  expect(initials('Léa Anne Martin')).toBe('LM');
  expect(initials('maya@acme.test')).toBe('MT');
  expect(initials('Jon')).toBe('J');
  expect(initials('  ')).toBe('?');
});
