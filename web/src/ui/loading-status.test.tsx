import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { LoadingStatus } from './loading-status';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

const slowText = 'Still loading. Your connection to Colliedesk is slow.';

test('a short wait shows only the loading status', () => {
  render(<LoadingStatus onRetry={() => undefined} />);
  act(() => { vi.advanceTimersByTime(4_900); });
  expect(screen.getByRole('status')).toHaveTextContent('Loading workspace…');
  expect(screen.queryByText(slowText)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
});

test('after 5 seconds the status says the connection is slow and offers Try again', () => {
  const onRetry = vi.fn();
  render(<LoadingStatus onRetry={onRetry} />);
  act(() => { vi.advanceTimersByTime(5_000); });
  expect(screen.getByRole('status')).toHaveTextContent(slowText);
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(onRetry).toHaveBeenCalledTimes(1);
});

test('Try again starts a new wait: the slow notice comes back only if the new request is slow too', () => {
  render(<LoadingStatus onRetry={() => undefined} />);
  act(() => { vi.advanceTimersByTime(5_000); });
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(screen.queryByText(slowText)).not.toBeInTheDocument();
  act(() => { vi.advanceTimersByTime(5_000); });
  expect(screen.getByText(slowText)).toBeInTheDocument();
});

test('without a retry the slow notice still shows, with no button', () => {
  render(<LoadingStatus />);
  act(() => { vi.advanceTimersByTime(5_000); });
  expect(screen.getByRole('status')).toHaveTextContent(slowText);
  expect(screen.queryByRole('button')).not.toBeInTheDocument();
});

test('with a page shape, the wait shows grey blocks in that shape and keeps the words for screen readers only', () => {
  const { container } = render(<LoadingStatus shape={<i className="skeleton-line" />} />);
  const status = screen.getByRole('status');
  expect(status).toHaveTextContent('Loading workspace…');
  expect(status).toHaveClass('sr-only');
  expect(container.querySelector('[aria-busy="true"]')).toContainElement(status);
  const blocks = container.querySelector('[aria-hidden="true"]');
  expect(blocks?.querySelector('.skeleton-line')).not.toBeNull();
});

test('a slow wait over a page shape says so out loud, above the blocks', () => {
  render(<LoadingStatus shape={<i className="skeleton-line" />} onRetry={() => undefined} />);
  act(() => { vi.advanceTimersByTime(5_000); });
  expect(screen.getByRole('status')).not.toHaveClass('sr-only');
  expect(screen.getByRole('status')).toHaveTextContent(slowText);
  expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
});
