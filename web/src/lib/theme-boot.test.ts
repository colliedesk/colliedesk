import { readFileSync } from 'node:fs';
import { afterEach, expect, test } from 'vitest';
import { themeStorageKey } from './theme';

// index.html must set the stored theme in <head>, before first paint and before the module bundle, or a dark
// user sees a light page first. The boot script is plain inline JavaScript run by the parser.
const page = new DOMParser().parseFromString(readFileSync('index.html', 'utf8'), 'text/html');
const bootElement = [...page.head.querySelectorAll('script')].find((script) => !script.src && !script.type);
const boot = bootElement?.textContent ?? '';
const run = () => new Function(boot)();

afterEach(() => { localStorage.clear(); document.documentElement.removeAttribute('data-theme'); });

test('the boot script runs before any stylesheet or module script', () => {
  expect(boot).toContain(themeStorageKey);
  const loads = page.querySelectorAll('link[rel="stylesheet"], script[type="module"]');
  expect(loads.length).toBeGreaterThan(0);
  for (const load of loads) expect(bootElement!.compareDocumentPosition(load) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

test('a stored dark choice is on <html> before the app loads', () => {
  localStorage.setItem(themeStorageKey, 'dark');
  run();
  expect(document.documentElement.dataset.theme).toBe('dark');
});

test('nothing stored leaves the page light', () => {
  run();
  expect(document.documentElement.dataset.theme).not.toBe('dark');
});

test('a stored dark choice also paints the browser chrome dark', () => {
  const meta = document.createElement('meta');
  meta.name = 'theme-color';
  meta.content = '#fafafa';
  document.head.append(meta);
  localStorage.setItem(themeStorageKey, 'dark');
  run();
  expect(meta.content).toBe('#0a0a0a');
  meta.remove();
});

test('the theme-color tag is parsed before the boot script that rewrites it', () => {
  const tag = page.head.querySelector('meta[name="theme-color"]');
  expect(tag).not.toBeNull();
  expect(tag!.compareDocumentPosition(bootElement!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});
