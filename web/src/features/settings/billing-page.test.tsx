import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { BillingPage } from './billing-page';

const free: Billing = {
  plan: 'trial', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'none', current_period_end: null, cancel_at_period_end: false, active_subscriptions: 0, complimentary: false, complimentary_until: null, checkout_plans: ['team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 10, replies_per_24h: 200, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, usage: { agents: 2, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};
const team: Billing = {
  plan: 'team', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'active', current_period_end: '2026-11-15T09:30:00Z', cancel_at_period_end: false, active_subscriptions: 1, complimentary: false, complimentary_until: null, checkout_plans: ['team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 10, replies_per_24h: null, storage_bytes: 107374182400, storage_warn_bytes: 85899345920 }, usage: { agents: 6, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};

test('a trial workspace sees when the trial ends, its usage and five ways to subscribe', () => {
  const onCheckout = vi.fn();
  render(<BillingPage billing={free} timezone="UTC" onCheckout={onCheckout} onManage={vi.fn()} />);
  expect(screen.getByRole('heading', { name: /plan and billing/i })).toBeInTheDocument();
  expect(screen.getByText('Free trial')).toBeInTheDocument();
  expect(screen.getByText(/trial ends oct 15/i)).toBeInTheDocument();
  expect(screen.getByText(/2 of 10 agents/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /manage billing/i })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Team, $49 per month' }));
  fireEvent.click(screen.getByRole('button', { name: 'Team, $490 per year' }));
  fireEvent.click(screen.getByRole('button', { name: 'Team 25, $99 per month' }));
  fireEvent.click(screen.getByRole('button', { name: 'Team 25, $990 per year' }));
  fireEvent.click(screen.getByRole('button', { name: /founding.*\$100.*year/i }));
  expect(onCheckout.mock.calls).toEqual([['team_monthly'], ['team_yearly'], ['team25_monthly'], ['team25_yearly'], ['founding']]);
});

test('a server without the Team 25 products offers only the plans it can sell', () => {
  render(<BillingPage billing={{ ...free, checkout_plans: ['team_monthly', 'team_yearly', 'founding'] }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getAllByRole('button').map((button) => button.textContent)).toEqual(['Team, $49 per month', 'Team, $490 per year', 'Founding, $100 per year for life']);
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

test('a Team 25 workspace is named, shows 25 agents and its 250 GiB cap, and has no upgrade buttons', () => {
  render(<BillingPage billing={{ ...team, plan: 'team25', limits: { agents: 25, replies_per_24h: null, storage_bytes: 268435456000, storage_warn_bytes: 214748364800 } }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText('Team 25')).toBeInTheDocument();
  expect(screen.getByText(/6 of 25 agents/i)).toBeInTheDocument();
  expect(screen.getByText('Attachment storage: 0 B of 250.0 GiB')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /\$99/ })).not.toBeInTheDocument();
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
  render(<BillingPage billing={{ ...free, plan: 'none', locked: true, limits: { agents: 0, replies_per_24h: null, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, status: 'canceled', current_period_end: '2026-10-01T00:00:00Z' }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
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


test('a complimentary workspace says so, shows its end and offers no billing portal or checkout', () => {
  const comp: Billing = { ...team, status: 'canceled', current_period_end: '2027-01-01T00:00:00Z', complimentary: true, complimentary_until: '2027-01-01T00:00:00Z', active_subscriptions: 1 };
  render(<BillingPage billing={comp} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText(/complimentary team plan \(until jan 1, 2027/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /manage billing/i })).not.toBeInTheDocument();
  expect(screen.queryByText(/cancels|renews/i)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /\$49/ })).not.toBeInTheDocument();
});

test('a complimentary plan with no end says it has none', () => {
  const comp: Billing = { ...team, status: 'active', current_period_end: null, complimentary: true, complimentary_until: null };
  render(<BillingPage billing={comp} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText(/^complimentary team plan$/i)).toBeInTheDocument();
});

test('a trial shows its attachment storage and replies, and warns from 80 percent', () => {
  const GiB = 1024 ** 3;
  const withStorage = (bytes: number): Billing => ({ ...free, usage: { ...free.usage, storage_bytes: bytes, replies_last_24h: 12 } });
  const { rerender } = render(<BillingPage billing={withStorage(1.7 * GiB)} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText(/storage: 1\.7 GiB of 2\.0 GiB/i)).toBeInTheDocument();
  expect(screen.getByText(/12 of 200 replies in the last 24 hours/i)).toBeInTheDocument();
  rerender(<BillingPage billing={{ ...withStorage(1.7 * GiB), limits: { ...free.limits, replies_per_24h: 150 } }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText(/12 of 150 replies/i)).toBeInTheDocument();
  rerender(<BillingPage billing={withStorage(1.7 * GiB)} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByRole('status')).toHaveTextContent(/storage is 85% full/i);
  rerender(<BillingPage billing={withStorage(1.5 * GiB)} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

test('a paid plan shows its storage cap and no reply cap', () => {
  render(<BillingPage billing={team} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText('Attachment storage: 0 B of 100.0 GiB')).toBeInTheDocument();
  expect(screen.queryByText(/replies in the last 24 hours/i)).not.toBeInTheDocument();
});
