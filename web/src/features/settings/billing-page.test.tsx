import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { BillingPage } from './billing-page';

const free: Billing = {
  plan: 'free', status: 'none', current_period_end: null, cancel_at_period_end: false, active_subscriptions: 0,
  limits: { agents: 3, conversations_per_month: 100 }, usage: { agents: 2, conversations_this_month: 40 },
};
const team: Billing = {
  plan: 'team', status: 'active', current_period_end: '2026-11-15T09:30:00Z', cancel_at_period_end: false, active_subscriptions: 1,
  limits: { agents: 10, conversations_per_month: null }, usage: { agents: 6, conversations_this_month: 412 },
};

test('a free workspace sees its usage against the limits and three ways to upgrade', () => {
  const onCheckout = vi.fn();
  render(<BillingPage billing={free} timezone="UTC" onCheckout={onCheckout} onManage={vi.fn()} />);
  expect(screen.getByRole('heading', { name: /plan and billing/i })).toBeInTheDocument();
  expect(screen.getByText('Free')).toBeInTheDocument();
  expect(screen.getByText(/2 of 3 agents/i)).toBeInTheDocument();
  expect(screen.getByText(/40 of 100 new conversations this month/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /manage billing/i })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /team.*\$49.*month/i }));
  fireEvent.click(screen.getByRole('button', { name: /team.*\$490.*year/i }));
  fireEvent.click(screen.getByRole('button', { name: /founding.*\$100.*year/i }));
  expect(onCheckout.mock.calls).toEqual([['team_monthly'], ['team_yearly'], ['founding']]);
});

test('a paying workspace sees renewal, no upgrade buttons and a way to manage billing', () => {
  const onManage = vi.fn();
  render(<BillingPage billing={team} timezone="UTC" onCheckout={vi.fn()} onManage={onManage} />);
  expect(screen.getByText('Team')).toBeInTheDocument();
  expect(screen.getByText(/renews nov 15/i)).toBeInTheDocument();
  expect(screen.getByText(/6 of 10 agents/i)).toBeInTheDocument();
  expect(screen.getByText(/412 new conversations this month \(no limit\)/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /\$49/ })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /manage billing/i }));
  expect(onManage).toHaveBeenCalled();
});

test('a cancelled plan says when access ends', () => {
  render(<BillingPage billing={{ ...team, status: 'canceled', cancel_at_period_end: true }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText(/cancels nov 15/i)).toBeInTheDocument();
  expect(screen.queryByText(/renews/i)).not.toBeInTheDocument();
});

test('a failed payment is flagged while the plan stays active', () => {
  render(<BillingPage billing={{ ...team, status: 'past_due' }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText('Team')).toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent(/last payment failed/i);
});

test('after a subscription ended the workspace is free again but can still open billing', () => {
  render(<BillingPage billing={{ ...free, status: 'canceled', current_period_end: '2026-10-01T00:00:00Z' }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText('Free')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: /manage billing/i })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: /team, \$49 per month/i })).toBeInTheDocument();
});

test('shows a message after returning from checkout and an error when an action fails', () => {
  const { rerender } = render(<BillingPage billing={free} timezone="UTC" checkoutReturned onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByRole('status')).toHaveTextContent(/payment received/i);
  rerender(<BillingPage billing={free} timezone="UTC" failed onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByRole('alert')).toHaveTextContent(/billing action failed/i);
});

test('two active subscriptions are flagged so the admin cancels the extra one', () => {
  const onManage = vi.fn();
  render(<BillingPage billing={{ ...team, active_subscriptions: 2 }} timezone="UTC" onCheckout={vi.fn()} onManage={onManage} />);
  expect(screen.getByRole('alert')).toHaveTextContent(/more than one active subscription/i);
  expect(screen.getByRole('alert')).toHaveTextContent(/cancel the extra one/i);
  expect(screen.queryByRole('alert', { name: /last payment/i })).not.toBeInTheDocument();
});
