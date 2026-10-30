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
  // The less used tools are in More formatting, a menu at the end of the toolbar.
  fireEvent.click(within(toolbar).getByRole('button', { name: 'More formatting' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Divider' }));
  const undo = within(toolbar).getByRole('button', { name: /^Undo/ });
  await waitFor(() => expect(undo).toBeEnabled());
  within(toolbar).getByRole('button', { name: /^Bold/ }).focus();
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowRight' });
  expect(document.activeElement).toHaveAccessibleName(/^Italic/);
  fireEvent.keyDown(document.activeElement!, { key: 'End' });
  expect(document.activeElement).toHaveAccessibleName('More formatting');
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
  expect(document.activeElement).toBe(undo);
  fireEvent.click(undo);
  await waitFor(() => expect(undo).toBeDisabled());
  expect(enabled().filter((button) => button.tabIndex === 0)).toHaveLength(1);
});

test('a list, checklist or heading put on the line of the cursor leaves the cursor after its marker', async () => {
  const { handle, text } = await make('');
  const toolbar = screen.getByRole('toolbar', { name: 'Formatting' });
  fireEvent.click(within(toolbar).getByRole('button', { name: 'More formatting' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Checklist' }));
  // CodeMirror reads typed text from the page; inserting at the selection stands in for a key press here.
  const view = EditorView.findFromDOM(text)!;
  view.dispatch(view.state.replaceSelection('Open Settings'));
  expect(handle.markdown()).toBe('- [ ] Open Settings');
  view.dispatch(view.state.update({ changes: { from: view.state.doc.length, insert: '\n' }, selection: { anchor: view.state.doc.length + 1 } }));
  fireEvent.change(within(toolbar).getByRole('combobox', { name: 'Block type' }), { target: { value: 'h2' } });
  view.dispatch(view.state.replaceSelection('Steps'));
  expect(handle.markdown()).toBe('- [ ] Open Settings\n## Steps');
});

test('the block select offers Text, Heading 2 and Heading 3; a stored Heading 1 line still shows as one while the cursor is on it', async () => {
  const { text } = await make('Intro\n\n# Old title');
  const block = within(screen.getByRole('toolbar', { name: 'Formatting' })).getByRole('combobox', { name: 'Block type' });
  const options = () => within(block).getAllByRole('option').map((option) => option.textContent);
  // The article page shows the title as the page's h1; a Heading 1 in the text would be a second one.
  expect(options()).toEqual(['Text', 'Heading 2', 'Heading 3']);
  expect(block).toHaveValue('paragraph');
  const view = EditorView.findFromDOM(text)!;
  act(() => { view.dispatch({ selection: { anchor: view.state.doc.length } }); });
  await waitFor(() => expect(block).toHaveValue('h1'));
  expect(options()).toEqual(['Text', 'Heading 1', 'Heading 2', 'Heading 3']);
  act(() => { view.dispatch({ selection: { anchor: 0 } }); });
  await waitFor(() => expect(options()).toEqual(['Text', 'Heading 2', 'Heading 3']));
});

test('Insert video takes a YouTube or Vimeo link and puts its player in a block of its own; a link it cannot use says why', async () => {
  const { handle, text } = await make('Intro');
  // One Insert menu: the library, an upload, or a video site link, each said what it does.
  const open = () => { fireEvent.click(screen.getByRole('button', { name: 'Insert image or video' })); fireEvent.click(screen.getByRole('menuitem', { name: /^YouTube or Vimeo link/ })); };
  open();
  expect(screen.getByRole('dialog', { name: 'YouTube or Vimeo link' })).toBeInTheDocument();
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
  expect(screen.queryByRole('dialog', { name: 'YouTube or Vimeo link' })).not.toBeInTheDocument();
  await waitFor(() => expect(text).toHaveFocus());

  // Escape closes the dialog and gives the text back its focus.
  open();
  fireEvent.keyDown(screen.getByRole('textbox', { name: 'YouTube or Vimeo link' }), { key: 'Escape' });
  expect(screen.queryByRole('dialog', { name: 'YouTube or Vimeo link' })).not.toBeInTheDocument();
  await waitFor(() => expect(text).toHaveFocus());
});

test('the library picker takes several items in the order they are picked, with their alt text, each a block of its own', async () => {
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
  fireEvent.click(screen.getByRole('button', { name: 'Insert image or video' }));
  fireEvent.click(screen.getByRole('menuitem', { name: /^From the media library/ }));
  const picker = screen.getByRole('dialog', { name: 'Insert from the media library' });
  // The grid is one Tab stop: arrows move, Space picks.
  const shot = await within(picker).findByRole('option', { name: /^Shot 1, image/ });
  const tour = within(picker).getByRole('option', { name: /^Tour, video/ });
  expect([shot.tabIndex, tour.tabIndex]).toEqual([0, -1]);
  fireEvent.click(tour);
  shot.focus();
  fireEvent.keyDown(shot, { key: ' ' });
  expect(within(picker).getByText('2 selected, inserted in this order')).toBeInTheDocument();
  expect(within(picker).getByRole('textbox', { name: 'Alt text for Shot 1' })).toHaveValue('The inbox');
  fireEvent.click(within(picker).getByRole('button', { name: 'Insert 2' }));
  await waitFor(() => expect(handle!.markdown().trimEnd()).toBe(`Intro\n\n<video controls preload="metadata" src="/api/v1/kb/media/${id(2)}" title="Product &quot;tour&quot;"></video>\n\n![The inbox](/api/v1/kb/images/${id(1)})`));
  vi.unstubAllGlobals();
});

test('Cancel in an upload\'s place takes it away and aborts the upload', async () => {
  let signal: AbortSignal | undefined;
  const error = vi.fn();
  const uploads: UploadHooks = { ...never, onError: error, upload: (_file, _progress, given) => { signal = given; return new Promise(() => {}); } };
  const { handle, text } = await make('Text', uploads);
  fireEvent.paste(text, { clipboardData: { files: [new File(['x'], 'tour.webm', { type: 'video/webm' })] } });
  fireEvent.click(await screen.findByRole('button', { name: 'Cancel the upload of tour.webm' }));
  expect(signal?.aborted).toBe(true);
  expect(error).toHaveBeenCalledTimes(1);
  expect(handle.uploading()).toBe(false);
  expect(handle.markdown()).toBe('Text');
});

test('an image without alt text waits in the picker until it gets one, which is saved to the library as it stands, kept unchanged', async () => {
  const id = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000009';
  const bare = { id, kind: 'image', content_type: 'image/png', size: 10, sha256: 'a'.repeat(64), url: `/api/v1/kb/images/${id}`, name: 'Invoices', alt: '', width: 10, height: 10,
    duration_ms: null, codecs: [], thumbnail_url: null, poster_url: null, kept: false, deleted_at: null, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' };
  const sent: { method: string; body: unknown }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (_url: string, init: RequestInit = {}) => {
    if (init.method === 'PATCH') { sent.push({ method: 'PATCH', body: JSON.parse(init.body as string) }); return new Response(JSON.stringify({ ...bare, alt: 'The invoices list', updated_at: '2026-10-03T00:00:00Z' }), { status: 200 }); }
    return new Response(JSON.stringify({ items: [bare], next_cursor: null }), { status: 200 });
  }));
  const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
  let handle: EditorHandle | undefined;
  render(<QueryClientProvider client={new QueryClient()}><CodeMirrorEditor initial="Intro" uploads={never} label="Text" describedBy="help" invalid={false} mode="edit"
    onChange={() => undefined} onReady={(ready) => { handle = ready; }} /></QueryClientProvider>);
  await waitFor(() => expect(handle).toBeDefined());
  fireEvent.click(screen.getByRole('button', { name: 'Insert image or video' }));
  fireEvent.click(screen.getByRole('menuitem', { name: /^From the media library/ }));
  const picker = screen.getByRole('dialog', { name: 'Insert from the media library' });
  fireEvent.click(await within(picker).findByRole('option', { name: /^Invoices, image/ }));
  expect(within(picker).getByRole('button', { name: 'Insert 1' })).toBeDisabled();
  expect(within(picker).getByRole('button', { name: 'Insert 1' })).toHaveAccessibleDescription(/Insert waits for the alt text of one image/);
  fireEvent.change(within(picker).getByRole('textbox', { name: 'Alt text for Invoices' }), { target: { value: 'The invoices list' } });
  fireEvent.click(within(picker).getByRole('button', { name: 'Insert 1' }));
  await waitFor(() => expect(handle!.markdown().trimEnd()).toBe(`Intro\n\n![The invoices list](/api/v1/kb/images/${id})`));
  expect(sent).toEqual([{ method: 'PATCH', body: { updated_at: bare.updated_at, alt: 'The invoices list', kept: false } }]);
  vi.unstubAllGlobals();
});

test('alt text from the picker still goes in when the item was saved since without touching it (a poster drawn after its upload)', async () => {
  const id = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000010';
  const bare = { id, kind: 'image', content_type: 'image/png', size: 10, sha256: 'a'.repeat(64), url: `/api/v1/kb/images/${id}`, name: 'Plans', alt: '', width: 10, height: 10,
    duration_ms: null, codecs: [], thumbnail_url: null, poster_url: null, kept: true, deleted_at: null, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' };
  const versions: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init: RequestInit = {}) => {
    if (init.method === 'PATCH') {
      const body = JSON.parse(init.body as string) as { updated_at: string };
      versions.push(body.updated_at);
      return body.updated_at === bare.updated_at
        ? new Response(JSON.stringify({ error: { code: 'changed_elsewhere', message: 'Changed.' } }), { status: 409 })
        : new Response(JSON.stringify({ ...bare, alt: 'Plans', updated_at: '2026-10-05T00:00:00Z' }), { status: 200 });
    }
    if (url.endsWith('/details')) return new Response(JSON.stringify({ ...bare, updated_at: '2026-10-04T00:00:00Z', articles: [] }), { status: 200 });
    return new Response(JSON.stringify({ items: [bare], next_cursor: null }), { status: 200 });
  }));
  const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
  let handle: EditorHandle | undefined;
  render(<QueryClientProvider client={new QueryClient()}><CodeMirrorEditor initial="Intro" uploads={never} label="Text" describedBy="help" invalid={false} mode="edit"
    onChange={() => undefined} onReady={(ready) => { handle = ready; }} /></QueryClientProvider>);
  await waitFor(() => expect(handle).toBeDefined());
  fireEvent.click(screen.getByRole('button', { name: 'Insert image or video' }));
  fireEvent.click(screen.getByRole('menuitem', { name: /^From the media library/ }));
  const picker = screen.getByRole('dialog', { name: 'Insert from the media library' });
  fireEvent.click(await within(picker).findByRole('option', { name: /^Plans, image/ }));
  fireEvent.change(within(picker).getByRole('textbox', { name: 'Alt text for Plans' }), { target: { value: 'Plans' } });
  fireEvent.click(within(picker).getByRole('button', { name: 'Insert 1' }));
  await waitFor(() => expect(handle!.markdown().trimEnd()).toBe(`Intro\n\n![Plans](/api/v1/kb/images/${id})`));
  expect(versions).toEqual([bare.updated_at, '2026-10-04T00:00:00Z']);
  vi.unstubAllGlobals();
});

// A window as wide as a phone (kb-ui.tsx usePhone), switched on and off in a test.
function phoneScreen(on = true) {
  const listeners = new Set<() => void>();
  const query = { get matches() { return state.on; }, addEventListener: (_: string, listener: () => void) => listeners.add(listener), removeEventListener: (_: string, listener: () => void) => listeners.delete(listener) };
  const state = { on };
  const other = { matches: false, addEventListener: () => undefined, removeEventListener: () => undefined };
  vi.stubGlobal('matchMedia', (text: string) => (text.includes('44rem') ? query : other));
  return (next: boolean) => { state.on = next; act(() => { for (const listener of listeners) listener(); }); };
}
const libraryItem = (n: number, fields: Record<string, unknown> = {}) => ({ id: `aaaaaaaa-aaaa-4aaa-8aaa-${String(n).padStart(12, '0')}`, kind: 'image', content_type: 'image/png', size: 10, sha256: 'a'.repeat(64),
  url: `/api/v1/kb/images/aaaaaaaa-aaaa-4aaa-8aaa-${String(n).padStart(12, '0')}`, name: `Shot ${n}`, alt: '', width: 10, height: 10, duration_ms: null, codecs: [], thumbnail_url: null,
  poster_url: null, kept: true, deleted_at: null, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z', ...fields });
async function withLibrary(items: unknown[], answer: (url: string, init: RequestInit) => Response | undefined = () => undefined) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init: RequestInit = {}) => answer(url, init) ?? new Response(JSON.stringify({ items, next_cursor: null }), { status: 200 })));
  const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
  let handle: EditorHandle | undefined;
  render(<QueryClientProvider client={new QueryClient()}><CodeMirrorEditor initial="Intro" uploads={never} label="Text" describedBy="help" invalid={false} mode="edit"
    onChange={() => undefined} onReady={(ready) => { handle = ready; }} /></QueryClientProvider>);
  await waitFor(() => expect(handle).toBeDefined());
  fireEvent.click(screen.getByRole('button', { name: /^Insert/ }));
  fireEvent.click(screen.getByRole('menuitem', { name: /^From the media library/ }));
  return () => handle!;
}

