// @vitest-environment node
import { readFileSync } from 'node:fs';
import { expect, test } from 'vitest';
import { contrast, report, themeTokens } from './check-contrast.mjs';

const css = readFileSync(new URL('../src/styles/tokens.css', import.meta.url), 'utf8');

test('contrast matches the WCAG reference values', () => {
  expect(contrast('#000000', '#ffffff')).toBeCloseTo(21, 5);
  expect(contrast('#777777', '#ffffff')).toBeCloseTo(4.48, 2);
});

test('light is the default block and dark is opt-in', () => {
  expect(css).toMatch(/:root, :root\[data-theme="light"\] \{\s*color-scheme: light;/);
  expect(themeTokens(css, 'light')['--on-accent']).toBe('#ffffff');
  expect(themeTokens(css, 'dark')['--surface-0']).toBe('#0b0f15');
});

test('every token pair in both themes meets its WCAG minimum', () => {
  for (const { theme, failures } of report(css)) {
    expect(failures.map((row) => `${theme} ${row.fg} on ${row.bg} ${row.ratio.toFixed(2)}`)).toEqual([]);
  }
});
