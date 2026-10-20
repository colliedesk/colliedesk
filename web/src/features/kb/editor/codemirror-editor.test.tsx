import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { EditorView } from '@codemirror/view';
import { expect, test, vi } from 'vitest';
import CodeMirrorEditor from './codemirror-editor';
import type { EditorHandle, UploadHooks } from './codemirror-editor';

// jsdom lays nothing out; CodeMirror measures text ranges, so they answer with empty boxes.
if (!('getClientRects' in Range.prototype)) Object.assign(Range.prototype, { getClientRects: () => [], getBoundingClientRect: () => new DOMRect() });

const never: UploadHooks = { accept: (files) => files, upload: () => new Promise(() => {}), onStart: () => {}, onDone: () => {}, onError: () => {} };

async function make(initial: string, uploads: UploadHooks = never) {
  let handle: EditorHandle | undefined;
  const onChange = vi.fn();
  render(<CodeMirrorEditor initial={initial} uploads={uploads} label="Text" describedBy="help" invalid={false} mode="edit" onChange={onChange}
    onReady={(ready) => { handle = ready; }} />);
  await waitFor(() => expect(handle).toBeDefined());
  return { handle: handle!, onChange, text: screen.getByRole('textbox', { name: 'Text' }) };
}

test('the text is kept byte for byte: what is loaded is what is saved', async () => {
  const article = 'Steps\n-----\n\nSee [the guide][g].\n\n|a|b|\n|-|-|\n|1|2|\n\n<b>raw</b>\r\n\n[g]: https://example.com\n[unused]: https://example.com\n';
  const { handle, onChange } = await make(article);
  // CodeMirror splits lines on \r\n, \r and \n alike and writes \n: the one byte change it makes, and it is
  // no change the page hears of (the page compares line endings out, article-editor.tsx sameBody).
  expect(handle.markdown()).toBe(article.replace('\r\n', '\n'));
  expect(onChange).not.toHaveBeenCalled();
  handle.load('# Other');
  expect(handle.markdown()).toBe('# Other');
});

test('a pasted image waits on screen, never in the text, and goes in after the block the cursor is in', async () => {
  let finish: (value: { url: string; alt: string }) => void = () => {};
  const done = vi.fn();
  const uploads: UploadHooks = { ...never, upload: () => new Promise((resolve) => { finish = resolve; }), onDone: done };
  const { handle, text } = await make('| Plan | Agents |\n| --- | --- |\n| Team | 5 |\n\nNext', uploads);
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'plans.png', { type: 'image/png' })] } });
  expect(await screen.findByText('Uploading plans.png…')).toBeInTheDocument();
  expect(handle.uploading()).toBe(true);
  expect(handle.markdown()).toBe('| Plan | Agents |\n| --- | --- |\n| Team | 5 |\n\nNext');
  await act(async () => { finish({ url: '/api/v1/kb/images/1', alt: 'plans' }); });
  expect(handle.markdown()).toBe('| Plan | Agents |\n| --- | --- |\n| Team | 5 |\n\n![plans](/api/v1/kb/images/1)\n\nNext');
  expect(handle.uploading()).toBe(false);
  expect(screen.queryByText('Uploading plans.png…')).not.toBeInTheDocument();
  expect(done).toHaveBeenCalledTimes(1);
});

test('a refused upload only takes its place away; an upload whose text was replaced still ends', async () => {
  let refuse: (error: Error) => void = () => {};
  let finish: (value: { url: string; alt: string }) => void = () => {};
  const error = vi.fn();
  const done = vi.fn();
  let calls = 0;
  const uploads: UploadHooks = { ...never, onError: error, onDone: done,
    upload: () => new Promise((resolve, reject) => { calls += 1; if (calls === 1) refuse = reject; else finish = resolve; }) };
  const { handle, text } = await make('Text', uploads);
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'a.png', { type: 'image/png' })] } });
  await act(async () => { refuse(new Error('Storage is full.')); });
  expect(error).toHaveBeenCalledTimes(1);
  expect(handle.markdown()).toBe('Text');
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'b.png', { type: 'image/png' })] } });
  handle.load('Theirs');
  await act(async () => { finish({ url: '/api/v1/kb/images/2', alt: 'b' }); });
  expect(handle.markdown()).toBe('Theirs');
  expect(done).toHaveBeenCalledTimes(1);
});

test('the toolbar is one Tab stop that arrows move, and it stays when the button that held it turns disabled', async () => {
  await make('Text');
  const toolbar = screen.getByRole('toolbar', { name: 'Formatting' });
  const enabled = () => within(toolbar).getAllByRole('button').filter((button) => !button.hasAttribute('disabled'));
  expect(enabled().filter((button) => button.tabIndex === 0)).toHaveLength(1);
  expect(enabled()[0]).toHaveAccessibleName(/^Bold \(/);
  fireEvent.click(within(toolbar).getByRole('button', { name: 'Divider' }));
  const undo = within(toolbar).getByRole('button', { name: /^Undo/ });
  await waitFor(() => expect(undo).toBeEnabled());
  within(toolbar).getByRole('button', { name: /^Bold/ }).focus();
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
  expect(document.activeElement).toHaveAccessibleName(/^Italic/);
  fireEvent.keyDown(document.activeElement!, { key: 'End' });
  expect(document.activeElement).toBe(undo);
  fireEvent.click(undo);
  await waitFor(() => expect(undo).toBeDisabled());
  expect(enabled().filter((button) => button.tabIndex === 0)).toHaveLength(1);
});

test('a list, checklist or heading put on the line of the cursor leaves the cursor after its marker', async () => {
  const { handle, text } = await make('');
  const toolbar = screen.getByRole('toolbar', { name: 'Formatting' });
  fireEvent.click(within(toolbar).getByRole('button', { name: 'Checklist' }));
  // CodeMirror reads typed text from the page; inserting at the selection stands in for a key press here.
  const view = EditorView.findFromDOM(text)!;
  view.dispatch(view.state.replaceSelection('Open Settings'));
  expect(handle.markdown()).toBe('- [ ] Open Settings');
  view.dispatch(view.state.update({ changes: { from: view.state.doc.length, insert: '\n' }, selection: { anchor: view.state.doc.length + 1 } }));
  fireEvent.change(within(toolbar).getByRole('combobox', { name: 'Block type' }), { target: { value: 'h2' } });
  view.dispatch(view.state.replaceSelection('Steps'));
  expect(handle.markdown()).toBe('- [ ] Open Settings\n## Steps');
});
