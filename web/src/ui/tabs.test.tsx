import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { expect, test } from 'vitest';
import { Tabs } from './tabs';

function Three() {
  const [value, setValue] = useState<'a' | 'b' | 'c'>('b');
  return <Tabs label="Pages" prefix="t" value={value} onChange={setValue} tabs={[{ id: 'a', label: 'A' }, { id: 'b', label: 'B' }, { id: 'c', label: 'C' }]} />;
}

test('arrow keys move between tabs, Home and End go to the first and the last', () => {
  render(<Three />);
  fireEvent.keyDown(screen.getByRole('tab', { name: 'B' }), { key: 'ArrowRight' });
  expect(screen.getByRole('tab', { name: 'C' })).toHaveAttribute('aria-selected', 'true');
  expect(document.activeElement).toBe(screen.getByRole('tab', { name: 'C' }));
  fireEvent.keyDown(screen.getByRole('tab', { name: 'C' }), { key: 'Home' });
  expect(screen.getByRole('tab', { name: 'A' })).toHaveAttribute('aria-selected', 'true');
  expect(document.activeElement).toBe(screen.getByRole('tab', { name: 'A' }));
  fireEvent.keyDown(screen.getByRole('tab', { name: 'A' }), { key: 'End' });
  expect(screen.getByRole('tab', { name: 'C' })).toHaveAttribute('aria-selected', 'true');
});

test('a strip wider than its place fades at the edge that hides tabs', () => {
  const sizes = { scrollWidth: 300, clientWidth: 100 };
  const restore = (['scrollWidth', 'clientWidth'] as const).map((key) => {
    const before = Object.getOwnPropertyDescriptor(HTMLElement.prototype, key);
    Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => sizes[key] });
    return () => (before ? Object.defineProperty(HTMLElement.prototype, key, before) : undefined);
  });
  try {
    render(<Three />);
    const strip = screen.getByRole('tablist', { name: 'Pages' });
    expect(strip).toHaveClass('fade-end');
    strip.scrollLeft = 200;
    fireEvent.scroll(strip);
    expect(strip).toHaveClass('fade-start');
    expect(strip).not.toHaveClass('fade-end');
  } finally {
    restore.forEach((undo) => undo());
  }
});

test('the open tab is scrolled into sight by where it is in the strip, whatever the strip sits in', () => {
  const sizes = { scrollWidth: 400, clientWidth: 100 };
  const restore = (['scrollWidth', 'clientWidth'] as const).map((key) => {
    const before = Object.getOwnPropertyDescriptor(HTMLElement.prototype, key);
    Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => sizes[key] });
    return () => (before ? Object.defineProperty(HTMLElement.prototype, key, before) : undefined);
  });
  const rect = HTMLElement.prototype.getBoundingClientRect;
  // The strip starts 16 px into the page; tab C is 250 px into the strip, 50 px wide.
  HTMLElement.prototype.getBoundingClientRect = function box(this: HTMLElement) {
    const left = this.getAttribute('role') === 'tablist' ? 16 : this.textContent === 'C' ? 266 : 16;
    return { left, right: left + (this.getAttribute('role') === 'tablist' ? 100 : 50), width: this.getAttribute('role') === 'tablist' ? 100 : 50, top: 0, bottom: 30, height: 30, x: left, y: 0, toJSON: () => ({}) };
  };
  try {
    render(<Tabs label="Pages" prefix="t" value="c" onChange={() => undefined} tabs={[{ id: 'a', label: 'A' }, { id: 'b', label: 'B' }, { id: 'c', label: 'C' }]} />);
    expect(screen.getByRole('tablist', { name: 'Pages' }).scrollLeft).toBe(225);
  } finally {
    HTMLElement.prototype.getBoundingClientRect = rect;
    restore.forEach((undo) => undo());
  }
});

test('the fade is measured again when a tab changes width, not only the strip', () => {
  const observed: Element[] = [];
  const before = globalThis.ResizeObserver;
  globalThis.ResizeObserver = class { observe(target: Element) { observed.push(target); } unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  try {
    render(<Three />);
    expect(observed).toContain(screen.getByRole('tablist', { name: 'Pages' }));
    for (const tab of screen.getAllByRole('tab')) expect(observed).toContain(tab);
  } finally {
    globalThis.ResizeObserver = before;
  }
});
