// @vitest-environment node
import { readFileSync } from 'node:fs';
import { expect, test } from 'vitest';
import { themeTokens } from './check-contrast.mjs';

// Form controls follow the "ink and paper" brand (docs/internals/design.md): one shared rule in base.css styles every
// select, checkbox and radio from tokens, so a control never falls back to the browser look or to a colour that stays
// fixed when the theme changes.
const read = (name) => readFileSync(new URL(`../src/styles/${name}`, import.meta.url), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
const base = read('base.css');
const components = read('components.css');
const tokens = readFileSync(new URL('../src/styles/tokens.css', import.meta.url), 'utf8');

function rule(css, selector) {
  const at = css.indexOf(`${selector} {`);
  expect(at, `missing rule "${selector}"`).toBeGreaterThanOrEqual(0);
  return css.slice(at, css.indexOf('}', at));
}

test('select is drawn by the app with a chevron in theme tokens, not a fixed image', () => {
  const select = rule(base, 'select');
  expect(select).toMatch(/appearance:\s*none/);
  expect(select).toMatch(/background-image:\s*var\(--select-chevron\)/);
  expect(select).not.toMatch(/url\(|#[0-9a-f]{3,6}\b|%23[0-9a-f]{6}/i);
});

test('the chevron is drawn in --text-3 of each theme, so it never stays dark in dark mode', () => {
  for (const theme of ['light', 'dark']) {
    const block = tokens.match(new RegExp(`:root\\[data-theme="${theme}"\\]\\s*\\{([^}]*)\\}`))[1];
    const stroke = block.match(/--select-chevron:[^;]*stroke='%23([0-9a-f]{6})'/i)?.[1];
    expect(`#${stroke}`, theme).toBe(themeTokens(tokens, theme)['--text-3']);
  }
});

test('a disabled select keeps its chevron', () => {
  const disabled = rule(base, 'input:disabled, select:disabled, textarea:disabled');
  expect(disabled).not.toMatch(/background:/);
});

test('checkboxes and radios share one ink rule in base.css', () => {
  const check = rule(base, 'input:where([type="checkbox"], [type="radio"])');
  expect(check).toMatch(/accent-color:\s*var\(--accent\)/);
  expect(check).toMatch(/width:\s*var\(--space-4\)/);
  expect(components).not.toMatch(/accent-color/);
});

test('text-input sizing does not reach checkboxes and radios', () => {
  expect(base).toMatch(/input:where\(:not\(\[type="checkbox"\]\):not\(\[type="radio"\]\)\), select, textarea \{/);
});

test('visually hidden inputs still cover their whole label', () => {
  for (const selector of ['.status-tabs input', '.tag-check input', '.segmented input']) {
    expect(rule(components, selector)).toMatch(/width:\s*100%;\s*height:\s*100%/);
  }
});
