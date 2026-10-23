// @vitest-environment node
import { readdirSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test } from 'vitest';

// "Sentence case everywhere. No uppercase labels." (docs/internals/design.md, Type). A label set in
// capitals by CSS reads as shouting and is spelled letter by letter by some screen readers, so no
// stylesheet of the web app may turn text into capitals. There is no exception today; one would need
// a reason in the design guideline and an entry here naming its selector.
const src = fileURLToPath(new URL('../src', import.meta.url));
const allowed = new Set([]);

function stylesheets(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return stylesheets(path);
    return entry.name.endsWith('.css') ? [path] : [];
  });
}

test('no stylesheet sets text in capitals', () => {
  const offenders = stylesheets(src).flatMap((file) => {
    const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
    return [...css.matchAll(/([^{}]+)\{[^}]*(?:text-transform:\s*(?:uppercase|capitalize)|font-variant(?:-caps)?:\s*[^;}]*(?:small-caps|all-small-caps|all-petite-caps))[^}]*\}/g)]
      .map((match) => `${relative(src, file)}: ${match[1].trim()}`)
      .filter((offender) => !allowed.has(offender));
  });
  expect(offenders).toEqual([]);
});
