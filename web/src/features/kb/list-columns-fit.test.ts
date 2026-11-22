import { describe, expect, it } from 'vitest';
import type { KBListColumn } from '../../api/schemas';
import { fittingColumns } from './list-columns';

const four: KBListColumn[] = ['status', 'author', 'tags', 'updated'];
const all: KBListColumn[] = ['status', 'collection', 'author', 'tags', 'languages', 'created', 'updated', 'votes'];

// Worked by hand at 16 px to the rem: the row starts 16 px in (44 px with the tick box), the title keeps 192 px,
// the open button 40 (in a collection's order the open button and the order buttons share one 72 px track),
// 16 px between tracks; status 120, author 128, tags 144, updated 128,
// collection 144. The list at 1440 px is 756 px wide inside its scroll bar.
describe('fittingColumns', () => {
  it('keeps every chosen column when they fit', () => {
    // 44 + 192 + 520 + 40 + 5 gaps of 16 = 876.
    expect(fittingColumns(four, { width: 876, ordered: false, selects: true, rem: 16 })).toBe(4);
  });

  it('leaves out the last columns that would squeeze the title below its width', () => {
    // 876 > 756; three columns need 44 + 192 + 392 + 40 + 64 = 732.
    expect(fittingColumns(four, { width: 756, ordered: false, selects: true, rem: 16 })).toBe(3);
    // Without the tick box 4 columns need 848, three 704.
    expect(fittingColumns(four, { width: 756, ordered: false, selects: false, rem: 16 })).toBe(3);
    expect(fittingColumns(four, { width: 848, ordered: false, selects: false, rem: 16 })).toBe(4);
  });

  it('counts the order buttons of a collection\'s order', () => {
    // Three columns: 44 + 192 + 392 + 72 + 64 = 764 > 756; two: 44 + 192 + 248 + 72 + 48 = 604.
    expect(fittingColumns(four, { width: 756, ordered: true, selects: true, rem: 16 })).toBe(2);
    expect(fittingColumns(four, { width: 764, ordered: true, selects: true, rem: 16 })).toBe(3);
  });

  it('fits the default columns in a collection\'s order at 1440 px', () => {
    // Status, author, updated: 44 + 192 + 376 + 72 + 64 = 748 <= 756.
    expect(fittingColumns(['status', 'author', 'updated'], { width: 756, ordered: true, selects: true, rem: 16 })).toBe(3);
  });

  it('follows the order the person chose', () => {
    // status, collection, author: 44 + 192 + 392 + 40 + 64 = 732; adding tags makes 892.
    expect(fittingColumns(all, { width: 756, ordered: false, selects: true, rem: 16 })).toBe(3);
  });

  it('keeps the first column on a narrow list, which shows the title and that column only', () => {
    expect(fittingColumns(four, { width: 300, ordered: true, selects: true, rem: 16 })).toBe(1);
    expect(fittingColumns([], { width: 300, ordered: false, selects: false, rem: 16 })).toBe(0);
  });

  it('scales the rem tracks with the root font size, not the px padding and gaps', () => {
    // At 20 px to the rem: 44 + 240 + 650 + 50 + 5 gaps of 16 = 1064.
    expect(fittingColumns(four, { width: 1064, ordered: false, selects: true, rem: 20 })).toBe(4);
    expect(fittingColumns(four, { width: 1063, ordered: false, selects: true, rem: 20 })).toBe(3);
  });

  it('shows only the first column when the list is 36rem or narrower, as its container query does', () => {
    // 600 px inside the scroll bar: votes and languages fit (16 + 192 + 96 + 128 + 40 + 16 = 488), but a list of
    // 576 px (36rem) shows the title and the first column only.
    const narrow: KBListColumn[] = ['votes', 'languages', 'status'];
    expect(fittingColumns(narrow, { width: 600, list: 577, ordered: false, selects: false, rem: 16 })).toBe(2);
    expect(fittingColumns(narrow, { width: 600, list: 576, ordered: false, selects: false, rem: 16 })).toBe(1);
  });

  it('keeps every chosen column on a phone, whose rows show the status beside the title', () => {
    expect(fittingColumns(['author', 'status'], { width: 300, list: 300, phone: true, ordered: false, selects: false, rem: 16 })).toBe(2);
  });
});
