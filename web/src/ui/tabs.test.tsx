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
