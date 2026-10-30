// @vitest-environment node
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { expect, test } from 'vitest';

// Brand guideline rules of docs/internals/design.md that a stylesheet can break.
// "Colour means something" (rule 2): amber is focus, private
// notes and the pending status, nothing else. A warning uses the warning tokens (--warn, --warn-soft),
// so a reader never takes it for a private note.
const read = (path) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
const amber = /var\(--(?:note-[a-z]+|focus|focus-ring|pending)\)/;

function rule(css, selector) {
  const at = css.indexOf(`\n${selector} {`);
  expect(at, `missing rule "${selector}"`).toBeGreaterThanOrEqual(0);
  return css.slice(at, css.indexOf('}', at));
}

const warnings = [
  // The Knowledge page banner "The help center is off": a neutral card, the warning colour on its icon.
  ['features/kb/kb.css', ['.kb-notice', '.kb-notice > svg']],
  // The thread badge "Unverified sender (web form)" and "Sender not verified".
  ['styles/components.css', ['.badge-none']],
];

for (const [file, selectors] of warnings) {
  test(`${selectors[0]} is a warning, not amber`, () => {
    const css = selectors.map((selector) => rule(read(file), selector)).join('\n');
    expect(css).not.toMatch(amber);
    expect(css).toMatch(/var\(--warn(?:-soft)?\)/);
  });
}

// Avatars (design.md, Rules): the hue tells people apart; it is low chroma, comes from tokens, and is
// never used for status. The initials stay readable (4.5:1) on every hue the app picks.
const tokens = readFileSync(new URL('../src/styles/tokens.css', import.meta.url), 'utf8');
const shared = tokens.match(/^:root \{([^}]*)\}/m)[1];
const token = (name) => Number(shared.match(new RegExp(`${name}:\\s*([0-9.]+)`))?.[1]);
const hues = readFileSync(new URL('../src/ui/avatar.tsx', import.meta.url), 'utf8').match(/const hues = \[([^\]]*)\]/)[1].split(',').map(Number);

// Relative luminance of an OKLCH colour (Björn Ottosson's OKLab matrices), clamped to sRGB.
function luminance(l, c, h) {
  const a = c * Math.cos((h * Math.PI) / 180), b = c * Math.sin((h * Math.PI) / 180);
  const [L, M, S] = [l + 0.3963377774 * a + 0.2158037573 * b, l - 0.1055613458 * a - 0.0638541728 * b, l - 0.0894841775 * a - 1.291485548 * b].map((v) => v ** 3);
  const clamp = (v) => Math.min(1, Math.max(0, v));
  const r = clamp(4.0767416621 * L - 3.3077115913 * M + 0.2309699292 * S);
  const g = clamp(-1.2684380046 * L + 2.6097574011 * M - 0.3413193965 * S);
  const bl = clamp(-0.0041960863 * L - 0.7034186147 * M + 1.707614701 * S);
  return 0.2126 * r + 0.7152 * g + 0.0722 * bl;
}

test('avatar colours come from tokens, not numbers in the component rule', () => {
  const avatar = rule(read('styles/components.css'), '.avatar');
  expect(avatar).toMatch(/background:\s*oklch\(var\(--avatar-lightness\) var\(--avatar-chroma\) var\(--avatar-hue\)\)/);
  expect(avatar).toMatch(/color:\s*oklch\(var\(--avatar-text-lightness\) var\(--avatar-text-chroma\) var\(--avatar-hue\)\)/);
});

test('avatar hues are low chroma and their initials pass 4.5:1 on every hue', () => {
  expect(token('--avatar-chroma')).toBeLessThanOrEqual(0.08);
  expect(token('--avatar-text-chroma')).toBeLessThanOrEqual(0.08);
  for (const hue of hues) {
    const tile = luminance(token('--avatar-lightness'), token('--avatar-chroma'), hue);
    const text = luminance(token('--avatar-text-lightness'), token('--avatar-text-chroma'), hue);
    expect((Math.max(tile, text) + 0.05) / (Math.min(tile, text) + 0.05), `hue ${hue}`).toBeGreaterThanOrEqual(4.5);
  }
});

// "Rings, not borders" (rule 4): the composer is a raised card on its ring; a 1px border on top of the
// ring drew a second outline. The amber focus outline and the amber note ring stay.
test('the composer card sits on its ring, without a border', () => {
  const components = read('styles/components.css');
  expect(rule(components, '.composer-card')).not.toMatch(/border:\s*1px/);
  expect(rule(components, '.composer-card')).toMatch(/box-shadow:\s*var\(--shadow-2\)/);
  expect(rule(components, '.composer-card[data-kind="note"]')).not.toMatch(/border-color/);
  expect(rule(components, '.composer-card[data-kind="note"]')).toMatch(/box-shadow:\s*inset 0 0 0 1px var\(--note-line\), var\(--shadow-2\)/);
  expect(rule(components, '.composer-card:focus-within')).toMatch(/outline:\s*2px solid var\(--focus\)/);
});