test('when one alt text of several fails to save, Insert again saves the rest over the versions already saved and inserts all', async () => {
  const sent: string[] = [];
  let failB = true;
  const handle = await withLibrary([libraryItem(1), libraryItem(2)], (url, init) => {
    if (init.method !== 'PATCH') return undefined;
    const body = JSON.parse(init.body as string) as { updated_at: string; alt: string };
    sent.push(`${url.slice(-2)} ${body.updated_at}`);
    if (url.endsWith('02') && failB) { failB = false; return new Response(JSON.stringify({ error: { code: 'internal', message: 'x' } }), { status: 500 }); }
    // A's first save stored it: a second one from the old version would be refused.
    if (url.endsWith('01') && body.updated_at === '2026-10-01T00:00:00Z' && sent.filter((line) => line.startsWith('01')).length > 1) return new Response(JSON.stringify({ error: { code: 'changed_elsewhere', message: 'Changed.' } }), { status: 409 });
    return new Response(JSON.stringify(libraryItem(Number(url.slice(-2)), { alt: body.alt, updated_at: '2026-10-05T00:00:00Z' })), { status: 200 });
  });
  const picker = screen.getByRole('dialog', { name: 'Insert from the media library' });
  fireEvent.click(await within(picker).findByRole('option', { name: /^Shot 1, image/ }));
  fireEvent.click(within(picker).getByRole('option', { name: /^Shot 2, image/ }));
  fireEvent.change(within(picker).getByRole('textbox', { name: 'Alt text for Shot 1' }), { target: { value: 'One' } });
  fireEvent.change(within(picker).getByRole('textbox', { name: 'Alt text for Shot 2' }), { target: { value: 'Two' } });
  fireEvent.click(within(picker).getByRole('button', { name: 'Insert 2' }));
  expect(await within(picker).findByRole('alert')).toBeInTheDocument();
  fireEvent.click(within(picker).getByRole('button', { name: 'Insert 2' }));
  await waitFor(() => expect(handle().markdown().trimEnd()).toBe('Intro\n\n![One](/api/v1/kb/images/aaaaaaaa-aaaa-4aaa-8aaa-000000000001)\n\n![Two](/api/v1/kb/images/aaaaaaaa-aaaa-4aaa-8aaa-000000000002)'));
  vi.unstubAllGlobals();
});

