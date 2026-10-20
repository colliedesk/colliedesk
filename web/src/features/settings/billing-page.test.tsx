import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { BillingPage } from './billing-page';

const free: Billing = {
  plan: 'trial', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'none', current_period_end: null, cancel_at_period_end: false, active_subscriptions: 0, complimentary: false, complimentary_until: null, checkout_plans: ['solo_monthly', 'solo_yearly', 'small_monthly', 'small_yearly', 'team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 10, replies_per_24h: 200, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, usage: { agents: 2, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};
const team: Billing = {
  plan: 'team', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'active', current_period_end: '2026-11-15T09:30:00Z', cancel_at_period_end: false, active_subscriptions: 1, complimentary: false, complimentary_until: null, checkout_plans: ['team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 10, replies_per_24h: null, storage_bytes: 107374182400, storage_warn_bytes: 85899345920 }, usage: { agents: 6, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};

test('a trial workspace sees when the trial ends, its usage and every way to subscribe, smallest plan first', () => {
  const onCheckout = vi.fn();
  render(<BillingPage billing={free} timezone="UTC" onCheckout={onCheckout} onManage={vi.fn()} />);
  expect(screen.getByRole('heading', { name: /plan and billing/i })).toBeInTheDocument();
  expect(screen.getByText('Free trial')).toBeInTheDocument();
  expect(screen.getByText(/trial ends oct 15/i)).toBeInTheDocument();
  expect(screen.getByText(/2 of 10 agents/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /manage billing/i })).not.toBeInTheDocument();
  expect(screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'))).toEqual(['Solo', 'Small', 'Team', 'Team 25', 'Founding']);
  fireEvent.click(screen.getByRole('button', { name: 'Choose Solo, $19 per month' }));
  fireEvent.click(screen.getByRole('button', { name: 'Choose Small, $39 per month' }));
  fireEvent.click(screen.getByRole('button', { name: 'Choose Team, $69 per month' }));
  fireEvent.click(screen.getByRole('button', { name: 'Choose Team 25, $149 per month' }));
  fireEvent.click(screen.getByRole('radio', { name: /yearly/i }));
  fireEvent.click(screen.getByRole('button', { name: 'Choose Solo, $190 per year' }));
  fireEvent.click(screen.getByRole('button', { name: 'Choose Small, $390 per year' }));
  fireEvent.click(screen.getByRole('button', { name: 'Choose Team, $690 per year' }));
  fireEvent.click(screen.getByRole('button', { name: 'Choose Team 25, $1,490 per year' }));
  fireEvent.click(screen.getByRole('button', { name: /founding.*\$100.*year/i }));
  expect(onCheckout.mock.calls).toEqual([['solo_monthly'], ['small_monthly'], ['team_monthly'], ['team25_monthly'],
    ['solo_yearly'], ['small_yearly'], ['team_yearly'], ['team25_yearly'], ['founding']]);
});

test('a server without the optional products offers only the plans it can sell', () => {
  render(<BillingPage billing={{ ...free, checkout_plans: ['team_monthly', 'team_yearly', 'founding'] }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getAllByRole('button').map((button) => button.getAttribute('aria-label'))).toEqual(['Choose Team, $69 per month', 'Choose Founding, $100 per year for life']);
  for (const name of ['Solo', 'Small', 'Team 25']) expect(screen.queryByRole('article', { name })).not.toBeInTheDocument();
});

test('a paying workspace sees renewal, no upgrade buttons and a way to manage billing', () => {
  const onManage = vi.fn();
  render(<BillingPage billing={team} timezone="UTC" onCheckout={vi.fn()} onManage={onManage} />);
  expect(within(screen.getByRole('region', { name: 'Current plan' })).getByText('Team')).toBeInTheDocument();
  expect(screen.getByText(/renews nov 15/i)).toBeInTheDocument();
  expect(screen.getByText(/6 of 10 agents/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /\$69/ })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /manage billing/i }));
  expect(onManage).toHaveBeenCalled();
});

test('a Team 25 workspace is named, shows 25 agents and its 250 GiB cap, and has no upgrade buttons', () => {
  render(<BillingPage billing={{ ...team, plan: 'team25', limits: { agents: 25, replies_per_24h: null, storage_bytes: 268435456000, storage_warn_bytes: 214748364800 } }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(within(screen.getByRole('region', { name: 'Current plan' })).getByText('Team 25')).toBeInTheDocument();
  expect(screen.getByText(/6 of 25 agents/i)).toBeInTheDocument();
  expect(screen.getByText('Attachment storage: 0 B of 250.0 GiB')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /\$149/ })).not.toBeInTheDocument();
});

test('a Solo workspace is named and shows its 1 agent and 10 GiB cap', () => {
  render(<BillingPage billing={{ ...team, plan: 'solo', usage: { ...team.usage, agents: 1 }, limits: { agents: 1, replies_per_24h: null, storage_bytes: 10737418240, storage_warn_bytes: 8589934592 } }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(within(screen.getByRole('region', { name: 'Current plan' })).getByText('Solo')).toBeInTheDocument();
  expect(screen.getByText(/1 of 1 agents/i)).toBeInTheDocument();
  expect(screen.getByText('Attachment storage: 0 B of 10.0 GiB')).toBeInTheDocument();
});

test('a cancelled plan says when access ends', () => {
  render(<BillingPage billing={{ ...team, status: 'canceled', cancel_at_period_end: true }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText(/cancels nov 15/i)).toBeInTheDocument();
  expect(screen.queryByText(/renews/i)).not.toBeInTheDocument();
});

test('a failed payment is flagged while the plan stays active', () => {
  render(<BillingPage billing={{ ...team, status: 'past_due' }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(within(screen.getByRole('region', { name: 'Current plan' })).getByText('Team')).toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent(/last payment failed/i);
});

test('after the trial or a subscription ended the workspace is locked but can still pick a plan and open billing', () => {
  render(<BillingPage billing={{ ...free, plan: 'none', locked: true, limits: { agents: 0, replies_per_24h: null, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, status: 'canceled', current_period_end: '2026-10-01T00:00:00Z' }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText('Trial ended')).toBeInTheDocument();
  expect(screen.getByText(/trial ended oct 15/i)).toBeInTheDocument();
  expect(screen.getByRole('button', { name: /manage billing/i })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: /team, \$69 per month/i })).toBeInTheDocument();
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
  expect(screen.queryByRole('button', { name: /\$69/ })).not.toBeInTheDocument();
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
  expect(screen.getByText(/12 of 200 people emailed in the last 24 hours/i)).toBeInTheDocument();
  rerender(<BillingPage billing={{ ...withStorage(1.7 * GiB), limits: { ...free.limits, replies_per_24h: 150 } }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText(/12 of 150 people emailed/i)).toBeInTheDocument();
  rerender(<BillingPage billing={withStorage(1.7 * GiB)} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByRole('status')).toHaveTextContent(/storage is 85% full/i);
  rerender(<BillingPage billing={withStorage(1.5 * GiB)} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

test('a paid plan shows its storage cap and no reply cap', () => {
  render(<BillingPage billing={team} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText('Attachment storage: 0 B of 100.0 GiB')).toBeInTheDocument();
  expect(screen.queryByText(/people emailed in the last 24 hours/i)).not.toBeInTheDocument();
});

test('each plan is a card with its price, what it includes and one clear Choose button; Yearly switches the prices', () => {
  render(<BillingPage billing={free} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  const card = (name: string) => screen.getByRole('article', { name });
  expect(card('Solo')).toHaveTextContent('$19per monthBilled monthly');
  expect(card('Solo')).toHaveTextContent('1 agent');
  expect(card('Solo')).not.toHaveTextContent('Up to');
  expect(card('Solo')).toHaveTextContent('10 GiB attachment storage');
  expect(card('Small')).toHaveTextContent('$39per month');
  expect(card('Small')).toHaveTextContent('Up to 3 agents');
  expect(card('Small')).toHaveTextContent('30 GiB attachment storage');
  expect(card('Team')).toHaveTextContent('$69per monthBilled monthly');
  expect(card('Team')).toHaveTextContent('Up to 10 agents');
  expect(card('Team')).toHaveTextContent('100 GiB attachment storage');
  expect(card('Team 25')).toHaveTextContent('Up to 25 agents');
  expect(card('Team 25')).toHaveTextContent('250 GiB attachment storage');
  expect(card('Founding')).toHaveTextContent('10 workspaces only');
  // One solid button per plan, so buying is the obvious action on the page.
  for (const name of ['Solo', 'Small', 'Team', 'Team 25', 'Founding']) {
    const buttons = within(card(name)).getAllByRole('button');
    expect(buttons.map((button) => button.textContent)).toEqual([`Choose ${name}`]);
    expect(buttons[0]).toHaveClass('primary-action');
  }
  expect(screen.getByRole('radio', { name: 'Monthly' })).toBeChecked();
  fireEvent.click(screen.getByRole('radio', { name: /yearly/i }));
  expect(card('Solo')).toHaveTextContent('Billed $190 yearly');
  expect(card('Small')).toHaveTextContent('Billed $390 yearly');
  expect(card('Team')).toHaveTextContent('Billed $690 yearly');
  expect(card('Team 25')).toHaveTextContent('Billed $1,490 yearly');
  expect(card('Founding')).toHaveTextContent('Billed $100 yearly');
});

test('without any monthly and yearly pair there is no Monthly/Yearly switch', () => {
  render(<BillingPage billing={{ ...free, checkout_plans: ['founding'] }} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.queryByRole('radio')).not.toBeInTheDocument();
});

test('usage reads as meters with their numbers', () => {
  render(<BillingPage billing={free} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  const agents = screen.getByRole('meter', { name: 'Agents' });
  expect(agents).toHaveAttribute('aria-valuenow', '2');
  expect(agents).toHaveAttribute('aria-valuemax', '10');
  expect(screen.getByRole('meter', { name: 'Attachment storage' })).toHaveAttribute('aria-valuenow', '0');
  expect(screen.getByRole('meter', { name: 'People emailed in 24 hours' })).toHaveAttribute('aria-valuemax', '200');
});

test('trial and renewal dates are days, not times', () => {
  const { rerender } = render(<BillingPage billing={free} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText(/trial ends Oct 15, 2026\./)).toBeInTheDocument();
  expect(screen.queryByText(/\d:\d\d/)).not.toBeInTheDocument();
  rerender(<BillingPage billing={team} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByText('Renews Nov 15, 2026.')).toBeInTheDocument();
});

test('yearly prices read per month with what is billed, so plans compare at a glance', () => {
  render(<BillingPage billing={free} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  const card = (name: string) => screen.getByRole('article', { name });
  expect(card('Founding')).toHaveTextContent('$8.33per month');
  expect(card('Founding')).toHaveTextContent('Billed $100 yearly, the price never changes');
  fireEvent.click(screen.getByRole('radio', { name: /yearly/i }));
  expect(card('Solo')).toHaveTextContent('$15.83per month');
  expect(card('Small')).toHaveTextContent('$32.50per month');
  expect(card('Team')).toHaveTextContent('$57.50per month');
  expect(card('Team')).toHaveTextContent('Billed $690 yearly');
  expect(card('Team 25')).toHaveTextContent('$124.17per month');
  expect(card('Team 25')).toHaveTextContent('Billed $1,490 yearly');
});

test('the current plan, its usage and Manage billing sit together at the top', () => {
  render(<BillingPage billing={team} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  const current = screen.getByRole('region', { name: 'Current plan' });
  expect(within(current).getByRole('button', { name: /manage billing/i })).toBeInTheDocument();
  expect(within(current).getByRole('meter', { name: 'Agents' })).toBeInTheDocument();
});
