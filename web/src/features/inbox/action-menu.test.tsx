import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { ActionMenu } from './action-menu';

// A menu of choices that each apply at once (bulk bar, message moves, a status that tells the customer).
test('an icon menu is named by its label and shows it as a tip', () => {
  render(<ActionMenu label="Set status" icon={<span>i</span>} disabled={false} onApply={vi.fn()}>{() => null}</ActionMenu>);
  const summary = screen.getByLabelText('Set status', { selector: 'summary' });
  expect(summary).toHaveAttribute('data-tip', 'Set status');
});

test('a menu with its own summary is named by the labels it points at, with no tip, and says when it opens', () => {
  const onToggle = vi.fn();
  const onApply = vi.fn();
  render(<><span id="row-label">Status</span><ActionMenu label="Status" summary={<span id="row-value">Open</span>} labelledBy="row-label row-value" disabled={false} onApply={onApply} onToggle={onToggle}>
    {(apply) => <button type="button" onClick={() => apply('resolved')}>Resolved</button>}</ActionMenu></>);
  const summary = screen.getByText('Open').closest('summary') as HTMLElement;
  expect(summary).toHaveAccessibleName('Status Open');
  expect(summary).not.toHaveAttribute('aria-label');
  expect(summary).not.toHaveAttribute('data-tip');
  const details = summary.closest('details') as HTMLDetailsElement;
  details.open = true;
  fireEvent(details, new Event('toggle'));
  expect(onToggle).toHaveBeenLastCalledWith(true);
  fireEvent.click(screen.getByRole('button', { name: 'Resolved' }));
  expect(onApply).toHaveBeenCalledWith('resolved');
  expect(details.open).toBe(false);
});
