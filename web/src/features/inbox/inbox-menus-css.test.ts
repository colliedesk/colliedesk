import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const css = readFileSync(join(process.cwd(), 'src/styles/components.css'), 'utf8');

// jsdom does no layout, so these read the rules whose absence broke the inbox menus and details in a real browser.
const rule = (selector: string) => css.split('\n').find((line) => line.startsWith(`${selector} {`));

describe('inbox menus and details', () => {
  it('keeps the Filter menu inside the screen, scrolling in itself with room for a scrollbar that shows', () => {
    const popover = rule('.filter-menu > .filter-popover');
    // Below the list header the screen has less than its full height: the menu stops before the bottom edge.
    expect(popover).toMatch(/max-height: min\([^;]*100dvh/);
    expect(popover).toContain('overflow-y: auto');
    expect(popover).toContain('scrollbar-gutter: stable');
  });

  it('shows the app checkbox itself on each Filter option, not a second box drawn by the row', () => {
    // The checkbox in base-plain.css is the one box of the app: the menu neither hides it nor draws its own.
    expect(css).not.toContain('input[type="checkbox"])::before');
    expect(css).not.toMatch(/\.filter-popover \.check-row > input:is\(\[type="checkbox"\]/);
    // A radio row still ends with a tick and hides its dot behind the whole row.
    expect(rule('.filter-popover .check-row > input[type="radio"]')).toContain('opacity: 0');
  });

  it('lets a details value wrap instead of cutting it with "…" while the column has room for it', () => {
    // Tags and Due: their words wrap onto a second line.
    const value = rule('.property-menu summary > span');
    expect(value).toContain('white-space: normal');
    expect(value).not.toContain('text-overflow');
    // Priority and Assignee are selects, which cannot wrap: no room is kept for an arrow that shows only on hover.
    const select = rule('.property-row select');
    expect(select).toContain('padding-right: var(--space-2)');
    expect(rule('.property-row select:hover:not(:disabled)')).toContain('padding-right: var(--space-6)');
    // The labels are short words: their column leaves the values the rest of the row.
    expect(rule('.details-panel .property-row')).toContain('--label: 64px');
  });
});
