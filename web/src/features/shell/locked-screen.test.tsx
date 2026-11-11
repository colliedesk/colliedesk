import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { LockedScreen } from './locked-screen';

const ended: Billing = {
  plan: 'none', trial_ends_at: '2026-10-15T09:00:00Z', locked: true, status: 'none', current_period_end: null, cancel_at_period_end: false,
  active_subscriptions: 0, complimentary: false, complimentary_until: null, checkout_refusals: [], storage_upgrade_plan: null, purchased_plan: null, migration_requested_at: null, checkout_plans: ['team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly'],
  limits: { agents: 0, replies_per_24h: null, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, usage: { agents: 2, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};
const exportSlot = <button type="button">Export all data</button>;

test('while Founding is open a locked workspace is offered Founding and Team 25, as on the website', () => {
  const onCheckout = vi.fn();
  render(<LockedScreen isAdmin billing={{ ...ended, checkout_plans: [...ended.checkout_plans, 'founding'] }} onCheckout={onCheckout} onSignOut={vi.fn()} />);
  expect(screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'))).toEqual(['Founding', 'Team 25']);
  expect(screen.getByRole('button', { name: 'Choose Founding, $99 per quarter for life' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /solo|small|team, /i })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Choose Team 25, $149 per month' }));
  fireEvent.click(screen.getByRole('radio', { name: /yearly/i }));
  fireEvent.click(screen.getByRole('button', { name: 'Choose Team 25, $1,490 per year' }));
  expect(onCheckout.mock.calls).toEqual([['team25_monthly'], ['team25_yearly']]);
});

test('a locked workspace whose visitor came for Team 25 finds it first and marked', () => {
  render(<LockedScreen isAdmin billing={{ ...ended, checkout_plans: [...ended.checkout_plans, 'founding'] }} preferredPlan="team25" onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'))).toEqual(['Team 25', 'Founding']);
  expect(screen.getByText('Your pick')).toBeInTheDocument();
});

test('the locked screen offers only the plans the server can sell', () => {
  render(<LockedScreen isAdmin billing={{ ...ended, checkout_plans: ['team_monthly', 'team_yearly'] }} onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.queryByRole('button', { name: /team 25/i })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: /team, \$69 per month/i })).toBeInTheDocument();
});

test('an admin whose trial ended can choose each plan, export, and write for more time', () => {
  const onCheckout = vi.fn();
  render(<LockedScreen isAdmin billing={ended} exportAction={exportSlot} onCheckout={onCheckout} onSignOut={vi.fn()} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Your trial has ended' })).toBeInTheDocument();
  expect(screen.getByText('Nothing is deleted. Choose a plan to open the workspace again, or export everything first.')).toBeInTheDocument();
  expect(screen.getByRole('heading', { name: 'Choose a plan' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /team, \$69 per month/i }));
  fireEvent.click(screen.getByRole('radio', { name: /yearly/i }));
  fireEvent.click(screen.getByRole('button', { name: /team, \$690 per year/i }));
  expect(onCheckout.mock.calls).toEqual([['team_monthly'], ['team_yearly']]);
  expect(screen.getByRole('button', { name: 'Export all data' })).toBeInTheDocument();
  expect(screen.getByText(/need more time\? email/i)).toHaveTextContent('Need more time? Email support@colliedesk.com');
  expect(screen.getByRole('link', { name: 'support@colliedesk.com' })).toHaveAttribute('href', 'mailto:support@colliedesk.com');
});

test('the locked screen holds one ink button: the plan that fits the team, with the compact cards and no list of what is included', () => {
  const { container } = render(<LockedScreen isAdmin billing={{ ...ended, checkout_plans: [...ended.checkout_plans, 'founding'] }} exportAction={exportSlot} onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(container.querySelectorAll('.primary-action')).toHaveLength(1);
  expect(container.querySelector('.plan-picker')).toHaveAttribute('data-compact');
  expect(screen.queryByText('20 GiB attachment storage')).not.toBeInTheDocument();
  expect(screen.getByText('You have 2 agents. Founding covers up to 10.')).toBeInTheDocument();
});

test('the Founding places left are read from the server and shown', () => {
  render(<LockedScreen isAdmin billing={{ ...ended, checkout_plans: [...ended.checkout_plans, 'founding'] }} placesLeft={7} onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.getByText('Price locked for life · 7 places left')).toBeInTheDocument();
});

test('an admin of a workspace whose subscription ended is told that, not "trial"', () => {
  render(<LockedScreen isAdmin billing={{ ...ended, status: 'canceled', current_period_end: '2026-10-01T00:00:00Z' }}
    exportAction={exportSlot} onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Your subscription has ended' })).toBeInTheDocument();
});

test('an agent who is not an admin is told which admins to ask, and gets no plan or export controls, only Sign out', () => {
  const onSignOut = vi.fn();
  render(<LockedScreen isAdmin={false} adminNames={['Maya Reyes', 'Daniel Kim']} onCheckout={vi.fn()} onSignOut={onSignOut} />);
  // Only an admin reads billing, so a person who is not one cannot be told whether a trial or a subscription ended.
  expect(screen.getByRole('heading', { level: 1, name: 'This workspace is locked' })).toBeInTheDocument();
  expect(screen.getByText('Nothing is deleted. Ask an admin to choose a plan: Maya Reyes, Daniel Kim.')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /team|founding/i })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /export/i })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }));
  expect(onSignOut).toHaveBeenCalledTimes(1);
});

test('an agent whose admins are not known is still told to ask one', () => {
  render(<LockedScreen isAdmin={false} onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.getByText('Nothing is deleted. Ask an admin to choose a plan.')).toBeInTheDocument();
});

test('a failed checkout says nothing was charged and offers Try again', () => {
  const retry = vi.fn();
  render(<LockedScreen isAdmin billing={ended} checkoutFailed exportAction={exportSlot} onCheckout={vi.fn()} onRetryCheckout={retry} onSignOut={vi.fn()} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Checkout did not open. Nothing was charged.');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(retry).toHaveBeenCalledTimes(1);
});

test('a lapsed subscription can be fixed in the billing portal instead of paying twice', () => {
  const onManage = vi.fn();
  render(<LockedScreen isAdmin billing={{ ...ended, status: 'past_due', current_period_end: '2026-10-01T00:00:00Z' }}
    exportAction={exportSlot} onCheckout={vi.fn()} onManage={onManage} onSignOut={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'Manage billing' }));
  expect(onManage).toHaveBeenCalledTimes(1);
});

test('an ended trial has no billing account to manage, and a complimentary plan has none either', () => {
  const { rerender } = render(<LockedScreen isAdmin billing={ended} exportAction={exportSlot} onCheckout={vi.fn()} onManage={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.queryByRole('button', { name: 'Manage billing' })).not.toBeInTheDocument();
  rerender(<LockedScreen isAdmin billing={{ ...ended, status: 'canceled', complimentary: true }} exportAction={exportSlot} onCheckout={vi.fn()} onManage={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.queryByRole('button', { name: 'Manage billing' })).not.toBeInTheDocument();
});

test('the heading takes focus when the page appears, so keyboard and screen reader users land on it', () => {
  render(<LockedScreen isAdmin billing={ended} exportAction={exportSlot} onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.getByRole('heading', { level: 1 })).toHaveFocus();
});

test('the retention promise is a floor, not a schedule', () => {
  render(<LockedScreen isAdmin billing={ended} exportAction={exportSlot} onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.getByText(/at least 12 months.*at least 30 days/i)).toBeInTheDocument();
});
