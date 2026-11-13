import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { Sheet } from './sheet';

function open() {
  render(<Sheet title="Reply drafts" closeLabel="Close" onClose={() => undefined} footer={<button type="button">Done</button>}>
    <label>Model<select><option>a</option></select></label><button type="button">Change</button></Sheet>);
  return screen.getByRole('dialog', { name: 'Reply drafts' });
}

test('Tab from the sheet itself goes to its first control and Shift+Tab to its last, so focus never leaves it', () => {
  const sheet = open();
  expect(document.activeElement).toBe(sheet);
  fireEvent.keyDown(sheet, { key: 'Tab', shiftKey: true });
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Done' }));
  sheet.focus();
  fireEvent.keyDown(sheet, { key: 'Tab' });
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Close' }));
});

test('Tab on the last control wraps to the first, and Shift+Tab on the first to the last', () => {
  const sheet = open();
  screen.getByRole('button', { name: 'Done' }).focus();
  fireEvent.keyDown(screen.getByRole('button', { name: 'Done' }), { key: 'Tab' });
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Close' }));
  fireEvent.keyDown(screen.getByRole('button', { name: 'Close' }), { key: 'Tab', shiftKey: true });
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Done' }));
  expect(sheet).toContainElement(document.activeElement as HTMLElement);
});
