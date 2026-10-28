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
  expect(themeTokens(css, 'dark')['--surface-0']).toBe('#0a0a0a');
});

// Brand "ink and paper" (docs/internals/design.md): surfaces, lines, text and the primary action are
// neutral grey or ink, with no hue. Colour is kept for status, focus, private notes and the logo tile.
const NEUTRAL = ['--surface-0', '--surface-1', '--surface-2', '--surface-3', '--surface-inverse', '--line', '--line-strong',
  '--line-control', '--text-1', '--text-2', '--text-3', '--text-inverse', '--accent', '--accent-hover', '--accent-text',
  '--accent-soft', '--on-accent', '--brand-panel', '--on-brand-panel', '--on-brand-panel-2'];
test('greys, text and the primary action carry no hue in either theme', () => {
  for (const theme of ['light', 'dark']) {
    const tokens = themeTokens(css, theme);
    const tinted = NEUTRAL.filter((name) => {
      const [r, g, b] = tokens[name].slice(1).match(/../g);
      return !(r === g && g === b);
    });
    expect(tinted.map((name) => `${theme} ${name} ${tokens[name]}`)).toEqual([]);
  }
});

// The logo is a white collie head on an orange tile, the same in both themes, the favicon and the site.
test('the logo tile is the brand orange in both themes', () => {
  for (const theme of ['light', 'dark']) expect(themeTokens(css, theme)['--logo-tile']).toBe('#c2410c');
  expect(contrast('#fafafa', '#c2410c')).toBeGreaterThanOrEqual(4.5);
});

test('every token pair in both themes meets its WCAG minimum', () => {
  for (const { theme, failures } of report(css)) {
    expect(failures.map((row) => `${theme} ${row.fg} on ${row.bg} ${row.ratio.toFixed(2)}`)).toEqual([]);
  }
});
