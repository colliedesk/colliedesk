// @vitest-environment node
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { expect, test } from 'vitest';
import { checkCeiling, readCeiling, scanQuietCopy, sourcesUnder } from './quiet-copy.mjs';

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

test('errors, alerts, warnings, lines marked as kept and short lines are left alone', () => {
  const found = scan(`import { m } from './m-messages';
    export const A = ({ failed }: { failed: boolean }) => <>
      <p className="error-copy field-note">{m.error}</p>
      <p role="alert" className="help">{m.error}</p>
      <p role={'alert'} className="help">{m.error}</p>
      <p className="warning-note">{m.long}</p>
      <p className="muted-copy" data-quiet="state">{m.long}</p>
      <p className="muted-copy" data-quiet="consequence">{m.long}</p>
      <p className="muted-copy" data-quiet="value">{m.long}</p>
      <p className={failed ? 'error-copy' : 'error-copy field-note'}>{m.error}</p>
      <p className="help">{m.short}</p>
    </>;`);
  expect(found).toEqual([]);
});

test('a muted-copy line counts anywhere, unless it is marked as a line the rule keeps visible', () => {
  const found = scan(`import { m } from './m-messages';
    export const A = () => <>
      <small className="muted-copy">{m.nested.why}</small>
      <p className="muted-copy" data-quiet="later">{m.long}</p>
    </>;`);
  expect(found.map((item) => item.key)).toEqual(['m-messages.ts:m.nested.why', 'm-messages.ts:m.long']);
});

test('each branch of a conditional className counts on its own', () => {
  const found = scan(`import { m } from './m-messages';
    export const A = ({ failed }: { failed: boolean }) => <>
      <p className={failed ? 'error-copy' : 'help'}>{m.long}</p>
      <p className={\`row \${failed ? 'error-copy' : 'field-note'}\`}>{m.nested.why}</p>
      <p className={failed && 'error-copy' || 'kb-hint'} role={failed ? 'alert' : undefined}>{m.error}</p>
    </>;`);
  expect(found.map((item) => item.key)).toEqual(['m-messages.ts:m.long', 'm-messages.ts:m.nested.why', 'm-messages.ts:m.error']);
});

test('a destructured parameter hides a constant of the same name, and its default counts', () => {
  const found = scan(`import { m } from './m-messages';
    const why = m.long;
    const note = m.nested.why;
    export const A = ({ why }: { why: string }) => <p className="help">{why}</p>;
    export function B({ note = m.error }: { note?: string }) { const { why } = props; return <><p className="help">{note}</p><p className="help">{why}</p></>; }`);
  expect(found.map((item) => item.key)).toEqual(['m-messages.ts:m.error']);
});

test('a component prop named description, note, detail or summary is helper copy', () => {
  const found = scan(`import { m } from './m-messages';
    export const A = () => <>
      <Trash description={m.long} />
      <Actions note={<>{m.nested.why}</>} />
      <Card detail={m.error} />
      <Step summary="Literal words that explain far too much for a single short line." />
      <Card detail={m.short} />
    </>;`);
  expect(found.map((item) => item.key)).toEqual(['m-messages.ts:m.long', 'm-messages.ts:m.nested.why', 'm-messages.ts:m.error', 'text:b33d20d16c4f']);
});

test('the ceiling list keeps each line at its recorded length: a new line, a longer one, a shorter one and a gone one fail', () => {
  const ceiling = readCeiling('# comment\na.ts:m.grew\t70\na.ts:m.same\t80\na.ts:m.shrank\t90\na.ts:m.gone\t75\n');
  expect([...ceiling]).toEqual([['a.ts:m.grew', 70], ['a.ts:m.same', 80], ['a.ts:m.shrank', 90], ['a.ts:m.gone', 75]]);
  const found = [
    { key: 'a.ts:m.grew', length: 71, text: 'grew', where: 'p.tsx:1' },
    { key: 'a.ts:m.same', length: 80, text: 'same', where: 'p.tsx:2' },
    { key: 'a.ts:m.shrank', length: 65, text: 'shrank', where: 'p.tsx:3' },
    { key: 'a.ts:m.new', length: 61, text: 'new', where: 'p.tsx:4' },
  ];
  expect(checkCeiling(found, ceiling)).toEqual([
    'a.ts:m.grew is 71 characters (p.tsx:1), over its 70 on the list: grew',
    'a.ts:m.shrank is 65 characters now (p.tsx:3): write 65 for it on the list',
    'a.ts:m.new is new (p.tsx:4, 61 characters): new',
    'a.ts:m.gone is 60 characters or less now, or gone: delete it from the list',
  ]);
});

// The ceiling list only shrinks, so its size is pinned here too: adding a line to the file fails this test.
test('the ceiling list holds at most 81 lines', () => {
  expect(readCeiling(readFileSync(ceilingFile, 'utf8')).size).toBeLessThanOrEqual(81);
});

test('every helper line of the web app longer than 60 characters is on the ceiling list at its recorded length', () => {
  const ceiling = readCeiling(readFileSync(ceilingFile, 'utf8'));
  expect(checkCeiling(scanQuietCopy(sourcesUnder(src), src), ceiling),
    `Move the how, why or definition into a HelpTip (src/ui/help-tip.tsx) and keep one line of 60 characters or less; update ${ceilingFile} only to lower or delete a line`).toEqual([]);
});
