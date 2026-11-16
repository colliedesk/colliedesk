import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { useMinuteNow } from './use-now';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

// What a minute clock feeds: the rows of the list and their date formatting. Counted here, as React may call the
// hook's own component once more to find that nothing changed, then keep what it drew.
let renders = 0;
function Face({ now }: { now: Date }) {
  renders += 1;
  return <time>{now.toISOString().slice(11, 16)}</time>;
}
function Clock() {
  return <Face now={useMinuteNow()} />;
}

// Each alt-tab back to the inbox fires focus (and often visibilitychange). Within the same minute nothing a minute
// clock shows has changed, so the list and its rows are not drawn again; a new minute is.
test('the minute clock draws again only when the minute changed, not on every focus or visibility change', () => {
  vi.setSystemTime(new Date('2026-11-16T05:00:10Z'));
  renders = 0;
  render(<Clock />);
  const first = renders;
  act(() => {
    vi.setSystemTime(new Date('2026-11-16T05:00:40Z'));
    fireEvent.focus(window);
    fireEvent(document, new Event('visibilitychange'));
  });
  expect(renders).toBe(first);
  expect(screen.getByText('05:00')).toBeInTheDocument();
  act(() => {
    vi.setSystemTime(new Date('2026-11-16T05:01:05Z'));
    fireEvent.focus(window);
  });
  expect(screen.getByText('05:01')).toBeInTheDocument();
  expect(renders).toBe(first + 1);
});
