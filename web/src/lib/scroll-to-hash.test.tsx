import { fireEvent, render } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { useScrollToHashWhenReady } from './scroll-to-hash';

// The page grows above the section as later parts arrive; ResizeObserver says so.
let grew = () => {};
class FakeResizeObserver { constructor(callback: () => void) { grew = callback; } observe() {} disconnect() { grew = () => {}; } }

function Page({ ready }: { ready: boolean }) {
  useScrollToHashWhenReady(ready);
  return <main><h2 id="export-heading">Export all data</h2></main>;
}

function open() {
  const scrolled = vi.fn();
  Element.prototype.scrollIntoView = scrolled;
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  window.history.replaceState(null, '', '/settings#export-heading');
  return scrolled;
}

afterEach(() => {
  window.history.replaceState(null, '', '/');
  delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView;
  vi.unstubAllGlobals();
  grew = () => {};
});

test('the section stays in view while the page above it fills in', () => {
  const scrolled = open();
  const view = render(<Page ready={false} />);
  expect(scrolled).not.toHaveBeenCalled();
  view.rerender(<Page ready />);
  expect(scrolled).toHaveBeenCalledOnce();
  grew();
  expect(scrolled).toHaveBeenCalledTimes(2);
  expect(scrolled.mock.contexts[1]).toBe(document.getElementById('export-heading'));
});

test('once the person scrolls, the page grows under them without moving them', () => {
  const scrolled = open();
  render(<Page ready />);
  fireEvent.wheel(window, { deltaY: 120 });
  grew();
  expect(scrolled).toHaveBeenCalledOnce();
});

test('another address on the same page is left alone', () => {
  const scrolled = open();
  const view = render(<Page ready={false} />);
  window.history.replaceState(null, '', '/settings#sessions-heading');
  view.rerender(<Page ready />);
  expect(scrolled).not.toHaveBeenCalled();
});

test('the section stops being followed after a few seconds', () => {
  vi.useFakeTimers();
  try {
    const scrolled = open();
    render(<Page ready />);
    vi.advanceTimersByTime(10_000);
    grew();
    expect(scrolled).toHaveBeenCalledOnce();
  } finally { vi.useRealTimers(); }
});
