import { act, fireEvent, screen, within } from '@testing-library/react';
import { expect, test } from 'vitest';
import { make } from './visual-editor.test.fixture';

// pick opens a toolbar menu and chooses one of its items.
const pick = (menu: string, item: string) => {
  fireEvent.click(screen.getByRole('button', { name: menu }));
  fireEvent.click(within(screen.getByRole('menu', { name: menu })).getByRole('menuitem', { name: item }));
};

test('Underline and Highlight mark the selected words as the server stores them', async () => {
  const { handle, editor } = await make('Some words here\n');
  act(() => { editor.commands.setTextSelection({ from: 1, to: 5 }); });
  fireEvent.click(screen.getByRole('button', { name: /^Underline/ }));
  expect(handle.markdown()).toBe('<u>Some</u> words here\n');
  expect(screen.getByRole('button', { name: /^Underline/ })).toHaveAttribute('aria-pressed', 'true');
  act(() => { editor.commands.setTextSelection({ from: 12, to: 16 }); });
  fireEvent.click(screen.getByRole('button', { name: /^Highlight/ }));
  expect(handle.markdown()).toBe('<u>Some</u> words <mark>here</mark>\n');
});

test('a colour from the palette goes on the selected words, and Default colour takes it off', async () => {
  const { handle, editor } = await make('Some words here\n');
  act(() => { editor.commands.setTextSelection({ from: 6, to: 11 }); });
  pick('Text colour', 'Red');
  expect(handle.markdown()).toBe('Some <span data-color="danger">words</span> here\n');
  act(() => { editor.commands.setTextSelection({ from: 6, to: 11 }); });
  pick('Text colour', 'Default colour');
  expect(handle.markdown()).toBe('Some words here\n');
});

test('a paragraph is aligned and indented in the wrapper the server keeps, and back to plain', async () => {
  const { handle, editor } = await make('Some words here\n');
  act(() => { editor.commands.setTextSelection(3); });
  pick('Align and indent', 'Align centre');
  expect(handle.markdown()).toBe('<p data-align="center">\n\nSome words here\n\n</p>\n');
  pick('Align and indent', 'Indent');
  pick('Align and indent', 'Indent');
  expect(handle.markdown()).toBe('<p data-align="center" data-indent="2">\n\nSome words here\n\n</p>\n');
  pick('Align and indent', 'Align left');
  pick('Align and indent', 'Outdent');
  expect(handle.markdown()).toBe('<p data-indent="1">\n\nSome words here\n\n</p>\n');
  pick('Align and indent', 'Outdent');
  expect(handle.markdown()).toBe('Some words here\n');
});

test('a heading is aligned in its own wrapper', async () => {
  const { handle, editor } = await make('## Plans\n');
  act(() => { editor.commands.setTextSelection(3); });
  pick('Align and indent', 'Align right');
  expect(handle.markdown()).toBe('<h2 data-align="right">\n\n## Plans\n\n</h2>\n');
});

test('alignment and indent are for top-level text: in a list or a quote they are off', async () => {
  const { editor } = await make('- One\n\n> Quoted\n');
  act(() => { editor.commands.setTextSelection(3); });
  expect(screen.getByRole('button', { name: 'Align and indent' })).toBeDisabled();
  act(() => { editor.commands.setTextSelection(editor.state.doc.content.size - 3); });
  expect(screen.getByRole('button', { name: 'Align and indent' })).toBeDisabled();
});

test('the block list offers Heading 1 to Heading 4', async () => {
  const { handle, editor } = await make('Some words here\n');
  act(() => { editor.commands.setTextSelection(3); });
  const block = screen.getByRole('combobox');
  expect([...block.querySelectorAll('option')].map((option) => option.textContent)).toEqual(['Text', 'Heading 1', 'Heading 2', 'Heading 3', 'Heading 4']);
  fireEvent.change(block, { target: { value: 'h4' } });
  expect(handle.markdown()).toBe('#### Some words here\n');
  expect(block).toHaveValue('h4');
  fireEvent.change(block, { target: { value: 'h1' } });
  expect(handle.markdown()).toBe('# Some words here\n');
});