test('on a phone the alt text step takes the focus, comes only with a pick, and Back gives the grid back its focus', async () => {
  phoneScreen();
  await withLibrary([libraryItem(1)]);
  const picker = screen.getByRole('dialog', { name: 'Insert from the media library' });
  const option = await within(picker).findByRole('option', { name: /^Shot 1, image/ });
  option.focus();
  fireEvent.keyDown(option, { key: 'Enter' });
  // Nothing picked: no empty step.
  expect(screen.queryByRole('dialog', { name: /^Alt text for/ })).not.toBeInTheDocument();
  fireEvent.click(option);
  fireEvent.click(within(picker).getByRole('button', { name: 'Next: alt text' }));
  const step = screen.getByRole('dialog', { name: 'Alt text for 1 item' });
  await waitFor(() => expect(step.contains(document.activeElement)).toBe(true));
  fireEvent.click(within(step).getAllByRole('button', { name: 'Back to the library' })[0]!);
  await waitFor(() => expect(screen.getByRole('dialog', { name: 'Insert from the media library' }).contains(document.activeElement)).toBe(true));
  vi.unstubAllGlobals();
});

test('the toolbar keeps one Tab stop when the window turns phone-sized and the button that held it is gone', async () => {
  const toPhone = phoneScreen(false);
  await make('Text');
  const toolbar = screen.getByRole('toolbar', { name: 'Formatting' });
  within(toolbar).getByRole('button', { name: /^Bold/ }).focus();
  fireEvent.keyDown(document.activeElement!, { key: 'End' });
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowLeft' });
  expect(document.activeElement).toHaveAccessibleName(/^Find and replace/);
  toPhone(true);
  expect(within(toolbar).getAllByRole('button').filter((button) => button.tabIndex === 0 && !button.hasAttribute('disabled'))).toHaveLength(1);
  vi.unstubAllGlobals();
});

