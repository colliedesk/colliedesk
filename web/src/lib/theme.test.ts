import { afterEach, expect, test, vi } from 'vitest';
import { applyStoredTheme, saveTheme, storedTheme, themeStorageKey } from './theme';

afterEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  vi.unstubAllGlobals();
});

test('defaults to light with nothing stored, even when the OS prefers dark', () => {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: query.includes('dark'), media: query, addEventListener() {}, removeEventListener() {} }));
  expect(storedTheme(localStorage)).toBe('light');
  applyStoredTheme();
  expect(document.documentElement.dataset.theme).toBe('light');
});

test('a saved dark choice survives a reload', () => {
  saveTheme('dark');
  expect(localStorage.getItem('colliedesk.theme')).toBe('dark');
  expect(document.documentElement.dataset.theme).toBe('dark');
  document.documentElement.removeAttribute('data-theme');
  applyStoredTheme();
  expect(document.documentElement.dataset.theme).toBe('dark');
});

test('an unknown stored value falls back to light', () => {
  localStorage.setItem(themeStorageKey, 'sepia');
  expect(storedTheme(localStorage)).toBe('light');
});

test('blocked storage still applies the theme for this page', () => {
  const blocked = { getItem: () => { throw new Error('denied'); }, setItem: () => { throw new Error('denied'); } };
  expect(storedTheme(blocked)).toBe('light');
  saveTheme('dark', blocked);
  expect(document.documentElement.dataset.theme).toBe('dark');
});
