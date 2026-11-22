import { describe, expect, it } from 'vitest';
import type { KBListColumn } from '../../api/schemas';
import { fittingColumns } from './list-columns';

const defaults: KBListColumn[] = ['status', 'author', 'tags', 'updated'];
const all: KBListColumn[] = ['status', 'collection', 'author', 'tags', 'languages', 'created', 'updated', 'votes'];

// Worked by hand at 16 px to the rem: the row starts 16 px in (44 px with the tick box), the title keeps 192 px,
// the open button 40, the order buttons 72, 16 px between tracks; status 120, author 128, tags 144, updated 128,
// collection 144. The list at 1440 px is 756 px wide inside its scroll bar.
describe('fittingColumns', () => {
  it('keeps every chosen column when they fit', () => {
    // 44 + 192 + 520 + 40 + 5 gaps of 16 = 876.
    expect(fittingColumns(defaults, { width: 876, ordered: false, selects: true, rem: 16 })).toBe(4);
  });

  it('leaves out the last columns that would squeeze the title below its width', () => {
    // 876 > 756; three columns need 44 + 192 + 392 + 40 + 64 = 732.
    expect(fittingColumns(defaults, { width: 756, ordered: false, selects: true, rem: 16 })).toBe(3);
    // Without the tick box 4 columns need 848, three 704.
    expect(fittingColumns(defaults, { width: 756, ordered: false, selects: false, rem: 16 })).toBe(3);
    expect(fittingColumns(defaults, { width: 848, ordered: false, selects: false, rem: 16 })).toBe(4);
  });

  it('counts the order buttons of a collection\'s order', () => {
    // Three columns: 44 + 192 + 392 + 40 + 72 + 80 = 820 > 756; two: 44 + 192 + 248 + 40 + 72 + 64 = 660.
    expect(fittingColumns(defaults, { width: 756, ordered: true, selects: true, rem: 16 })).toBe(2);
    expect(fittingColumns(defaults, { width: 820, ordered: true, selects: true, rem: 16 })).toBe(3);
  });

  it('follows the order the person chose', () => {
    // status, collection, author: 44 + 192 + 392 + 40 + 64 = 732; adding tags makes 892.
    expect(fittingColumns(all, { width: 756, ordered: false, selects: true, rem: 16 })).toBe(3);
  });

  it('keeps the first column on a narrow list, which shows the title and that column only', () => {
    expect(fittingColumns(defaults, { width: 300, ordered: true, selects: true, rem: 16 })).toBe(1);
    expect(fittingColumns([], { width: 300, ordered: false, selects: false, rem: 16 })).toBe(0);
  });

  it('scales the rem tracks with the root font size, not the px padding and gaps', () => {
    // At 20 px to the rem: 44 + 240 + 650 + 50 + 5 gaps of 16 = 1064.
    expect(fittingColumns(defaults, { width: 1064, ordered: false, selects: true, rem: 20 })).toBe(4);
    expect(fittingColumns(defaults, { width: 1063, ordered: false, selects: true, rem: 20 })).toBe(3);
  });
});
