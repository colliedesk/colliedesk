import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const css = readFileSync(join(process.cwd(), 'src/features/autofix/autofix.css'), 'utf8');

// jsdom does no layout, so these read the rules whose absence broke the screens in a real browser.
const rule = (selector: string) => css.split('\n').find((line) => line.startsWith(selector));

describe('autofix.css', () => {
  it('keeps the ✓ and ✕ marks of the GitHub dialog in the first column, over the rule that puts every span in the second', () => {
    expect(rule('.af-cardopt > .af-ok-mark, .af-cardopt > .af-no-mark')).toContain('grid-column: 1');
    expect(rule('.af-cardopt > .af-ok-mark {')).toContain('color: var(--ok)');
  });

  it('draws the arrow icon of a link inline, not as a block on a line of its own', () => {
    expect(rule('.af-kv svg')).toContain('display: inline-block');
  });

  it('lets a track label wrap instead of cutting it off', () => {
    const label = rule('.af-lbl');
    expect(label).not.toContain('text-overflow');
    expect(label).not.toContain('nowrap');
  });

  it('keeps the subject cell a table cell, so the row border and the state stripe reach it', () => {
    expect(rule('.af-c-subj {')).toBeUndefined();
    expect(rule('.af-subj')).toContain('display: grid');
  });

  it('gives the three Fixes tables the same columns', () => {
    expect(rule('.af-table { table-layout: fixed; }')).toBeDefined();
  });

  it('gives the buttons, links and menus of the Autofix cards and dialogs a 44px tap on a phone', () => {
    const phone = css.slice(css.indexOf('@media (max-width: 44rem)'));
    for (const selector of ['.af-dialog :is(button, select)', '.af-stepcard :is(button, .af-btn, select)', '.af-kv dd > a', '.af-evts-head .link-button', '.af-bar-row select']) {
      expect(phone, selector).toContain(selector);
    }
  });
});
