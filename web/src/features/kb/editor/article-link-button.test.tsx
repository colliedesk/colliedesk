import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { EditorView } from '@codemirror/view';
import { afterEach, expect, test, vi } from 'vitest';
import CodeMirrorEditor from './codemirror-editor';
import type { EditorHandle, UploadHooks } from './codemirror-editor';
import { json, serve, summary } from '../kb.test.fixture';

if (!('getClientRects' in Range.prototype)) Object.assign(Range.prototype, { getClientRects: () => [], getBoundingClientRect: () => new DOMRect() });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

const never: UploadHooks = { accept: (files) => files, upload: () => new Promise(() => {}), onStart: () => {}, onDone: () => {}, onError: () => {} };
const center = { enabled: true, title: '', locale: 'en', content_version: 3, updated_at: null, url: 'https://desk.example.com/help/acme' };
const reset = summary(7, 'Reset your password', 'published');
const refund = summary(8, 'Refund steps', 'published');

async function open(initial: string, articleId?: string) {
  const calls = serve({
    'GET /api/v1/help-center': () => json(200, center),
    'GET /api/v1/kb/articles': () => json(200, { items: [reset, refund], next_cursor: null }),
  });
  let handle: EditorHandle | undefined;
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <CodeMirrorEditor initial={initial} uploads={never} label="Text" describedBy="help" invalid={false} mode="edit" articleId={articleId}
      onChange={() => undefined} onReady={(ready) => { handle = ready; }} /></QueryClientProvider>);
  await waitFor(() => expect(handle).toBeDefined());
  const view = EditorView.findFromDOM(screen.getByRole('textbox', { name: 'Text' }))!;
  return { handle: handle!, view, calls };
}

test('Link to article opens the article list, and picking one makes the selected text its link', async () => {
  const { handle, view } = await open('See how to reset it.');
  view.dispatch({ selection: { anchor: 8, head: 20 } });
  fireEvent.click(within(screen.getByRole('toolbar', { name: 'Formatting' })).getByRole('button', { name: 'Link to article' }));
  expect(await screen.findByRole('option', { name: 'Reset your password' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('option', { name: 'Reset your password' }));
  expect(handle.markdown()).toBe('See how [to reset it.](/a/7-reset-your-password)');
  expect(screen.queryByRole('dialog', { name: 'Articles' })).not.toBeInTheDocument();
  await waitFor(() => expect(view.hasFocus).toBe(true));
});

test('with nothing selected, Enter on a search result links it by its title at the cursor', async () => {
  const { handle, view } = await open('Read  first.');
  view.dispatch({ selection: { anchor: 5 } });
  fireEvent.click(screen.getByRole('button', { name: 'Link to article' }));
  const search = await screen.findByRole('combobox', { name: 'Search published articles' });
  await screen.findByRole('option', { name: 'Refund steps' });
  fireEvent.keyDown(search, { key: 'ArrowDown' });
  fireEvent.keyDown(search, { key: 'Enter' });
  expect(handle.markdown()).toBe('Read [Refund steps](/a/8-refund-steps) first.');
});

test('Escape closes the list without changing the text and puts the cursor back in the editor', async () => {
  const { handle, view } = await open('Text');
  fireEvent.click(screen.getByRole('button', { name: 'Link to article' }));
  fireEvent.keyDown(await screen.findByRole('combobox', { name: 'Search published articles' }), { key: 'Escape' });
  expect(screen.queryByRole('dialog', { name: 'Articles' })).not.toBeInTheDocument();
  expect(handle.markdown()).toBe('Text');
  await waitFor(() => expect(view.hasFocus).toBe(true));
});

test('the article being edited is not offered', async () => {
  await open('Text', reset.id);
  fireEvent.click(screen.getByRole('button', { name: 'Link to article' }));
  expect(await screen.findByRole('option', { name: 'Refund steps' })).toBeInTheDocument();
  expect(screen.queryByRole('option', { name: 'Reset your password' })).not.toBeInTheDocument();
});

test('on a phone the button is under More formatting and still opens the list and links the article', async () => {
  const shells: ((entries: { contentRect: { width: number } }[]) => void)[] = [];
  vi.stubGlobal('ResizeObserver', class {
    constructor(private readonly callback: (entries: { contentRect: { width: number } }[]) => void) {}
    observe(element: Element) { if (element.classList.contains('md-shell')) shells.push(this.callback); }
    unobserve() {}
    disconnect() {}
  });
  const { handle } = await open('Text');
  act(() => { for (const callback of shells) callback([{ contentRect: { width: 500 } }]); });
  const toolbar = screen.getByRole('toolbar', { name: 'Formatting' });
  expect(within(toolbar).queryByRole('button', { name: 'Link to article' })).not.toBeInTheDocument();
  fireEvent.click(within(toolbar).getByRole('button', { name: 'More formatting' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Link to article' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Refund steps' }));
  expect(handle.markdown()).toBe('[Refund steps](/a/8-refund-steps)Text');
});
