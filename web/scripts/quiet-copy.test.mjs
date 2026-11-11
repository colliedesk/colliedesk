// @vitest-environment node
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { expect, test } from 'vitest';
import { readCeiling, scanQuietCopy, sourcesUnder } from './quiet-copy.mjs';

// "Quiet by default" (docs/internals/design.md): a row shows its label and at most one short line of 60
// characters or less. How, why and definitions go into a HelpTip. This gate reads every always-visible helper
// line of the web app and fails on one longer than 60 characters that is not on the ceiling list.
const src = fileURLToPath(new URL('../src', import.meta.url));
const ceilingFile = fileURLToPath(new URL('./quiet-copy-ceiling.txt', import.meta.url));

const messages = `export const m = {
  short: 'Saved in this browser only.',
  long: 'The only active member with the Admin role keeps it. Give Admin to someone else first.',
  nested: { why: 'Replies use each email channel sender name, not the workspace name you set here.' },
  count: (n: number) => \`\${n} conversations wait for a reply; open them from the inbox to answer each one.\`,
  error: 'This address is already taken by another workspace; choose a different one please.',
};`;

const scan = (tsx) => scanQuietCopy(new Map([['/app/m-messages.ts', messages], ['/app/page.tsx', tsx]]), '/app');

test('a helper line longer than 60 characters is found and keyed by its message', () => {
  const found = scan(`import { m } from './m-messages';
    export const A = () => <div><p className="help">{m.long}</p><p className="help">{m.short}</p></div>;`);
  expect(found).toEqual([{ key: 'm-messages.ts:m.long', length: 86, text: 'The only active member with the Admin role keeps it. Give Admin to someone else first.', where: 'page.tsx:2' }]);
});

test('every kind of helper counts: classes, a section description, a settings row note and a hint prop', () => {
  const found = scan(`import { m as text } from './m-messages';
    const t = text.nested;
    export const A = () => <>
      <small className="field-note">{t.why}</small>
      <div className="section-heading"><h2>Team</h2><p>{text.count(3)}</p></div>
      <div className="settings-rows"><p className="muted-copy">Literal words that explain far too much for a single short line.</p></div>
      <NameForm hint={text.long} />
    </>;`);
  expect(found.map((item) => item.key)).toEqual(['m-messages.ts:m.nested.why', 'm-messages.ts:m.count', 'text:b33d20d16c4f', 'm-messages.ts:m.long']);
});

test('errors, alerts, warnings and short lines are left alone', () => {
  const found = scan(`import { m } from './m-messages';
    export const A = () => <>
      <p className="error-copy field-note">{m.error}</p>
      <p role="alert" className="help">{m.error}</p>
      <p className="warning-note">{m.long}</p>
      <p className="muted-copy">{m.long}</p>
      <p className="help">{m.short}</p>
    </>;`);
  expect(found).toEqual([]);
});

test('no helper line of the web app is longer than 60 characters unless it is on the ceiling list', () => {
  const ceiling = readCeiling(readFileSync(ceilingFile, 'utf8'));
  const added = scanQuietCopy(sourcesUnder(src), src).filter((item) => !ceiling.has(item.key));
  expect(added.map((item) => `${item.key} (${item.where}, ${item.length} characters): ${item.text}`),
    'Move the how, why or definition into a HelpTip (src/ui/help-tip.tsx) and keep one line of 60 characters or less').toEqual([]);
});

test('the ceiling list only shrinks: a line that is now short or gone leaves the list', () => {
  const ceiling = readCeiling(readFileSync(ceilingFile, 'utf8'));
  const current = new Set(scanQuietCopy(sourcesUnder(src), src).map((item) => item.key));
  expect([...ceiling].filter((key) => !current.has(key)), `Remove these lines from ${ceilingFile}`).toEqual([]);
});
