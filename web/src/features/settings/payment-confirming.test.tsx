import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { PaymentConfirming } from './payment-confirming';

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { vi.useRealTimers(); });

test('for the first minute the page says how long this takes and keeps the order in view, without claiming the payment arrived', () => {
  render(<PaymentConfirming plan="founding" onCheckAgain={vi.fn()} />);
  expect(screen.getByRole('heading', { name: 'Confirming your payment' })).toBeInTheDocument();
  expect(screen.getByText('This usually takes under a minute. You can keep using Colliedesk or close this page.')).toBeInTheDocument();
  expect(screen.getByText('Plan')).toBeInTheDocument();
  expect(screen.getByText('Founding')).toBeInTheDocument();
  expect(screen.getByText('$99 every 3 months, price locked for life')).toBeInTheDocument();
  expect(screen.queryByText(/payment received/i)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Check again' })).not.toBeInTheDocument();
});

test('after 59 seconds nothing has changed, and at 60 seconds the page says it is still waiting and offers Check again', () => {
  const onCheckAgain = vi.fn();
  render(<PaymentConfirming plan="founding" onCheckAgain={onCheckAgain} />);
  act(() => { vi.advanceTimersByTime(59_000); });
  expect(screen.queryByRole('button', { name: 'Check again' })).not.toBeInTheDocument();
  act(() => { vi.advanceTimersByTime(1_000); });
  expect(screen.getByText(/still waiting for the payment provider\. if your card was charged, your plan updates on its own\./i)).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'support@colliedesk.com' })).toHaveAttribute('href', 'mailto:support@colliedesk.com');
  fireEvent.click(screen.getByRole('button', { name: 'Check again' }));
  expect(onCheckAgain).toHaveBeenCalledTimes(1);
  expect(screen.getByText('Founding')).toBeInTheDocument();
});

test('a Team 25 buyer sees the price of the period they chose; a buyer whose choice is not known sees no summary', () => {
  const { rerender } = render(<PaymentConfirming plan="team25_yearly" onCheckAgain={vi.fn()} />);
  expect(screen.getByText('Team 25')).toBeInTheDocument();
  expect(screen.getByText('$1,490 a year, 2 months free')).toBeInTheDocument();
  rerender(<PaymentConfirming onCheckAgain={vi.fn()} />);
  expect(screen.queryByText('Plan')).not.toBeInTheDocument();
  expect(screen.getByRole('heading', { name: 'Confirming your payment' })).toBeInTheDocument();
});

test('the wait is announced politely to a screen reader', () => {
  render(<PaymentConfirming plan="founding" onCheckAgain={vi.fn()} />);
  expect(screen.getByRole('region', { name: 'Confirming your payment' })).toHaveAttribute('aria-live', 'polite');
});
