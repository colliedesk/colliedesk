import { expect, test } from 'vitest';
import { formatBytes } from './format-bytes';

test('formats attachment sizes in binary units', () => {
  expect(formatBytes(0)).toBe('0 B');
  expect(formatBytes(1023)).toBe('1023 B');
  expect(formatBytes(2048)).toBe('2.0 KB');
  expect(formatBytes(5 * 1024 * 1024)).toBe('5.0 MB');
});
