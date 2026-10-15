import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { BillingPage } from './billing-page';

const free: Billing = {
  plan: 'trial', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'none', current_period_end: null, cancel_at_period_end: false, active_subscriptions: 0,
  limits: { agents: 10 }, usage: { agents: 2, pending_invites: 0, replies_last_24h: 0 },
};
const team: Billing = {
  plan: 'team', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'active', current_period_end: '2026-11-15T09:30:00Z', cancel_at_period_end: false, active_subscriptions: 1,
  limits: { agents: 10 }, usage: { agents: 6, pending_invites: 0, replies_last_24h: 0 },
};

test('a trial workspace sees when the trial ends, its usage and three ways to subscribe', () => {
  const onCheckout = vi.fn();
  render(<BillingPage billing={free} timezone="UTC" onCheckout={onCheckout} onManage={vi.fn()} />);
  expect(screen.getByRole('heading', { name: /plan and billing/i })).toBeInTheDocument();
  expect(screen.getByText('Free trial')).toBeInTheDocument();
  expect(screen.getByText(/trial ends oct 15/i)).toBeInTheDocument();
  expect(screen.getByText(/2 of 10 agents/i)).toBeInTheDocument();
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

test('after the trial or a subscription ended the workspace is locked but can still pick a plan and open billing', () => {
  render(<BillingPage billing={{ ...free, plan: 'none', locked: true, limits: { agents: 0 }, status: 'canceled', current_period_end: '2026-10-01T00:00:00Z' }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText('Trial ended')).toBeInTheDocument();
  expect(screen.getByText(/trial ended oct 15/i)).toBeInTheDocument();
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

test('pending invites are shown because they hold a seat against the cap', () => {
  render(<BillingPage billing={{ ...free, usage: { ...free.usage, agents: 1, pending_invites: 2, replies_last_24h: 0 } }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText(/1 of 10 agents, plus 2 pending invites/i)).toBeInTheDocument();
});

