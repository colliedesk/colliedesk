import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { EditorView } from '@codemirror/view';
import { expect, test, vi } from 'vitest';
import CodeMirrorEditor from './codemirror-editor';
import type { EditorHandle, UploadHooks } from './codemirror-editor';

// The formatting the server keeps beyond Markdown, typed in the Markdown source: each tool writes the tag in the
// one spelling the server's normalizer keeps (server/internal/kbformat), so the source and the visual editor agree.

if (!('getClientRects' in Range.prototype)) Object.assign(Range.prototype, { getClientRects: () => [], getBoundingClientRect: () => new DOMRect() });

const never: UploadHooks = { accept: (files) => files, upload: () => new Promise(() => {}), store: () => new Promise(() => {}), onStart: () => {}, onDone: () => {}, onError: () => {} };

async function make(initial: string) {
  let handle: EditorHandle | undefined;
  render(<CodeMirrorEditor initial={initial} uploads={never} label="Text" describedBy="help" invalid={false} mode="edit" onChange={vi.fn()}
    onReady={(ready) => { handle = ready; }} />);
  await waitFor(() => expect(handle).toBeDefined());
  const text = screen.getByRole('textbox', { name: 'Text' });
  const view = EditorView.findFromDOM(text)!;
  const select = (anchor: number, head = anchor) => act(() => { view.dispatch({ selection: { anchor, head } }); });
  return { handle: handle!, text, select };
}

const pick = (menu: string, item: string) => {
  fireEvent.click(screen.getByRole('button', { name: menu }));
  fireEvent.click(within(screen.getByRole('menu', { name: menu })).getByRole('menuitem', { name: item }));
};

test('Underline and Highlight wrap the selected words in <u> and <mark>, and take them off again', async () => {
  const { handle, select } = await make('Some words here');
  select(0, 4);
  fireEvent.click(screen.getByRole('button', { name: /^Underline/ }));
  expect(handle.markdown()).toBe('<u>Some</u> words here');
  await waitFor(() => expect(screen.getByRole('button', { name: /^Underline/ })).toHaveAttribute('aria-pressed', 'true'));
  fireEvent.click(screen.getByRole('button', { name: /^Underline/ }));
  expect(handle.markdown()).toBe('Some words here');
  select(11, 15);
  fireEvent.click(screen.getByRole('button', { name: /^Highlight/ }));
  expect(handle.markdown()).toBe('Some words <mark>here</mark>');
});

test('Ctrl+U underlines the selection', async () => {
  const { handle, text, select } = await make('Some words here');
  select(5, 10);
  fireEvent.keyDown(text, { key: 'u', code: 'KeyU', ctrlKey: true });
  expect(handle.markdown()).toBe('Some <u>words</u> here');
});

test('a colour wraps the selection in a palette span; another colour or Default colour changes the same span', async () => {
  const { handle, select } = await make('Some words here');
  select(5, 10);
  pick('Text colour', 'Red');
  expect(handle.markdown()).toBe('Some <span data-color="danger">words</span> here');
  pick('Text colour', 'Green');
  expect(handle.markdown()).toBe('Some <span data-color="success">words</span> here');
  pick('Text colour', 'Default colour');
  expect(handle.markdown()).toBe('Some words here');
});

test('alignment and indent wrap the paragraph of the cursor in the <p> the server keeps, and unwrap it at the default', async () => {
  const { handle, select } = await make('Intro\n\nSome words here\n\nEnd');
  select(10);
  pick('Align and indent', 'Align centre');
  expect(handle.markdown()).toBe('Intro\n\n<p data-align="center">\n\nSome words here\n\n</p>\n\nEnd');
  pick('Align and indent', 'Indent');
  expect(handle.markdown()).toBe('Intro\n\n<p data-align="center" data-indent="1">\n\nSome words here\n\n</p>\n\nEnd');
  pick('Align and indent', 'Align left');
  expect(handle.markdown()).toBe('Intro\n\n<p data-indent="1">\n\nSome words here\n\n</p>\n\nEnd');
  pick('Align and indent', 'Outdent');
  expect(handle.markdown()).toBe('Intro\n\nSome words here\n\nEnd');
});

test('a heading is aligned in its own <hN> wrapper', async () => {
  const { handle, select } = await make('## Plans');
  select(4);
  pick('Align and indent', 'Align right');
  expect(handle.markdown()).toBe('<h2 data-align="right">\n\n## Plans\n\n</h2>');
});

test('in a list or a quote, Align and indent is off', async () => {
  const { select } = await make('- One\n\n> Quoted');
  select(3);
  await waitFor(() => expect(screen.getByRole('button', { name: 'Align and indent' })).toBeDisabled());
  select(10);
  await waitFor(() => expect(screen.getByRole('button', { name: 'Align and indent' })).toBeDisabled());
});

test('Heading 4 is offered in the source too', async () => {
  const { handle } = await make('Steps');
  fireEvent.change(screen.getByRole('combobox', { name: 'Block type' }), { target: { value: 'h4' } });
  expect(handle.markdown()).toBe('#### Steps');
  await waitFor(() => expect(screen.getByRole('combobox', { name: 'Block type' })).toHaveValue('h4'));
});
