import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { UploadHooks } from '../editor-types';
import { schema } from './schema';
import { make, never } from './visual-editor.test.fixture';

test('an article is saved as it was written until the person changes it', async () => {
  const article = 'Steps\n=====\n\n__bold__ and *it*\n\n<b>raw</b>\n';
  const { handle, onChange, editor } = await make(article);
  expect(handle.markdown()).toBe(article);
  // Moving the cursor is no change.
  act(() => { editor.commands.setTextSelection(3); });
  expect(handle.markdown()).toBe(article);
  act(() => { editor.commands.insertContentAt(editor.state.doc.child(0).nodeSize - 1, '!'); });
  expect(handle.markdown()).toBe('# Steps!\n\n**bold** and *it*\n\n<b>raw</b>\n');
  await waitFor(() => expect(onChange).toHaveBeenCalledWith('# Steps!\n\n**bold** and *it*\n\n<b>raw</b>\n'));
  expect(onChange).toHaveBeenCalledTimes(1);
});

test('a Markdown block the editor cannot show stays read-only text in its place', async () => {
  await make('Before\n\n<details><summary>More</summary>Hidden</details>\n');
  expect(screen.getByText('<details><summary>More</summary>Hidden</details>')).toBeInTheDocument();
  expect(document.querySelector('.kb-markdown-block')).toHaveAttribute('contenteditable', 'false');
});

test('load shows other text with a fresh undo history, and is no change', async () => {
  const { handle, onChange, editor } = await make('One\n');
  act(() => { editor.commands.insertContentAt(4, ' more'); });
  expect(editor.can().undo()).toBe(true);
  act(() => { handle.load('__Theirs__\n'); });
  expect(handle.markdown()).toBe('__Theirs__\n');
  expect(editor.can().undo()).toBe(false);
  await new Promise((resolve) => setTimeout(resolve, 200));
  expect(onChange).not.toHaveBeenCalledWith('__Theirs__\n');
});

test('the editor holds exactly what an article can store', async () => {
  const { editor } = await make('');
  expect(Object.keys(editor.schema.nodes).sort()).toEqual(Object.keys(schema.nodes).sort());
  expect(Object.keys(editor.schema.marks).sort()).toEqual(Object.keys(schema.marks).sort());
});

test('the toolbar formats the selected text', async () => {
  const { handle, editor } = await make('Make this strong\n');
  act(() => { editor.commands.setTextSelection({ from: 11, to: 17 }); });
  fireEvent.click(screen.getByRole('button', { name: /^Bold/ }));
  expect(handle.markdown()).toBe('Make this **strong**\n');
  expect(screen.getByRole('button', { name: /^Bold/ })).toHaveAttribute('aria-pressed', 'true');
  fireEvent.click(screen.getByRole('button', { name: /^Numbered list/ }));
  expect(handle.markdown()).toBe('1. Make this **strong**\n');
});

test('an image alt text set from the page, and a template inserted after the block the cursor is in', async () => {
  const { handle, editor } = await make('![](/api/v1/kb/images/1)\n\nNext\n');
  expect(handle.replace('![](/api/v1/kb/images/1)', '![Plans](/api/v1/kb/images/1)')).toBe(true);
  expect(handle.markdown()).toBe('![Plans](/api/v1/kb/images/1)\n\nNext\n');
  expect(handle.replace('![](/gone.png)', '![x](/gone.png)')).toBe(false);
  act(() => { editor.commands.setTextSelection(2); });
  act(() => { handle.insert('## Steps\n\n1. Open\n'); });
  expect(handle.markdown()).toBe('![Plans](/api/v1/kb/images/1)\n\n## Steps\n\n1. Open\n\nNext\n');
});

test('a pasted image waits on screen, never in the text, and goes in after the block the cursor is in', async () => {
  let finish: (value: { snippet: string }) => void = () => {};
  const done = vi.fn();
  const uploads: UploadHooks = { ...never, upload: () => new Promise((resolve) => { finish = resolve; }), onDone: done };
  const { handle, text, editor } = await make('Intro\n\nNext\n', uploads);
  act(() => { editor.commands.setTextSelection(2); });
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'plans.png', { type: 'image/png' })], types: ['Files'], getData: () => '' } });
  expect(await screen.findByText('Uploading plans.png…')).toBeInTheDocument();
  expect(handle.uploading()).toBe(true);
  expect(handle.markdown()).toBe('Intro\n\nNext\n');
  await act(async () => { finish({ snippet: '![plans](/api/v1/kb/images/1)' }); });
  expect(handle.markdown()).toBe('Intro\n\n![plans](/api/v1/kb/images/1)\n\nNext\n');
  expect(handle.uploading()).toBe(false);
  expect(screen.queryByText('Uploading plans.png…')).not.toBeInTheDocument();
  expect(done).toHaveBeenCalledTimes(1);
});

test('cancel takes the upload away; an upload whose text was replaced still ends', async () => {
  let finish: (value: { snippet: string }) => void = () => {};
  const error = vi.fn();
  const done = vi.fn();
  let calls = 0;
  const uploads: UploadHooks = { ...never, onError: error, onDone: done,
    upload: () => new Promise((resolve) => { calls += 1; if (calls === 2) finish = resolve; }) };
  const { handle, text } = await make('Text\n', uploads);
  const paste = (name: string) => fireEvent.paste(text, { clipboardData: { files: [new File(['x'], name, { type: 'image/png' })], types: ['Files'], getData: () => '' } });
  paste('a.png');
  fireEvent.click(await screen.findByRole('button', { name: 'Cancel the upload of a.png' }));
  expect(error).toHaveBeenCalledTimes(1);
  expect(handle.uploading()).toBe(false);
  paste('b.png');
  act(() => { handle.load('Theirs\n'); });
  await act(async () => { finish({ snippet: '![b](/api/v1/kb/images/2)' }); });
  expect(handle.markdown()).toBe('Theirs\n');
  expect(done).toHaveBeenCalledTimes(1);
});

test('copied formatted text goes in as the article stores it, its images first to the media library', async () => {
  let stored: (url: string) => void = () => {};
  const uploads: UploadHooks = { ...never, store: () => new Promise((resolve) => { stored = resolve; }) };
  const { handle, text, editor } = await make('Start\n', uploads);
  act(() => { editor.commands.setTextSelection(6); });
  const html = '<p><b>Bold</b> words</p><p><img src="https://example.com/a/shot.png" alt="Shot"></p>';
  fireEvent.paste(text, { clipboardData: { files: [], types: ['text/html', 'text/plain'], getData: (type: string) => (type === 'text/html' ? html : 'Bold words') } });
  expect(await screen.findByText('Uploading shot.png…')).toBeInTheDocument();
  expect(handle.markdown()).toBe('Start\n');
  await act(async () => { stored('/api/v1/kb/images/9'); });
  expect(handle.markdown()).toBe('Start**Bold** words\n\n![Shot](/api/v1/kb/images/9)\n');
});
