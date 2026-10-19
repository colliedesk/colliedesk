import { readFileSync } from 'node:fs';
import { afterEach, expect, test, vi } from 'vitest';
import { applyStoredTheme, onThemeChange, saveTheme, storedTheme, themeStorageKey } from './theme';

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

test('the browser chrome colour follows the theme, and matches the page background token', () => {
  document.head.insertAdjacentHTML('beforeend', '<meta name="theme-color" content="#fafafa">');
  const meta = () => document.querySelector('meta[name="theme-color"]')!.getAttribute('content');
  const tokens = readFileSync('src/styles/tokens.css', 'utf8');
  const surface0 = (block: string) => new RegExp(`${block}[^{]*\\{[^}]*--surface-0:\\s*(#[0-9a-f]{6})`, 'i').exec(tokens)?.[1];
  saveTheme('dark');
  expect(meta()).toBe(surface0(':root\\[data-theme="dark"\\]'));
  saveTheme('light');
  expect(meta()).toBe(surface0(':root, :root\\[data-theme="light"\\]'));
  document.querySelector('meta[name="theme-color"]')!.remove();
});

test('a theme chosen in another tab is applied here', () => {
  const seen: string[] = [];
  const stop = onThemeChange((theme) => seen.push(theme));
  window.dispatchEvent(new StorageEvent('storage', { key: themeStorageKey, newValue: 'dark' }));
  window.dispatchEvent(new StorageEvent('storage', { key: 'something.else', newValue: 'dark' }));
  window.dispatchEvent(new StorageEvent('storage', { key: themeStorageKey, newValue: null }));
  stop();
  window.dispatchEvent(new StorageEvent('storage', { key: themeStorageKey, newValue: 'dark' }));
  expect(seen).toEqual(['dark', 'light']);
});
