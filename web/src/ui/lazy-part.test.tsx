import type { ComponentType } from 'react';
import { act, render, screen } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { collectDiagnostics } from '../lib/diagnostics';
import { navigation, reloadMarkKey } from '../lib/stale-build';
import { lazyPart, PartUnavailable } from './lazy-part';

// Each test makes its own lazyPart: the state lives in the part (one per module in the app), so a part shared
// between tests would carry one test's outcome into the next.

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

beforeEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

// The page before this one already loaded the app again for a missing chunk a moment ago: one more failure is
// not a deploy the reload can fix, so it is shown, never reloaded in a loop.
function markReloadedByEarlierPage() {
  sessionStorage.setItem(reloadMarkKey, JSON.stringify({ at: Date.now(), page: 'an-earlier-page' }));
}

function Host({ part }: { part: ReturnType<typeof lazyPart<{ Text: ComponentType }>> }) {
  const state = part.use();
  if (state.status === 'ready') return <state.module.Text />;
  if (state.status === 'error') return <PartUnavailable />;
  return <p>placeholder</p>;
}

test('preload starts the import once; a part used later renders as soon as the module is there', async () => {
  const chunk = deferred<{ Text: ComponentType }>();
  const load = vi.fn(() => chunk.promise);
  const part = lazyPart(load);
  part.preload();
  part.preload();
  render(<Host part={part} />);
  expect(screen.getByText('placeholder')).toBeInTheDocument();
  await act(async () => chunk.resolve({ Text: () => <p>Loaded part</p> }));
  expect(screen.getByText('Loaded part')).toBeInTheDocument();
  expect(load).toHaveBeenCalledTimes(1);
});

test('a module already loaded renders on the first render, with no placeholder and no wait', async () => {
  const part = lazyPart(async () => ({ Text: () => <p>Loaded part</p> }));
  part.preload();
  await act(async () => {});
  render(<Host part={part} />);
  expect(screen.getByText('Loaded part')).toBeInTheDocument();
  expect(screen.queryByText('placeholder')).not.toBeInTheDocument();
});

test('a chunk that fails says so in its own place with a reload, and preload leaves no unhandled rejection', async () => {
  const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
  const chunk = deferred<{ Text: ComponentType }>();
  const part = lazyPart(() => chunk.promise);
  expect(() => part.preload()).not.toThrow();
  render(<main><h1>Settings</h1><Host part={part} /><p>Web form</p></main>);
  await act(async () => chunk.reject(new TypeError('Failed to fetch dynamically imported module')));
  expect(screen.getByRole('alert')).toHaveTextContent('This part of the page could not be loaded.');
  expect(screen.getByRole('button', { name: 'Reload the page' })).toBeInTheDocument();
  expect(screen.getByRole('heading', { name: 'Settings' })).toBeInTheDocument();
  expect(screen.getByText('Web form')).toBeInTheDocument();
  // The failure is not swallowed: the console says so, and "Report a problem" can attach it.
  expect(warn).toHaveBeenCalledWith(expect.stringContaining('could not be loaded'), expect.any(TypeError));
  expect(collectDiagnostics().client_errors?.some((error) => error.source === 'lazy-part' && error.message.includes('Failed to fetch dynamically imported module'))).toBe(true);
  warn.mockRestore();
});

test('using the part without a preload starts the import', async () => {
  const load = vi.fn(async () => ({ Text: () => <p>Loaded part</p> }));
  render(<Host part={lazyPart(load)} />);
  expect(await screen.findByText('Loaded part')).toBeInTheDocument();
  expect(load).toHaveBeenCalledTimes(1);
});

test('ready settles once the part can render, loaded or failed, never rejects, and imports once', async () => {
  const loaded = deferred<{ Text: ComponentType }>();
  const load = vi.fn(() => loaded.promise);
  const part = lazyPart(load);
  let settled = false;
  void part.ready().then(() => { settled = true; });
  await act(async () => {});
  expect(settled).toBe(false);
  await act(async () => loaded.resolve({ Text: () => <p>Loaded part</p> }));
  expect(settled).toBe(true);
  await part.ready();
  expect(load).toHaveBeenCalledTimes(1);

  const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
  const failed = deferred<{ Text: ComponentType }>();
  const broken = lazyPart(() => failed.promise);
  const waiting = broken.ready();
  failed.reject(new TypeError('Failed to fetch dynamically imported module'));
  await expect(waiting).resolves.toBeUndefined();
  render(<Host part={broken} />);
  expect(screen.getByRole('alert')).toHaveTextContent('This part of the page could not be loaded.');
  warn.mockRestore();
});

test('a chunk missing after a deploy loads the app again once, at the address the person was going to, and shows no error', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  const assign = vi.spyOn(navigation, 'assign').mockImplementation(() => {});
  const failed = deferred<{ Text: ComponentType }>();
  const part = lazyPart(() => failed.promise);
  const waiting = part.ready({ target: '/mentions?filter=unread' });
  render(<Host part={part} />);
  await act(async () => failed.reject(new TypeError('Failed to fetch dynamically imported module')));
  await expect(waiting).resolves.toBeUndefined();
  expect(assign).toHaveBeenCalledTimes(1);
  expect(assign).toHaveBeenCalledWith('/mentions?filter=unread');
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  expect(screen.getByText('placeholder')).toBeInTheDocument();
  // Still recorded, so "Report a problem" can tell a deploy reload from a network fault.
  expect(collectDiagnostics().client_errors?.some((error) => error.source === 'lazy-part')).toBe(true);
});

