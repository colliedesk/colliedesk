import { act, fireEvent, screen, within } from '@testing-library/react';
import type { Editor } from '@tiptap/core';
import { CellSelection } from '@tiptap/pm/tables';
import { TextSelection } from '@tiptap/pm/state';
import { expect, test } from 'vitest';
import { make } from './visual-editor.test.fixture';

const plain = '| A | B |\n| - | - |\n| 1 | 2 |\n';

const pick = (menu: string, item: string) => {
  fireEvent.click(screen.getByRole('button', { name: menu }));
  fireEvent.click(within(screen.getByRole('menu', { name: menu })).getByRole('menuitem', { name: item }));
};

// cells is where each cell of the article's tables starts, row by row.
function cells(editor: Editor) {
  const at: number[] = [];
  editor.state.doc.descendants((node, pos) => {
    if (node.type.name === 'tableHeader' || node.type.name === 'tableCell') at.push(pos);
  });
  return at;
}
const selectCells = (editor: Editor, from: number, to: number) =>
  act(() => { editor.view.dispatch(editor.state.tr.setSelection(CellSelection.create(editor.state.doc, cells(editor)[from]!, cells(editor)[to]!))); });
// into puts the cursor in the text of a cell.
const into = (editor: Editor, cell: number) =>
  act(() => { editor.view.dispatch(editor.state.tr.setSelection(TextSelection.near(editor.state.doc.resolve(cells(editor)[cell]! + 2)))); });
const firstRow = (editor: Editor) => editor.state.doc.firstChild!.firstChild!.childCount;

test('Table tools show only in a table, in place of the button that inserts one', async () => {
  const { editor } = await make(`Intro\n\n${plain}`);
  act(() => { editor.commands.setTextSelection(2); });
  expect(screen.queryByRole('button', { name: 'Table tools' })).not.toBeInTheDocument();
  into(editor, 2);
  expect(screen.getByRole('button', { name: 'Table tools' })).toBeEnabled();
});

test('merged cells are saved as the HTML table the server keeps, and a split takes the merge away', async () => {
  const { handle, editor } = await make(plain);
  selectCells(editor, 0, 1);
  pick('Table tools', 'Merge cells');
  expect(handle.markdown()).toBe('<table>\n<tr>\n<th colspan="2">\n\nA\n\nB\n\n</th>\n</tr>\n<tr>\n<td>\n\n1\n\n</td>\n<td>\n\n2\n\n</td>\n</tr>\n</table>\n');
  expect(firstRow(editor)).toBe(1);
  into(editor, 0);
  pick('Table tools', 'Split cell');
  expect(firstRow(editor)).toBe(2);
  expect(handle.markdown()).not.toContain('colspan');
});

test('Merge cells needs more than one cell selected, Split cell a merged one', async () => {
  const { editor } = await make(plain);
  into(editor, 0);
  fireEvent.click(screen.getByRole('button', { name: 'Table tools' }));
  const menu = within(screen.getByRole('menu', { name: 'Table tools' }));
  expect(menu.getByRole('menuitem', { name: 'Merge cells' })).toBeDisabled();
  expect(menu.getByRole('menuitem', { name: 'Split cell' })).toBeDisabled();
});

test('rows and columns are added and deleted where the cursor is', async () => {
  const { handle, editor } = await make(plain);
  into(editor, 2);
  pick('Table tools', 'Add row below');
  expect(editor.state.doc.firstChild!.childCount).toBe(3);
  expect(handle.markdown()).toBe('| A | B |\n| - | - |\n| 1 | 2 |\n| | |\n');
  // The cursor stays in its row; in the new one (cells 4 and 5), Delete row takes that one away.
  into(editor, 4);
  pick('Table tools', 'Delete row');
  expect(handle.markdown()).toBe(plain);
  into(editor, 1);
  pick('Table tools', 'Add column right');
  expect(firstRow(editor)).toBe(3);
  into(editor, 2);
  pick('Table tools', 'Delete column');
  expect(handle.markdown()).toBe(plain);
  into(editor, 1);
  pick('Table tools', 'Delete column');
  expect(handle.markdown()).toBe('| A |\n| - |\n| 1 |\n');
});

test('the header row can be turned off', async () => {
  const { handle, editor } = await make(plain);
  into(editor, 0);
  pick('Table tools', 'Header row off');
  expect(handle.markdown()).toBe('<table>\n<tr>\n<td>\n\nA\n\n</td>\n<td>\n\nB\n\n</td>\n</tr>\n<tr>\n<td>\n\n1\n\n</td>\n<td>\n\n2\n\n</td>\n</tr>\n</table>\n');
  pick('Table tools', 'Header row on');
  expect(handle.markdown()).toBe(plain);
});

test('a cell takes a background from the palette', async () => {
  const { handle, editor } = await make(plain);
  into(editor, 2);
  pick('Table tools', 'Green background');
  expect(handle.markdown()).toBe('<table>\n<tr>\n<th>\n\nA\n\n</th>\n<th>\n\nB\n\n</th>\n</tr>\n<tr>\n<td data-bg="success">\n\n1\n\n</td>\n<td>\n\n2\n\n</td>\n</tr>\n</table>\n');
  pick('Table tools', 'No background');
  expect(handle.markdown()).toBe(plain);
});

test('alignment in a table aligns the selected cells; a whole column stays a Markdown table', async () => {
  const { handle, editor } = await make(plain);
  selectCells(editor, 1, 3);
  pick('Align and indent', 'Align right');
  expect(handle.markdown()).toBe('| A | B |\n| - | -: |\n| 1 | 2 |\n');
  into(editor, 0);
  fireEvent.click(screen.getByRole('button', { name: 'Align and indent' }));
  expect(within(screen.getByRole('menu', { name: 'Align and indent' })).getByRole('menuitem', { name: 'Indent' })).toBeDisabled();
});

test('Delete table takes the whole table away', async () => {
  const { handle, editor } = await make(`Intro\n\n${plain}`);
  into(editor, 0);
  pick('Table tools', 'Delete table');
  expect(handle.markdown()).toBe('Intro\n');
});