test('in a column too narrow for every tool (a tablet held upright), the toolbar keeps Insert, Bold, Italic and Link and puts the rest under More', async () => {
  // CodeMirror watches its own boxes too: only the editor's shell hears the width.
  const shells: ((entries: { contentRect: { width: number } }[]) => void)[] = [];
  vi.stubGlobal('ResizeObserver', class {
    constructor(private readonly callback: (entries: { contentRect: { width: number } }[]) => void) {}
    observe(element: Element) { if (element.classList.contains('md-shell')) shells.push(this.callback); }
    unobserve() {}
    disconnect() {}
  });
  const report = (width: number) => { for (const callback of shells) callback([{ contentRect: { width } }]); };
  await make('Text');
  const toolbar = screen.getByRole('toolbar', { name: 'Formatting' });
  expect(within(toolbar).getByRole('button', { name: /^Find and replace/ })).toBeInTheDocument();
  act(() => report(500));
  expect(within(toolbar).queryByRole('button', { name: /^Find and replace/ })).not.toBeInTheDocument();
  expect(within(toolbar).getAllByRole('button').map((button) => button.getAttribute('aria-label') ?? button.textContent)).toEqual(
    ['Insert image or video', expect.stringMatching(/^Bold/), expect.stringMatching(/^Italic/), expect.stringMatching(/^Link/), 'More formatting']);
  fireEvent.click(within(toolbar).getByRole('button', { name: 'More formatting' }));
  expect(screen.getByRole('menuitem', { name: 'Find and replace' })).toBeInTheDocument();
  // The same blocks as the select of a wide toolbar: the title is the page's only Heading 1.
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent).filter((label) => /^(Text|Heading)/.test(label ?? ''))).toEqual(['Text', 'Heading 2', 'Heading 3']);
  vi.unstubAllGlobals();
});
