import { act, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { expect, test, vi } from 'vitest';
import type { TokenPermission } from '../../api/schemas';
import { PermissionMatrix } from './permission-matrix';
import { allPermissions } from './token-permissions';

function Matrix({ start = [] as TokenPermission[] }) {
  const [picked, setPicked] = useState<TokenPermission[]>(start);
  return <PermissionMatrix idPrefix="t" picked={picked} allowed={new Set(allPermissions.map((item) => item.name))}
    onToggle={(name) => setPicked((now) => (now.includes(name) ? now.filter((item) => item !== name) : [...now, name]))}
    onSetMany={(names, on) => setPicked((now) => (on ? [...new Set([...now, ...names])] : now.filter((item) => !names.includes(item))))} />;
}

// The count read out after the person stops ticking is the count at that moment, not one from before.
test('the count announced after a run of ticks is the final count', () => {
  vi.useFakeTimers();
  render(<Matrix />);
  const live = document.querySelector('[aria-live="polite"]') as HTMLElement;
  fireEvent.click(screen.getByRole('checkbox', { name: 'Tags: View' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Tags: Create' }));
  act(() => { vi.advanceTimersByTime(1100); });
  expect(live).toHaveTextContent(`2 of ${allPermissions.length} selected`);
  vi.useRealTimers();
});

// A ticked chip is told apart by a mark, not only by weight.
test('a ticked chip shows a check mark', () => {
  render(<Matrix start={['tags:read']} />);
  const read = screen.getByRole('checkbox', { name: 'Tags: View' }).closest('label') as HTMLElement;
  const create = screen.getByRole('checkbox', { name: 'Tags: Create' }).closest('label') as HTMLElement;
  expect(read.querySelector('.permission-check')).not.toBeNull();
  expect(create.querySelector('.permission-check')).toBeNull();
});
