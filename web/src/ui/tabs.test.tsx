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
