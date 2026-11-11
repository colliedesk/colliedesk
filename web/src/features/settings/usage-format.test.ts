import { expect, test } from 'vitest';
import { gibOf } from './usage-format';

const GiB = 1024 ** 3;

test('a size in GiB has one decimal and no trailing zero', () => {
  expect(gibOf(0.2 * GiB)).toBe('0.2');
  expect(gibOf(3.1 * GiB)).toBe('3.1');
  expect(gibOf(2 * GiB)).toBe('2');
  expect(gibOf(250 * GiB)).toBe('250');
  expect(gibOf(12 * 1024)).toBe('0');
});

test('usage just under a whole GiB is cut down, never rounded up to the limit', () => {
  expect(gibOf(2147000000)).toBe('1.9');
  expect(gibOf(2 * GiB - 1)).toBe('1.9');
});
