import { readFileSync } from 'node:fs';
import { afterEach, expect, test } from 'vitest';

// operator.html sets the stored console theme before first paint with an external script (its CSP allows no
// inline script), before any stylesheet or module.
const page = new DOMParser().parseFromString(readFileSync('operator.html', 'utf8'), 'text/html');
const boot = [...page.head.querySelectorAll('script')].find((script) => script.getAttribute('src') === '/operator/theme-boot.js');
const code = readFileSync('public-operator/theme-boot.js', 'utf8');

afterEach(() => { localStorage.clear(); document.documentElement.removeAttribute('data-theme'); });

test('the console boots its theme from its own file, before its module', () => {
  expect(boot).toBeDefined();
  expect([...page.querySelectorAll('script')].every((script) => script.getAttribute('src'))).toBe(true);
  const module = page.querySelector('script[type="module"]')!;
  expect(boot!.compareDocumentPosition(module) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

test('the console keeps its own theme choice, apart from the app', () => {
  localStorage.setItem('colliedesk.theme', 'dark');
  new Function(code)();
  expect(document.documentElement.dataset.theme).not.toBe('dark');
  localStorage.setItem('colliedesk.operator.theme', 'dark');
  const meta = document.createElement('meta');
  meta.name = 'theme-color';
  document.head.append(meta);
  new Function(code)();
  expect(document.documentElement.dataset.theme).toBe('dark');
  expect(meta.content).toBe('#0a0a0a');
  meta.remove();
});
