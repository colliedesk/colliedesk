import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { GiB, paidBilling } from './billing-fixtures';
import { PaidBillingRows } from './paid-billing-rows';

type Props = Partial<Parameters<typeof PaidBillingRows>[0]>;
function show(billing: Billing, props: Props = {}) {
  const handlers = { onRequestMigration: vi.fn(), onManage: vi.fn() };
  const { unmount } = render(<PaidBillingRows billing={billing} timezone="UTC" {...handlers} {...props} />);
  return { ...handlers, unmount };
}
const row = (label: string) => screen.getByText(label, { selector: 'dt' }).closest('.billing-row') as HTMLElement;
const founding = paidBilling('founding', 10, 20, { purchased_plan: 'founding' });
const team25 = paidBilling('team25', 25, 250, { purchased_plan: 'team25_monthly', current_period_end: '2026-11-30T09:00:00Z', usage: { agents: 14, pending_invites: 0, replies_last_24h: 0, storage_bytes: 3.1 * GiB } });

test('a Founding workspace reads as rows: plan and price, next charge, seats, storage, migration, billing portal', () => {
  show(founding, { migration: { requestedAt: null, done: false } });
  const labels = Array.from(document.querySelectorAll('.billing-row dt')).map((term) => term.textContent);
  expect(labels).toEqual(['Plan', 'Next charge', 'Agents', 'Attachment storage', 'Freshdesk migration', 'Payment and invoices']);
  expect(row('Plan')).toHaveTextContent('Founding · $99 every 3 months · price locked for life');
  expect(row('Next charge')).toHaveTextContent('$99 on Jan 28, 2027');
  expect(row('Agents')).toHaveTextContent('3 of 10');
  expect(within(row('Agents')).getByRole('meter', { name: 'Agents' })).toHaveAttribute('aria-valuenow', '3');
  expect(row('Attachment storage')).toHaveTextContent('0.2 of 20 GiB');
  expect(within(row('Attachment storage')).getByRole('meter', { name: 'Attachment storage' })).toHaveAttribute('aria-valuemax', '20');
  expect(row('Payment and invoices')).toHaveTextContent('Card, invoices and receipts are in the billing portal');
});

test('seats count pending invites too, and say so', () => {
  show({ ...founding, usage: { ...founding.usage, agents: 8, pending_invites: 2 } });
  expect(row('Agents')).toHaveTextContent('8 of 10, plus 2 pending invites');
  expect(within(row('Agents')).getByRole('meter')).toHaveAttribute('aria-valuenow', '10');
  expect(within(row('Agents')).getByRole('meter')).toHaveAttribute('data-level', 'high');
});

test('Invite people leads to the invites of the team settings', () => {
  show(founding);
  expect(within(row('Agents')).getByRole('link', { name: 'Invite people' })).toHaveAttribute('href', '/settings#invites-heading');
});

test('before the migration is asked for, its one button is the only ink button on the page', () => {
  const { onRequestMigration } = show(founding, { migration: { requestedAt: null, done: false } });
  expect(row('Freshdesk migration')).toHaveTextContent('Not requested');
  const send = within(row('Freshdesk migration')).getByRole('button', { name: 'Send your Freshdesk address' });
  expect(document.querySelectorAll('.primary-action')).toHaveLength(1);
  expect(send).toHaveClass('primary-action');
  fireEvent.click(send);
  expect(onRequestMigration).toHaveBeenCalledTimes(1);
});

test('while a dialog is open over the page its button steps back so the dialog holds the only ink button', () => {
  show(founding, { migration: { requestedAt: null, done: false }, dialogOpen: true });
  expect(document.querySelectorAll('.primary-action')).toHaveLength(0);
  expect(screen.getByRole('button', { name: 'Send your Freshdesk address' })).toBeEnabled();
});

test('a requested migration shows its date and the promise of an answer, with no button', () => {
  show(team25, { migration: { requestedAt: '2026-10-30T09:00:00Z', done: false } });
  expect(row('Freshdesk migration')).toHaveTextContent('Requested Oct 30');
  expect(row('Freshdesk migration')).toHaveTextContent('We reply by email within one working day.');
  expect(within(row('Freshdesk migration')).queryByRole('button')).not.toBeInTheDocument();
  expect(document.querySelectorAll('.primary-action')).toHaveLength(0);
});

test('a finished Freshdesk import reads Done', () => {
  show(founding, { migration: { requestedAt: '2026-10-30T09:00:00Z', done: true } });
  expect(row('Freshdesk migration')).toHaveTextContent('Done');
  expect(row('Freshdesk migration')).not.toHaveTextContent('Requested');
});