// "Ink, paper and one orange" (rule 2): the orange belongs to Colliedesk itself, the logo tile. Only the
// token file and the mark may name it, so it never turns up on a button, chip or warning.
test('the brand orange appears only in the logo', () => {
  const root = new URL('../src/', import.meta.url);
  const files = (dir) => readdirSync(dir).flatMap((name) => {
    const url = new URL(name, dir);
    return statSync(url).isDirectory() ? files(new URL(`${name}/`, dir)) : [url];
  });
  // The operator console draws the same mark coloured by class (its CSP allows no inline style).
  const allowed = ['styles/tokens.css', 'ui/logo.tsx', 'operator/operator.css'];
  const offenders = files(root)
    .filter((url) => /\.(css|tsx?)$/.test(url.pathname) && !url.pathname.endsWith('.test.tsx') && !url.pathname.endsWith('.test.ts'))
    .map((url) => url.pathname.slice(root.pathname.length))
    .filter((path) => !allowed.includes(path) && /#c2410c|--logo-tile/i.test(readFileSync(new URL(path, root), 'utf8')));
  expect(offenders).toEqual([]);
});

// Priority (design.md, Brand guideline): nothing is filled at rest. Urgent is a red arrow and red text and
// outranks High, an arrow and grey text; neither is a chip. The warning orange never marks a priority.
test('priority is an arrow and a word, Urgent red, nothing filled', () => {
  const css = read('styles/components.css');
  const high = rule(css, '.priority-high');
  const urgent = rule(css, '.priority-urgent');
  for (const r of [high, urgent]) {
    expect(r).not.toMatch(/var\(--warn(?:-soft)?\)/);
    expect(r).not.toMatch(/background:\s*var\(/);
  }
  expect(urgent).toMatch(/color:\s*var\(--danger\)/);
  expect(high).toMatch(/color:\s*var\(--text-2\)/);
});

// A plain notice is a neutral card: amber belongs to notes, focus and pending only.
test('a plain notice is neutral, not note amber', () => {
  expect(rule(read('ui/controls.css'), '.notice')).not.toMatch(amber);
  expect(rule(read('features/results/results.css'), '.results-notice')).not.toMatch(amber);
});

// Rule 4: a box sits on a ring or a surface step, never a border and a ring at once.
test('a panel sits on its ring, without a border', () => {
  expect(rule(read('styles/layout.css'), '.panel')).not.toMatch(/border:\s*1px/);
});

// Serif is for page titles: h1s, the sign-in promise and the KB title field. Empty states and sections are sans.
test('IBM Plex Serif sets page titles only', () => {
  const files = ['styles/components.css', 'styles/layout.css', 'features/results/results.css', 'features/kb/kb.css', 'features/settings/ai-settings.css', 'features/autofix/autofix.css'];
  const offenders = files.flatMap((file) => [...read(file).matchAll(/([^{}]+)\{[^}]*var\(--font-display\)[^}]*\}/g)]
    .map((m) => m[1].trim()).filter((selector) => /\b(?:h2|h3|h4)\b/.test(selector)).map((selector) => `${file}: ${selector}`));
  expect(offenders).toEqual([]);
});

// Motion comes from the tokens, so prefers-reduced-motion (which zeroes them) stops it.
test('transitions use the motion tokens', () => {
  const files = ['styles/components.css', 'styles/layout.css', 'styles/base-plain.css', 'ui/controls.css'];
  const raw = files.flatMap((file) => [...read(file).matchAll(/transition:[^;}]*\b\d+m?s\b[^;}]*/g)].map((m) => `${file}: ${m[0]}`));
  expect(raw).toEqual([]);
});

// Icons draw the stroke the tokens declare.
test('icons read --icon-stroke', () => {
  expect(read('styles/base-plain.css')).toMatch(/\.icon\s*\{[^}]*stroke-width:\s*var\(--icon-stroke\)/);
  expect(readFileSync(new URL('../src/ui/icons.tsx', import.meta.url), 'utf8')).toMatch(/className=\{?["`']?icon/);
});

test('the help center palette is ink and paper', () => {
  const css = readFileSync(new URL('../../server/internal/helpcenter/static/site.css', import.meta.url), 'utf8');
  const tinted = [...css.matchAll(/--(paper|surface|ink|muted|rule|accent|accent-ink|accent-soft|tab):\s*#([0-9a-f]{6})/gi)]
    .filter(([, , hex]) => !(hex.slice(0, 2) === hex.slice(2, 4) && hex.slice(2, 4) === hex.slice(4, 6)))
    .map(([, name, hex]) => `--${name} #${hex}`);
  expect(tinted).toEqual([]);
});

test('an open menu sits above the setup guide in the corner', () => {
  const layer = (file, selector) => Number(rule(read(file), selector).match(/z-index:\s*(\d+)/)?.[1]);
  const dock = layer('features/onboarding/setup-checklist.css', '.setup-dock');
  for (const menu of ['.filter-popover', '.action-popover']) expect(layer('styles/components.css', menu), menu).toBeGreaterThan(dock);
});

test('the setup guide marks a finished step with a stroke check, not a filled disc', () => {
  const done = rule(read('features/onboarding/setup-checklist.css'), '.setup-step[data-state="done"] .setup-mark');
  expect(done).not.toMatch(/background/);
  expect(done).toMatch(/color:\s*var\(--ok\)/);
  // The next step's row is a link styled as a primary action; its second line keeps the regular weight.
  expect(rule(read('features/onboarding/setup-checklist.css'), '.setup-step > a.primary-action .setup-state')).toMatch(/font-weight:\s*400/);
});
