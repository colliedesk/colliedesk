import { act, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { HelpCenterPreview, previewPolicy, useThemePreview } from './help-center-preview';
import type { PreviewTheme } from './theme-api';
import { json, serve } from './kb.test.fixture';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const origin = window.location.origin;
const expectedPolicy = `default-src 'none'; img-src ${origin}/api/v1/kb/images/ ${origin}/api/v1/kb/media/; style-src ${origin}/help/_/ 'unsafe-inline'; script-src 'none'; form-action 'none'; base-uri 'none'`;
const policy = `<meta http-equiv="Content-Security-Policy" content="${expectedPolicy}">`;
const page = (title: string) => `<!doctype html><html><head><meta charset="utf-8">\n${policy}\n<style>:root{}</style><title>${title}</title></head><body><h1>${title}</h1></body></html>`;
const answer = (html: string) => ({ html, ok: true, checks: [], fields: {} });

test('the preview frame may show images of the workspace but run nothing: sandbox without allow-scripts, and the document holds script-src none', () => {
  // Images only from the knowledge base's image and media routes, style only from the help center's sheets.
  expect(previewPolicy(origin)).toBe(expectedPolicy);
  render(<HelpCenterPreview html={page('Acme help')} pending={false} page="home" onPage={() => {}} />);
  const frame = screen.getByTitle('Preview of the help center');
  expect(frame.tagName).toBe('IFRAME');
  expect(frame).toHaveAttribute('sandbox', 'allow-same-origin');
  expect(frame.getAttribute('sandbox')).not.toContain('allow-scripts');
  expect(frame.getAttribute('srcdoc')).toContain("script-src 'none'");
  expect(frame.getAttribute('srcdoc')).toContain('<h1>Acme help</h1>');
});

test('the frame takes no input until its document has loaded, and again while the next page replaces it', () => {
  // A click or a key before load reaches the page before the app holds its links; inert keeps both out.
  const { rerender } = render(<HelpCenterPreview html={page('One')} pending={false} page="home" onPage={() => {}} />);
  const frame = screen.getByTitle('Preview of the help center');
  expect(frame).toHaveAttribute('inert');
  fireEvent.load(frame);
  expect(frame).not.toHaveAttribute('inert');
  rerender(<HelpCenterPreview html={page('Two')} pending={false} page="home" onPage={() => {}} />);
  expect(screen.getByTitle('Preview of the help center')).toHaveAttribute('inert');
  fireEvent.load(screen.getByTitle('Preview of the help center'));
  expect(screen.getByTitle('Preview of the help center')).not.toHaveAttribute('inert');
});

test('a document without its own policy, or with a wider one, is never put in the frame', () => {
  const wider = page('Wide').replace(expectedPolicy, "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; script-src 'none'; form-action 'none'; base-uri 'none'");
  for (const html of ['<!doctype html><title>x</title><h1>Unsafe</h1>', wider]) {
    const { unmount } = render(<HelpCenterPreview html={html} pending={false} page="home" onPage={() => {}} />);
    expect(screen.queryByTitle('Preview of the help center')).not.toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('The preview could not be shown.');
    unmount();
  }
});

test('the width and page controls are labelled radios; Phone narrows the frame and the page choice is passed on', () => {
  const onPage = vi.fn();
  render(<HelpCenterPreview html={page('Acme help')} pending page="home" onPage={onPage} />);
  const stage = screen.getByTitle('Preview of the help center').parentElement!;
  expect(stage).toHaveAttribute('data-width', 'desktop');
  fireEvent.click(screen.getByRole('radio', { name: 'Phone' }));
  expect(stage).toHaveAttribute('data-width', 'phone');
  fireEvent.click(screen.getByRole('radio', { name: 'Article' }));
  expect(onPage).toHaveBeenCalledWith('article');
  expect(screen.getByRole('status')).toHaveTextContent('Updating…');
});

test('preview requests wait 300 ms after the last change, one at a time, and an answer to an older theme is dropped', async () => {
  vi.useFakeTimers();
  const pending: { body: { theme: PreviewTheme }; resolve: (response: Response) => void }[] = [];
  serve({ 'POST /api/v1/help-center/theme/preview': (call) => new Promise<Response>((resolve) => pending.push({ body: call.body as { theme: PreviewTheme }, resolve })) });
  const theme = (primary: string): PreviewTheme => ({ colors: { primary } });
  const { result, rerender } = renderHook(({ value }) => useThemePreview('home', value), { initialProps: { value: theme('#000001') } });

  rerender({ value: theme('#000002') });
  await act(async () => { await vi.advanceTimersByTimeAsync(299); });
  expect(pending).toHaveLength(0);
  rerender({ value: theme('#000003') });
  await act(async () => { await vi.advanceTimersByTimeAsync(299); });
  expect(pending).toHaveLength(0);
  await act(async () => { await vi.advanceTimersByTimeAsync(1); });
  expect(pending.map((call) => call.body.theme.colors?.primary)).toEqual(['#000003']);

  // Changes while it is in flight: no second request until it answers.
  rerender({ value: theme('#000004') });
  rerender({ value: theme('#000005') });
  await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
  expect(pending).toHaveLength(1);
  expect(result.current.pending).toBe(true);

  // The answer for #000003 is stale: dropped, and the latest theme goes out at once.
  await act(async () => { pending[0]!.resolve(json(200, answer('old'))); await vi.advanceTimersByTimeAsync(0); });
  expect(result.current.preview).toBeUndefined();
  expect(pending.map((call) => call.body.theme.colors?.primary)).toEqual(['#000003', '#000005']);

  await act(async () => { pending[1]!.resolve(json(200, answer('new'))); await vi.advanceTimersByTimeAsync(0); });
  expect(result.current.preview?.html).toBe('new');
  expect(result.current.pending).toBe(false);
});

test('a failed preview says so and keeps the last page shown', async () => {
  vi.useFakeTimers();
  let fail = false;
  serve({ 'POST /api/v1/help-center/theme/preview': () => (fail ? json(500) : json(200, answer('first'))) });
  const { result, rerender } = renderHook(({ value }) => useThemePreview('home', value), { initialProps: { value: { colors: { primary: '#000001' } } as PreviewTheme } });
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
  expect(result.current.preview?.html).toBe('first');
  fail = true;
  rerender({ value: { colors: { primary: '#000002' } } });
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
  expect(result.current.preview?.html).toBe('first');
  expect(result.current.error).toBe('The preview could not be shown.');
});

test('nothing is asked after the preview goes away, not even the change that was waiting', async () => {
  vi.useFakeTimers();
  const pending: ((response: Response) => void)[] = [];
  serve({ 'POST /api/v1/help-center/theme/preview': () => new Promise<Response>((resolve) => pending.push(resolve)) });
  const { rerender, unmount } = renderHook(({ value }) => useThemePreview('home', value), { initialProps: { value: { colors: { primary: '#000001' } } as PreviewTheme } });
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
  expect(pending).toHaveLength(1);
  rerender({ value: { colors: { primary: '#000002' } } });
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
  unmount();
  await act(async () => { pending[0]!(json(200, answer('late'))); await vi.advanceTimersByTimeAsync(1000); });
  expect(pending).toHaveLength(1);
});

test('no preview is asked while the hook is not enabled; once enabled, the theme of that moment goes out', async () => {
  vi.useFakeTimers();
  const calls = serve({ 'POST /api/v1/help-center/theme/preview': () => json(200, answer('saved')) });
  const { result, rerender } = renderHook(({ value, enabled }) => useThemePreview('home', value, enabled),
    { initialProps: { value: {} as PreviewTheme, enabled: false } });
  await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
  expect(calls.filter((call) => call.method === 'POST')).toHaveLength(0);
  rerender({ value: { colors: { primary: '#0b5cad' } }, enabled: true });
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
  const posts = calls.filter((call) => call.method === 'POST');
  expect(posts.map((call) => (call.body as { theme: PreviewTheme }).theme.colors?.primary)).toEqual(['#0b5cad']);
  expect(result.current.preview?.html).toBe('saved');
});
