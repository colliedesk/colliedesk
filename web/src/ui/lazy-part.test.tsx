import type { ComponentType } from 'react';
import { act, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { collectDiagnostics } from '../lib/diagnostics';
import { lazyPart, PartUnavailable } from './lazy-part';

// Each test makes its own lazyPart: the state lives in the part (one per module in the app), so a part shared
// between tests would carry one test's outcome into the next.

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
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
