import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { EditorView } from '@codemirror/view';
import { expect, test, vi } from 'vitest';
import CodeMirrorEditor from './codemirror-editor';
import type { EditorHandle, UploadHooks } from './codemirror-editor';
import { googleDocsHTML } from './paste-html.test.fixture';

if (!('getClientRects' in Range.prototype)) Object.assign(Range.prototype, { getClientRects: () => [], getBoundingClientRect: () => new DOMRect() });

const never: UploadHooks = { accept: (files) => files, upload: () => new Promise(() => {}), store: () => new Promise(() => {}),
  onStart: () => {}, onDone: () => {}, onError: () => {} };

async function make(initial: string, uploads: UploadHooks = never) {
  let handle: EditorHandle | undefined;
  render(<CodeMirrorEditor initial={initial} uploads={uploads} label="Text" describedBy="help" invalid={false} mode="edit" onChange={vi.fn()}
    onReady={(ready) => { handle = ready; }} />);
  await waitFor(() => expect(handle).toBeDefined());
  const text = screen.getByRole('textbox', { name: 'Text' });
  return { handle: handle!, text, view: EditorView.findFromDOM(text)! };
}

// What a browser hands a paste: the copy as HTML and as plain text, and no files.
const clipboard = (html: string, plain: string) => ({ clipboardData: {
  files: [], types: ['text/html', 'text/plain'], getData: (type: string) => (type === 'text/html' ? html : type === 'text/plain' ? plain : ''),
} });

test('pasted HTML lands as Markdown, and the toast switches it to plain text and back in one click', async () => {
  const { handle, text, view } = await make('Intro\n\n');
  view.dispatch({ selection: { anchor: 7 } });
  fireEvent.paste(text, clipboard('<p>Open <b>Settings</b>, then <a href="https://example.com/s">Security</a></p><ul><li>One</li></ul>',
    'Open Settings, then Security\r\nOne'));
  expect(handle.markdown()).toBe('Intro\n\nOpen **Settings**, then [Security](https://example.com/s)\n\n- One');
  const keep = screen.getByRole('button', { name: 'Keep formatting' });
  const plain = screen.getByRole('button', { name: 'Paste as plain text' });
  expect(keep).toHaveAttribute('aria-pressed', 'true');
  fireEvent.click(plain);
  expect(handle.markdown()).toBe('Intro\n\nOpen Settings, then Security\nOne');
  expect(plain).toHaveAttribute('aria-pressed', 'true');
  fireEvent.click(keep);
  expect(handle.markdown()).toBe('Intro\n\nOpen **Settings**, then [Security](https://example.com/s)\n\n- One');
  // Typing on means the paste is settled: the toast goes.
  act(() => { view.dispatch({ changes: { from: 0, insert: 'X' }, userEvent: 'input.type' }); });
  expect(screen.queryByRole('button', { name: 'Paste as plain text' })).not.toBeInTheDocument();
});

test('a Google Docs copy waits on screen while its image is stored, then lands with the stored address', async () => {
  let finish: (url: string) => void = () => {};
  const store = vi.fn(() => new Promise<string>((resolve) => { finish = resolve; }));
  const done = vi.fn();
  const { handle, text } = await make('', { ...never, store, onDone: done });
  fireEvent.paste(text, clipboard(googleDocsHTML, 'Getting started'));
  expect(store).toHaveBeenCalledWith('https://lh7-rt.googleusercontent.com/docsz/AD_4nXcopy/photo.png', expect.any(AbortSignal));
  expect(await screen.findByText('Uploading photo.png…')).toBeInTheDocument();
  expect(handle.uploading()).toBe(true);
  expect(handle.markdown()).toBe('');
  await act(async () => { finish('/api/v1/kb/images/7'); });
  expect(handle.markdown()).toBe([
    '## Getting started',
    '- Open the app\n- Choose **Settings**',
    'See [the help page](https://example.com/help).',
    '|      |       |\n|------|-------|\n| Plan | Seats |\n| Team | 5     |',
    '![](/api/v1/kb/images/7)',
    '**Bold** and *italic* and plain',
  ].join('\n\n'));
  expect(handle.uploading()).toBe(false);
  expect(done).toHaveBeenCalledWith({ name: 'photo.png' });
  expect(screen.getByRole('button', { name: 'Keep formatting' })).toHaveAttribute('aria-pressed', 'true');
});

test('a pasted image that cannot be stored is left out and reported; the rest of the paste lands', async () => {
  const error = vi.fn();
  const store = (src: string) => (src.startsWith('data:') ? Promise.resolve('/api/v1/kb/images/8') : Promise.reject(new Error('Not an image.')));
  const { handle, text } = await make('', { ...never, store, onError: error });
  fireEvent.paste(text, clipboard('<p>A</p><p><img src="data:image/png;base64,iVBORw0KGgo="></p><p><img alt="gone" src="https://example.com/i/x.png"></p><p>B</p>', 'A\nB'));
  await waitFor(() => expect(handle.markdown()).toBe('A\n\n![](/api/v1/kb/images/8)\n\nB'));
  expect(error).toHaveBeenCalledTimes(1);
  expect(error.mock.calls[0][0]).toEqual({ name: 'x.png' });
});

test('cancelling a paste that waits on its images leaves the text as it was', async () => {
  const error = vi.fn();
  const { handle, text } = await make('Before', { ...never, onError: error });
  fireEvent.paste(text, clipboard('<p>New <img src="https://example.com/a.png"></p>', 'New'));
  fireEvent.click(await screen.findByRole('button', { name: 'Cancel the upload of a.png' }));
  expect(handle.uploading()).toBe(false);
  expect(handle.markdown()).toBe('Before');
  expect(error).toHaveBeenCalledTimes(1);
});

test('inside a code block a paste stays plain text', async () => {
  const { handle, text, view } = await make('```\n\n```');
  view.dispatch({ selection: { anchor: 4 } });
  fireEvent.paste(text, clipboard('<p><b>npm</b> install</p>', 'npm install'));
  expect(handle.markdown()).toBe('```\nnpm install\n```');
  expect(screen.queryByRole('button', { name: 'Paste as plain text' })).not.toBeInTheDocument();
});
