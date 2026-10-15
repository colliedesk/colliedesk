import { expect, test } from 'vitest';
import { formatBytes } from './format-bytes';

test('formats attachment sizes in binary units', () => {
  expect(formatBytes(0)).toBe('0 B');
  expect(formatBytes(1023)).toBe('1023 B');
  expect(formatBytes(2048)).toBe('2.0 KB');
  expect(formatBytes(5 * 1024 * 1024)).toBe('5.0 MB');
});

test('rounds usage down so it never reads as the limit before it is reached', () => {
  expect(formatBytes(2147000000)).toBe('2.0 GB');
  expect(formatBytes(2147000000, 'down')).toBe('1.9 GB');
  expect(formatBytes(2 * 1024 ** 3, 'down')).toBe('2.0 GB');
});