test('two parts failing on the same old page load the app again only once', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  const assign = vi.spyOn(navigation, 'assign').mockImplementation(() => {});
  const one = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
  const two = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
  await Promise.all([one.ready({ target: '/knowledge' }), two.ready({ target: '/mentions' })]);
  expect(assign).toHaveBeenCalledTimes(1);
  expect(assign).toHaveBeenCalledWith('/knowledge');
});

test('a chunk that still fails right after that reload is shown with its Reload link and loads nothing again', async () => {
  markReloadedByEarlierPage();
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  const assign = vi.spyOn(navigation, 'assign').mockImplementation(() => {});
  const part = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
  render(<Host part={part} />);
  await act(async () => part.ready({ target: '/mentions' }));
  expect(assign).not.toHaveBeenCalled();
  expect(screen.getByRole('alert')).toHaveTextContent('This part of the page could not be loaded.');
  expect(screen.getByRole('button', { name: 'Reload the page' })).toBeInTheDocument();
});

test('a reload that happened long ago does not stop the next deploy from recovering', async () => {
  sessionStorage.setItem(reloadMarkKey, JSON.stringify({ at: Date.now() - 10 * 60_000, page: 'an-earlier-page' }));
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  const assign = vi.spyOn(navigation, 'assign').mockImplementation(() => {});
  const part = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
  await part.ready({ target: '/knowledge' });
  expect(assign).toHaveBeenCalledWith('/knowledge');
});

test('without session storage the app cannot guard a loop, so it shows the error instead of reloading', async () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('denied', 'SecurityError'); });
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  const assign = vi.spyOn(navigation, 'assign').mockImplementation(() => {});
  const part = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
  render(<Host part={part} />);
  await act(async () => part.ready({ target: '/knowledge' }));
  expect(assign).not.toHaveBeenCalled();
  expect(screen.getByRole('alert')).toBeInTheDocument();
});

test('a page loaded ahead of a click (pointer over a link) that fails reloads nothing until the person opens it', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  const assign = vi.spyOn(navigation, 'assign').mockImplementation(() => {});
  const part = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
  await part.ready({ target: '/mentions', ahead: true });
  expect(assign).not.toHaveBeenCalled();
  render(<Host part={part} />);
  await act(async () => part.ready({ target: '/mentions' }));
  expect(assign).toHaveBeenCalledTimes(1);
  expect(assign).toHaveBeenCalledWith('/mentions');
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('a part inside a page (a picker, a dialog) that fails is shown with its Reload link and never reloads by itself: the person may be typing', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  const assign = vi.spyOn(navigation, 'assign').mockImplementation(() => {});
  const part = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
  render(<main><textarea aria-label="Message body" defaultValue="My draft" /><Host part={part} /></main>);
  await act(async () => part.ready());
  expect(assign).not.toHaveBeenCalled();
  expect(screen.getByRole('alert')).toHaveTextContent('This part of the page could not be loaded.');
  expect(screen.getByLabelText('Message body')).toHaveValue('My draft');
});

test('a page loaded ahead of a click that failed reloads at its address once the page is shown', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  const assign = vi.spyOn(navigation, 'assign').mockImplementation(() => {});
  const part = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
  await part.ready({ target: '/mentions', ahead: true });
  expect(assign).not.toHaveBeenCalled();
  // The router keeps the match it loaded ahead, moves to its address and only renders the page: the page's own
  // use() asks again.
  window.history.replaceState(null, '', '/mentions');
  await act(async () => { render(<Host part={part} />); });
  expect(assign).toHaveBeenCalledWith('/mentions');
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  window.history.replaceState(null, '', '/');
});

test('a part loaded ahead of a click for another page never reloads the page shown now when it is used here', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  const assign = vi.spyOn(navigation, 'assign').mockImplementation(() => {});
  window.history.replaceState(null, '', '/conversations/42');
  const part = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
  await part.ready({ target: '/fixes', ahead: true });
  // The same part draws a block inside the open conversation: the person may be typing a reply there.
  await act(async () => { render(<Host part={part} />); });
  expect(assign).not.toHaveBeenCalled();
  expect(screen.getByRole('alert')).toHaveTextContent('This part of the page could not be loaded.');
  window.history.replaceState(null, '', '/');
});

test('a reload the person cancels (Stay on the leave prompt) shows the error with its Reload link a moment later', async () => {
  vi.useFakeTimers();
  try {
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(navigation, 'assign').mockImplementation(() => {});
    const part = lazyPart(() => Promise.reject(new TypeError('Failed to fetch dynamically imported module')));
    render(<Host part={part} />);
    await act(async () => part.ready({ target: '/mentions' }));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    await act(async () => { vi.advanceTimersByTime(6000); });
    expect(screen.getByRole('alert')).toHaveTextContent('This part of the page could not be loaded.');
  } finally {
    vi.useRealTimers();
  }
});
