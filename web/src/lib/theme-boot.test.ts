import { readFileSync } from 'node:fs';
import { afterEach, expect, test } from 'vitest';
import { themeStorageKey } from './theme';

// index.html must set the stored theme in <head>, before first paint and before the module bundle, or a dark
// user sees a light page first. The boot script is plain inline JavaScript run by the parser.
const html = readFileSync('index.html', 'utf8');
const boot = html.match(/<script>([\s\S]*?)<\/script>/)?.[1] ?? '';
const run = () => new Function(boot)();

afterEach(() => { localStorage.clear(); document.documentElement.removeAttribute('data-theme'); });

test('the boot script runs before any stylesheet or module script', () => {
  expect(boot).toContain(themeStorageKey);
  const at = html.indexOf('<script>');
  const firstLoad = Math.min(...[html.indexOf('<link rel="stylesheet"'), html.indexOf('<script type="module"')].filter((i) => i >= 0));
  expect(at).toBeGreaterThan(-1);
  expect(at).toBeLessThan(firstLoad);
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
