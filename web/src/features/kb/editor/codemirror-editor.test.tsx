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
  let finish: (value: { snippet: string }) => void = () => {};
  const done = vi.fn();
  const uploads: UploadHooks = { ...never, upload: () => new Promise((resolve) => { finish = resolve; }), onDone: done };
  const { handle, text } = await make('| Plan | Agents |\n| --- | --- |\n| Team | 5 |\n\nNext', uploads);
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'plans.png', { type: 'image/png' })] } });
  expect(await screen.findByText('Uploading plans.png…')).toBeInTheDocument();
  expect(handle.uploading()).toBe(true);
  expect(handle.markdown()).toBe('| Plan | Agents |\n| --- | --- |\n| Team | 5 |\n\nNext');
  await act(async () => { finish({ snippet: '![plans](/api/v1/kb/images/1)' }); });
  expect(handle.markdown()).toBe('| Plan | Agents |\n| --- | --- |\n| Team | 5 |\n\n![plans](/api/v1/kb/images/1)\n\nNext');
  expect(handle.uploading()).toBe(false);
  expect(screen.queryByText('Uploading plans.png…')).not.toBeInTheDocument();
  expect(done).toHaveBeenCalledTimes(1);
});

test('a large upload shows how much has gone in its place', async () => {
  let report: (done: number) => void = () => {};
  let finish: (value: { snippet: string }) => void = () => {};
  const uploads: UploadHooks = { ...never, upload: (_file, progress) => { report = progress; return new Promise((resolve) => { finish = resolve; }); } };
  const { handle, text } = await make('Text', uploads);
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'tour.webm', { type: 'video/webm' })] } });
  await act(async () => { report(0.42); });
  expect(await screen.findByText('Uploading tour.webm: 42%')).toBeInTheDocument();
  await act(async () => { finish({ snippet: '<video controls preload="metadata" src="/api/v1/kb/media/1"></video>' }); });
  expect(handle.markdown().trimEnd()).toBe('Text\n\n<video controls preload="metadata" src="/api/v1/kb/media/1"></video>');
});

test('a refused upload only takes its place away; an upload whose text was replaced still ends', async () => {
  let refuse: (error: Error) => void = () => {};
  let finish: (value: { snippet: string }) => void = () => {};
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
  await act(async () => { finish({ snippet: '![b](/api/v1/kb/images/2)' }); });
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

test('Insert video takes a YouTube or Vimeo link and puts its player in a block of its own; a link it cannot use says why', async () => {
  const { handle, text } = await make('Intro');
  const open = screen.getByRole('button', { name: 'Insert video' });
  fireEvent.click(open);
  expect(open).toHaveAttribute('aria-expanded', 'true');
  const link = screen.getByRole('textbox', { name: 'YouTube or Vimeo link' });
  await waitFor(() => expect(link).toHaveFocus());

  fireEvent.change(link, { target: { value: 'https://evil.example/watch?v=jNQXAC9IVRw' } });
  fireEvent.keyDown(link, { key: 'Enter' });
  expect(screen.getByRole('alert')).toHaveTextContent('evil.example is not a video site articles can show: paste a YouTube or Vimeo link.');
  expect(link).toHaveAttribute('aria-invalid', 'true');
  expect(handle.markdown()).toBe('Intro');

  fireEvent.change(link, { target: { value: 'https://youtu.be/jNQXAC9IVRw?t=1m5s' } });
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  fireEvent.keyDown(link, { key: 'Enter' });
  expect(handle.markdown()).toBe('Intro\n\n<iframe src="https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=65" title="YouTube video" width="560" height="315" allow="encrypted-media; picture-in-picture" allowfullscreen loading="lazy"></iframe>\n');
  expect(screen.queryByRole('group', { name: 'Insert video' })).not.toBeInTheDocument();
  expect(open).toHaveAttribute('aria-expanded', 'false');
  await waitFor(() => expect(text).toHaveFocus());

  // Escape closes the row and gives the text back its focus.
  fireEvent.click(open);
  fireEvent.keyDown(screen.getByRole('textbox', { name: 'YouTube or Vimeo link' }), { key: 'Escape' });
  expect(screen.queryByRole('group', { name: 'Insert video' })).not.toBeInTheDocument();
  await waitFor(() => expect(text).toHaveFocus());
});

test('Media opens the library; a chosen image goes in as Markdown and a video as its HTML, each a block of its own', async () => {
  const id = (n: number) => `aaaaaaaa-aaaa-4aaa-8aaa-${String(n).padStart(12, '0')}`;
  const item = (n: number, fields: Record<string, unknown>) => ({ id: id(n), kind: 'image', content_type: 'image/png', size: 10, sha256: 'a'.repeat(64), url: `/api/v1/kb/images/${id(n)}`,
    name: `Shot ${n}`, alt: 'The inbox', width: 10, height: 10, duration_ms: null, codecs: [], thumbnail_url: null, poster_url: null, kept: true, deleted_at: null,
    created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z', ...fields });
  const items = [item(1, {}), item(2, { kind: 'video', content_type: 'video/mp4', url: `/api/v1/kb/media/${id(2)}`, name: 'Tour', alt: 'Product "tour"', duration_ms: 5000 })];
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ items, next_cursor: null }), { status: 200 })));
  const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
  let handle: EditorHandle | undefined;
  render(<QueryClientProvider client={new QueryClient()}><CodeMirrorEditor initial="Intro" uploads={never} label="Text" describedBy="help" invalid={false} mode="edit"
    onChange={() => undefined} onReady={(ready) => { handle = ready; }} /></QueryClientProvider>);
  await waitFor(() => expect(handle).toBeDefined());
  fireEvent.click(screen.getByRole('button', { name: 'Media' }));
  const picker = screen.getByRole('group', { name: 'Insert from the media library' });
  fireEvent.click(await within(picker).findByRole('button', { name: /^Shot 1, image/ }));
  expect(handle!.markdown().trimEnd()).toBe(`Intro\n\n![The inbox](/api/v1/kb/images/${id(1)})`);
  fireEvent.click(screen.getByRole('button', { name: 'Media' }));
  fireEvent.click(await screen.findByRole('button', { name: /^Tour, video/ }));
  expect(handle!.markdown().trimEnd()).toBe(`Intro\n\n![The inbox](/api/v1/kb/images/${id(1)})\n\n<video controls preload="metadata" src="/api/v1/kb/media/${id(2)}" title="Product &quot;tour&quot;"></video>`);
  vi.unstubAllGlobals();
});
