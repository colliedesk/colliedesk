import { act, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { EmailFrame } from './email-frame';
import { fitFrame } from './fit-frame';

// jsdom does no layout, so a frame's measurements are what the test says they are.
function measured(frame: HTMLIFrameElement, size: { width: number; contentWidth: number; contentHeight: number }) {
  const doc = frame.contentDocument!;
  Object.defineProperty(frame, 'clientWidth', { configurable: true, value: size.width });
  for (const element of [doc.documentElement, doc.body]) {
    Object.defineProperty(element, 'scrollWidth', { configurable: true, value: size.contentWidth });
    Object.defineProperty(element, 'scrollHeight', { configurable: true, value: size.contentHeight });
  }
  return doc;
}

function blankFrame(): HTMLIFrameElement {
  const frame = document.createElement('iframe');
  document.body.appendChild(frame);
  return frame;
}

afterEach(() => document.body.replaceChildren());

test('fitFrame gives the frame the full height of its content, so it never scrolls inside', () => {
  const frame = blankFrame();
  measured(frame, { width: 600, contentWidth: 600, contentHeight: 1234 });
  fitFrame(frame);
  expect(frame.style.height).toBe('1234px');
  expect(frame.contentDocument!.body.style.transform).toBe('');
});

test('fitFrame scales content that is wider than the column instead of clipping it', () => {
  const frame = blankFrame();
  const doc = measured(frame, { width: 500, contentWidth: 1000, contentHeight: 800 });
  fitFrame(frame);
  expect(doc.body.style.transform).toBe('scale(0.5)');
  expect(doc.body.style.width).toBe('1000px');
  expect(frame.style.height).toBe('400px');
});

test('fitFrame undoes the scale when the column grows wide enough again', () => {
  const frame = blankFrame();
  const doc = measured(frame, { width: 500, contentWidth: 1000, contentHeight: 800 });
  fitFrame(frame);
  measured(frame, { width: 1100, contentWidth: 1000, contentHeight: 800 });
  fitFrame(frame);
  expect(doc.body.style.transform).toBe('');
  expect(frame.style.height).toBe('800px');
});

let observers: Array<{ callback: () => void; targets: Element[]; disconnected: boolean }> = [];

beforeEach(() => {
  observers = [];
  vi.stubGlobal('ResizeObserver', class {
    entry = { callback: () => {}, targets: [] as Element[], disconnected: false };
    constructor(callback: () => void) { this.entry.callback = callback; observers.push(this.entry); }
    observe(target: Element) { this.entry.targets.push(target); }
    unobserve() {}
    disconnect() { this.entry.disconnected = true; }
  });
});
afterEach(() => vi.unstubAllGlobals());

test('the frame follows its content: it measures when loaded and again when the content or the column resizes', async () => {
  const { container, unmount } = render(<EmailFrame title="Original message" html="<p>Hello</p>" />);
  const frame = container.querySelector('iframe')!;
  // jsdom loads the frame twice (blank, then the srcdoc); the frame follows the document it shows last.
  const current = () => observers.at(-1)!;
  await waitFor(() => expect(current()?.targets.includes(frame.contentDocument!.body)).toBe(true));
  expect(current().targets).toContain(frame);
  measured(frame, { width: 600, contentWidth: 600, contentHeight: 300 });
  act(() => current().callback());
  expect(frame.style.height).toBe('300px');
  measured(frame, { width: 600, contentWidth: 600, contentHeight: 900 }); // an image finished loading
  act(() => current().callback());
  expect(frame.style.height).toBe('900px');
  const last = current();
  unmount();
  expect(last.disconnected).toBe(true);
  expect(observers.slice(0, -1).every((observer) => observer.disconnected)).toBe(true);
});

test('the frame is sandboxed without scripts and carries the locked document', () => {
  const { container } = render(<EmailFrame title="Original message" html='<p onclick="x()">Hello</p>' />);
  const frame = container.querySelector('iframe')!;
  const sandbox = (frame.getAttribute('sandbox') ?? '').split(/\s+/).sort();
  expect(sandbox).toEqual(['allow-popups', 'allow-popups-to-escape-sandbox', 'allow-same-origin']);
  const srcdoc = frame.getAttribute('srcdoc') ?? '';
  expect(srcdoc).toContain("script-src 'none'");
  expect(srcdoc).toContain(`img-src data: ${window.location.origin}`);
  expect(srcdoc).not.toContain('onclick');
  expect(frame).toHaveAttribute('scrolling', 'no');
});
