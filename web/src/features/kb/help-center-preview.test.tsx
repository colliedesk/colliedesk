import { act, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { HelpCenterPreview, previewPolicy, useThemePreview } from './help-center-preview';
import type { ThemeInput } from './theme-api';
import { json, serve } from './kb.test.fixture';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const policy = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; script-src 'none'; form-action 'none'; base-uri 'none'">`;
const page = (title: string) => `<!doctype html><html><head><meta charset="utf-8">\n${policy}\n<style>:root{}</style><title>${title}</title></head><body><h1>${title}</h1></body></html>`;
const answer = (html: string) => ({ html, ok: true, checks: [], fields: {} });

test('the preview frame may show images of the app but run nothing: sandbox without allow-scripts, and the document holds script-src none', () => {
  expect(previewPolicy).toBe("default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; script-src 'none'; form-action 'none'; base-uri 'none'");
  render(<HelpCenterPreview html={page('Acme help')} pending={false} page="home" onPage={() => {}} />);
  const frame = screen.getByTitle('Preview of the help center');
  expect(frame.tagName).toBe('IFRAME');
  expect(frame).toHaveAttribute('sandbox', 'allow-same-origin');
  expect(frame.getAttribute('sandbox')).not.toContain('allow-scripts');
  expect(frame.getAttribute('srcdoc')).toContain("script-src 'none'");
  expect(frame.getAttribute('srcdoc')).toContain('<h1>Acme help</h1>');
});

test('a document without its own policy is never put in the frame', () => {
  render(<HelpCenterPreview html="<!doctype html><title>x</title><h1>Unsafe</h1>" pending={false} page="home" onPage={() => {}} />);
  expect(screen.queryByTitle('Preview of the help center')).not.toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent('The preview could not be shown.');
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
  const pending: { body: { theme: ThemeInput }; resolve: (response: Response) => void }[] = [];
  serve({ 'POST /api/v1/help-center/theme/preview': (call) => new Promise<Response>((resolve) => pending.push({ body: call.body as { theme: ThemeInput }, resolve })) });
  const theme = (primary: string): ThemeInput => ({ colors: { primary } });
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
  const { result, rerender } = renderHook(({ value }) => useThemePreview('home', value), { initialProps: { value: { colors: { primary: '#000001' } } as ThemeInput } });
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
  expect(result.current.preview?.html).toBe('first');
  fail = true;
  rerender({ value: { colors: { primary: '#000002' } } });
  await act(async () => { await vi.advanceTimersByTimeAsync(300); });
  expect(result.current.preview?.html).toBe('first');
  expect(result.current.error).toBe('The preview could not be shown.');
});