test('the Plan row states the price of the plan it was bought as, with the charge it makes on the date it falls', () => {
  const cases: [Billing['plan'], NonNullable<Billing['purchased_plan']>, string, string][] = [
    ['team25', 'team25_monthly', 'Team 25 · $149 a month', '$149 on Nov 30, 2026'],
    ['team25', 'team25_yearly', 'Team 25 · $1,490 a year', '$1,490 on Nov 30, 2026'],
    ['founding', 'founding', 'Founding · $99 every 3 months', '$99 on Nov 30, 2026'],
    ['team', 'team_monthly', 'Team · $69 a month', '$69 on Nov 30, 2026'],
    ['team', 'team_yearly', 'Team · $690 a year', '$690 on Nov 30, 2026'],
    ['small', 'small_monthly', 'Small · $39 a month', '$39 on Nov 30, 2026'],
    ['solo', 'solo_yearly', 'Solo · $190 a year', '$190 on Nov 30, 2026'],
  ];
  for (const [plan, purchased, planText, chargeText] of cases) {
    const { unmount } = show(paidBilling(plan, 10, 100, { purchased_plan: purchased, current_period_end: '2026-11-30T09:00:00Z' }), { migration: undefined });
    expect(row('Plan'), purchased).toHaveTextContent(planText);
    expect(row('Next charge'), purchased).toHaveTextContent(chargeText);
    unmount();
  }
});

test('a yearly Team 25 never reads as monthly, and a monthly one never as yearly', () => {
  const { unmount } = show({ ...team25, purchased_plan: 'team25_yearly' });
  expect(row('Plan')).not.toHaveTextContent('a month');
  expect(row('Plan')).not.toHaveTextContent('$149');
  unmount();
  show(team25);
  expect(row('Plan')).not.toHaveTextContent('a year');
  expect(row('Plan')).not.toHaveTextContent('$1,490');
});

test('a paid plan whose price the server does not know names the plan and the charge date, and no price', () => {
  for (const unknown of [team25, founding]) {
    const { unmount } = show({ ...unknown, purchased_plan: null });
    expect(row('Plan')).toHaveTextContent(unknown.plan === 'team25' ? 'Team 25' : 'Founding');
    expect(row('Plan')).not.toHaveTextContent('$');
    expect(row('Next charge')).not.toHaveTextContent('$');
    expect(row('Next charge')).toHaveTextContent(/[A-Z][a-z]{2} \d{1,2}, 20\d\d/);
    unmount();
  }
});

test('a plan with no migration promised, such as Team, has no migration row', () => {
  show(paidBilling('team', 10, 100), { migration: undefined });
  expect(screen.queryByText('Freshdesk migration')).not.toBeInTheDocument();
});

test('cancel is a quiet text link under the rows that says how long access lasts, and opens the portal', () => {
  const { onManage } = show(founding);
  expect(screen.getByText('You keep access until Jan 28, 2027.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel plan' }));
  fireEvent.click(screen.getByRole('button', { name: /open billing portal/i }));
  expect(onManage).toHaveBeenCalledTimes(2);
  expect(screen.getByRole('button', { name: 'Cancel plan' })).toHaveClass('link-button');
});

test('a cancelled plan has no next charge and says when it ends instead of offering to cancel again', () => {
  show({ ...founding, status: 'canceled', cancel_at_period_end: true });
  expect(row('Next charge')).toHaveTextContent('None. Your plan ends Jan 28, 2027.');
  expect(screen.queryByRole('button', { name: 'Cancel plan' })).not.toBeInTheDocument();
  expect(screen.getByText('Cancels Jan 28, 2027. You keep the plan until then.')).toBeInTheDocument();
});

test('a complimentary plan shows its seats and storage and nothing to buy, charge, move or cancel', () => {
  show(paidBilling('team', 10, 100, { complimentary: true, status: 'active', current_period_end: null }), { migration: undefined });
  expect(row('Plan')).toHaveTextContent('Complimentary Team plan · no charge');
  const labels = Array.from(document.querySelectorAll('.billing-row dt')).map((term) => term.textContent);
  expect(labels).toEqual(['Plan', 'Agents', 'Attachment storage']);
  expect(screen.queryByRole('button', { name: /cancel|portal|send your freshdesk/i })).not.toBeInTheDocument();
  expect(document.querySelectorAll('.primary-action')).toHaveLength(0);
});

test('a complimentary plan with an end says when it ends', () => {
  show(paidBilling('team', 10, 100, { complimentary: true, complimentary_until: '2027-03-01T00:00:00Z', current_period_end: '2027-03-01T00:00:00Z' }));
  expect(row('Plan')).toHaveTextContent('Complimentary Team plan · no charge · until Mar 1, 2027');
  // A period end on a complimentary plan is not a charge: there is nothing to cancel and no portal to open.
  expect(screen.queryByRole('button', { name: 'Cancel plan' })).not.toBeInTheDocument();
  expect(screen.queryByText('Next charge')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /open billing portal/i })).not.toBeInTheDocument();
});
