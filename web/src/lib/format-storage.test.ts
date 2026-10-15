import { expect, test } from 'vitest';
import { formatStorage } from './format-storage';

test('writes plan storage sizes in GiB, MiB and KiB with one decimal at most', () => {
  expect(formatStorage(20 * 1024 ** 3)).toBe('20 GiB');
  expect(formatStorage(17.5 * 1024 ** 3)).toBe('17.5 GiB');
  expect(formatStorage(300 * 1024 ** 2)).toBe('300 MiB');
  expect(formatStorage(2048)).toBe('2 KiB');
  expect(formatStorage(0)).toBe('0 KiB');
});
