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

  it('draws an empty box on each Filter option, and the tick in it once ticked, so the rows read as choices', () => {
    const box = rule('.filter-popover .check-row:has(> input[type="checkbox"])::before');
    expect(box).toContain('border: 1.5px solid');
    const ticked = rule('.filter-popover .check-row:has(> input[type="checkbox"]:checked)::before');
    expect(ticked).toContain('background-color: var(--accent)');
    expect(ticked).toContain('url(');
  });
});
