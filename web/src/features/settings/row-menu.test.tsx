import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { expect, test, vi } from 'vitest';
import { RowMenu } from './row-menu';

// The "⋯" menu of a row: a button that opens a list of actions, moved with the arrow keys.
const items = (onPick = vi.fn()) => [
  { key: 'roles', label: 'Change roles', onSelect: () => onPick('roles') },
  { key: 'rename', label: 'Rename', onSelect: () => onPick('rename') },
  { key: 'deactivate', label: 'Deactivate', danger: true, onSelect: () => onPick('deactivate') },
];

test('the button opens a menu named for its row, with the first action focused', () => {
  render(<RowMenu label="Ann" items={items()} />);
  const button = screen.getByRole('button', { name: 'Actions for Ann' });
  expect(button).toHaveAttribute('aria-expanded', 'false');
  expect(screen.queryByRole('menu')).toBeNull();
  fireEvent.click(button);
  expect(button).toHaveAttribute('aria-expanded', 'true');
  expect(screen.getByRole('menu', { name: 'Actions for Ann' })).toBeInTheDocument();
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['Change roles', 'Rename', 'Deactivate']);
  expect(screen.getByRole('menuitem', { name: 'Change roles' })).toHaveFocus();
});

test('the arrow keys move through the actions and wrap, Home and End jump', () => {
  render(<RowMenu label="Ann" items={items()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Actions for Ann' }));
  const menu = screen.getByRole('menu');
  fireEvent.keyDown(menu, { key: 'ArrowDown' });
  expect(screen.getByRole('menuitem', { name: 'Rename' })).toHaveFocus();
  fireEvent.keyDown(menu, { key: 'ArrowDown' });
  fireEvent.keyDown(menu, { key: 'ArrowDown' });
  expect(screen.getByRole('menuitem', { name: 'Change roles' })).toHaveFocus();
  fireEvent.keyDown(menu, { key: 'ArrowUp' });
  expect(screen.getByRole('menuitem', { name: 'Deactivate' })).toHaveFocus();
  fireEvent.keyDown(menu, { key: 'Home' });
  expect(screen.getByRole('menuitem', { name: 'Change roles' })).toHaveFocus();
  fireEvent.keyDown(menu, { key: 'End' });
  expect(screen.getByRole('menuitem', { name: 'Deactivate' })).toHaveFocus();
});

test('Escape closes the menu and puts the focus back on its button', () => {
  render(<RowMenu label="Ann" items={items()} />);
  const button = screen.getByRole('button', { name: 'Actions for Ann' });
  fireEvent.click(button);
  fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
  expect(screen.queryByRole('menu')).toBeNull();
  expect(button).toHaveFocus();
});

test('choosing an action closes the menu and runs it once', () => {
  const pick = vi.fn();
  render(<RowMenu label="Ann" items={items(pick)} />);
  fireEvent.click(screen.getByRole('button', { name: 'Actions for Ann' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }));
  expect(pick).toHaveBeenCalledTimes(1);
  expect(pick).toHaveBeenCalledWith('rename');
  expect(screen.queryByRole('menu')).toBeNull();
});

test('a press outside closes the menu', () => {
  render(<div><button type="button">elsewhere</button><RowMenu label="Ann" items={items()} /></div>);
  fireEvent.click(screen.getByRole('button', { name: 'Actions for Ann' }));
  fireEvent.pointerDown(screen.getByRole('button', { name: 'elsewhere' }));
  expect(screen.queryByRole('menu')).toBeNull();
});

// A menu that closes must not leave the focus on the page body: from there Esc leaves Settings.
test('choosing an action that opens nothing leaves the focus on the actions button', () => {
  render(<RowMenu label="Ann" items={items()} />);
  const button = screen.getByRole('button', { name: 'Actions for Ann' });
  fireEvent.click(button);
  fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }));
  expect(button).toHaveFocus();
});

test('when the action takes the menu away from its row, the focus goes to the row', () => {
  function Row() {
    const [ended, setEnded] = useState(false);
    return <article aria-label="Ann row">{ended ? 'Revoked' : <RowMenu label="Ann" items={[{ key: 'revoke', label: 'Revoke', onSelect: () => setEnded(true) }]} />}</article>;
  }
  render(<Row />);
  fireEvent.click(screen.getByRole('button', { name: 'Actions for Ann' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Revoke' }));
  expect(screen.getByRole('article', { name: 'Ann row' })).toHaveFocus();
});
