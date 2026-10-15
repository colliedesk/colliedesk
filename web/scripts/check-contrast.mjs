// WCAG 2.x contrast of every token pair the app and colliedesk.com use, for both themes.
// Prints a Markdown table per theme; exits 1 when a pair is below its minimum.
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const channel = (value) => (value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4);
function luminance(hex) {
  const [r, g, b] = hex.slice(1).match(/../g).map((pair) => channel(parseInt(pair, 16) / 255));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
export function contrast(a, b) {
  const [high, low] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (high + 0.05) / (low + 0.05);
}

// themeTokens reads the hex tokens of one theme block: `:root, :root[data-theme="light"]` or `:root[data-theme="dark"]`.
export function themeTokens(css, theme) {
  const block = css.match(new RegExp(`:root\\[data-theme="${theme}"\\]\\s*\\{([^}]*)\\}`));
  if (!block) throw new Error(`tokens.css has no ${theme} block`);
  const tokens = Object.fromEntries([...block[1].matchAll(/(--[\w-]+):\s*(#[0-9a-f]{6})/gi)].map((m) => [m[1], m[2].toLowerCase()]));
  // var() aliases inside the block (--open: var(--accent)).
  for (const [, name, target] of block[1].matchAll(/(--[\w-]+):\s*var\((--[\w-]+)\)/g)) if (tokens[target]) tokens[name] = tokens[target];
  return tokens;
}

const surfaces = ['--surface-0', '--surface-1', '--surface-2', '--surface-3'];
export function pairs() {
  const rows = [];
  const add = (fg, bg, min, use) => rows.push({ fg, bg, min, use });
  for (const fg of ['--text-1', '--text-2', '--text-3', '--accent-text', '--danger', '--warn', '--ok']) for (const bg of surfaces) add(fg, bg, 4.5, 'text');
  add('--accent-text', '--accent-soft', 4.5, 'link on selected row');
  add('--text-1', '--accent-soft', 4.5, 'selected row');
  add('--text-3', '--accent-soft', 4.5, 'meta on selected row');
  add('--on-accent', '--accent', 4.5, 'primary button');
  add('--on-accent', '--accent-hover', 4.5, 'primary button hover');
  add('--text-inverse', '--surface-inverse', 4.5, 'tooltip');
  add('--note-text', '--note-bg', 4.5, 'private note label');
  add('--text-1', '--note-bg', 4.5, 'private note body');
  add('--danger', '--danger-soft', 4.5, 'urgent chip');
  add('--text-1', '--danger-soft', 4.5, 'error card body');
  add('--warn', '--warn-soft', 4.5, 'high chip');
  add('--ok', '--ok-soft', 4.5, 'sent badge, reconcile footer');
  add('--text-2', '--ok-soft', 4.5, 'reconcile footer body');
  add('--accent-text', '--ok-soft', 4.5, 'reconcile footer link');
  add('--on-brand-panel', '--brand-panel', 4.5, 'sign-in brand side');
  add('--on-brand-panel-2', '--brand-panel', 4.5, 'sign-in brand side, secondary');
  add('--pending', '--surface-1', 3, 'pending dot');
  add('--open', '--surface-1', 3, 'open dot');
  for (const bg of surfaces) add('--focus', bg, 3, 'focus ring');
  for (const bg of ['--surface-0', '--surface-1', '--surface-2']) add('--line-strong', bg, 3, 'input border');
  add('--accent', '--surface-1', 3, 'selected bar, active tab');
  return rows;
}

export function report(css) {
  return ['light', 'dark'].map((theme) => {
    const tokens = themeTokens(css, theme);
    const rows = pairs().map((row) => {
      if (!tokens[row.fg] || !tokens[row.bg]) throw new Error(`${theme}: missing ${tokens[row.fg] ? row.bg : row.fg}`);
      return { ...row, ratio: contrast(tokens[row.fg], tokens[row.bg]), fgHex: tokens[row.fg], bgHex: tokens[row.bg] };
    });
    return { theme, rows, failures: rows.filter((row) => row.ratio < row.min) };
  });
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const css = readFileSync(new URL('../src/styles/tokens.css', import.meta.url), 'utf8');
  let failed = 0;
  for (const { theme, rows, failures } of report(css)) {
    console.log(`\n### ${theme}\n\n| Foreground | Background | Ratio | Min | Use |\n|---|---|---|---|---|`);
    for (const row of rows) console.log(`| \`${row.fg}\` ${row.fgHex} | \`${row.bg}\` ${row.bgHex} | ${row.ratio.toFixed(2)}${row.ratio < row.min ? ' FAIL' : ''} | ${row.min} | ${row.use} |`);
    failed += failures.length;
  }
  if (failed) { console.error(`${failed} pair(s) below minimum`); process.exit(1); }
}
