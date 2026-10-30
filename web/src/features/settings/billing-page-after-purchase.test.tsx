import { render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { BillingPage } from './billing-page';

const GiB = 1024 ** 3;
const trial: Billing = {
  plan: 'trial', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'none', current_period_end: null, cancel_at_period_end: false, active_subscriptions: 0, complimentary: false, complimentary_until: null, checkout_refusals: [], storage_upgrade_plan: null,
  checkout_plans: ['team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 25, replies_per_24h: 200, storage_bytes: 2 * GiB, storage_warn_bytes: 1.6 * GiB }, usage: { agents: 1, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};
const paid = (plan: Billing['plan'], agents: number, storageGiB: number): Billing => ({
  ...trial, plan, status: 'active', active_subscriptions: 1, current_period_end: '2027-01-30T09:00:00Z', checkout_plans: [],
  limits: { agents, replies_per_24h: null, storage_bytes: storageGiB * GiB, storage_warn_bytes: storageGiB * GiB * 0.8 },
});
const founding = paid('founding', 10, 20);
const team25 = paid('team25', 25, 250);

function show(billing: Billing, props: Partial<Parameters<typeof BillingPage>[0]> = {}) {
  return render(<BillingPage billing={billing} timezone="UTC" workspaceName="Birch Desk" importDone={false} onCheckout={vi.fn()} onManage={vi.fn()} {...props} />);
}
const migrationCard = () => screen.queryByRole('region', { name: 'Next: your Freshdesk migration' });

test('a paid Founding workspace is told its migration is included and can email us its Freshdesk address', () => {
  show(founding);
  const card = within(screen.getByRole('region', { name: 'Next: your Freshdesk migration' }));
  expect(card.getByText('Your plan includes a Freshdesk migration done for you.')).toBeInTheDocument();
  expect(card.queryByText('We run the Freshdesk import with you.')).not.toBeInTheDocument();
  const mail = new URL(card.getByRole('link', { name: 'Email us your Freshdesk address' }).getAttribute('href') ?? '');
  expect(mail.protocol).toBe('mailto:');
  expect(mail.pathname).toBe('support@colliedesk.com');
  expect(mail.searchParams.get('subject')).toBe('Migration: Birch Desk');
  // A mail link breaks lines with CRLF (RFC 6068).
  expect(mail.searchParams.get('body')).toBe('Hello,\r\n\r\nMy Freshdesk address is: \r\n\r\nTwo times that suit me for the migration, with my time zone:\r\n1. \r\n2. \r\n');
  expect(card.getByRole('link', { name: 'Or import it yourself' })).toHaveAttribute('href', '/settings/import-freshdesk');
});

test('a paid Team 25 workspace is told we run the import with it', () => {
  show(team25);
  const card = within(screen.getByRole('region', { name: 'Next: your Freshdesk migration' }));
  expect(card.getByText('We run the Freshdesk import with you.')).toBeInTheDocument();
  expect(card.queryByText(/done for you/i)).not.toBeInTheDocument();
  expect(card.getByRole('link', { name: 'Email us your Freshdesk address' })).toBeInTheDocument();
  expect(card.getByRole('link', { name: 'Or import it yourself' })).toBeInTheDocument();
});

test('the subject keeps a workspace name with punctuation intact', () => {
  show(founding, { workspaceName: 'Acme & Sons, 50% Off' });
  const mail = new URL(screen.getByRole('link', { name: 'Email us your Freshdesk address' }).getAttribute('href') ?? '');
  expect(mail.searchParams.get('subject')).toBe('Migration: Acme & Sons, 50% Off');
});

test.each([
  ['a trial', trial],
  ['a locked workspace', { ...trial, plan: 'none', locked: true } as Billing],
  ['a Team workspace', paid('team', 10, 100)],
  ['a Solo workspace', paid('solo', 1, 10)],
  ['a complimentary Founding workspace', { ...founding, complimentary: true }],
])('%s has no migration card', (_name, billing) => {
  show(billing);
  expect(migrationCard()).not.toBeInTheDocument();
});

test('the card is gone once the workspace has a finished Freshdesk import, and is not shown while that is still unknown', () => {
  const { rerender } = show(founding, { importDone: true });
  expect(migrationCard()).not.toBeInTheDocument();
  rerender(<BillingPage billing={team25} timezone="UTC" workspaceName="Birch Desk" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(migrationCard()).not.toBeInTheDocument();
  rerender(<BillingPage billing={team25} timezone="UTC" workspaceName="Birch Desk" importDone={false} onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(migrationCard()).toBeInTheDocument();
});

test('right after checkout the paid page welcomes the buyer with the migration card above the plan details', () => {
  show(founding, { checkoutReturned: true });
  expect(screen.getByRole('status')).toHaveTextContent('Payment confirmed. Welcome to Founding.');
  const card = screen.getByRole('region', { name: 'Next: your Freshdesk migration' });
  const plan = screen.getByRole('region', { name: 'Current plan' });
  expect(card.compareDocumentPosition(plan) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

test('on a later visit the migration card follows the plan details and nothing welcomes the buyer', () => {
  show(founding);
  const card = screen.getByRole('region', { name: 'Next: your Freshdesk migration' });
  const plan = screen.getByRole('region', { name: 'Current plan' });
  expect(plan.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

test('while the payment is being confirmed every plan button is off and the page says so', () => {
  show(trial, { checkoutReturned: true });
  expect(screen.getByRole('status')).toHaveTextContent('Payment received. We are confirming your plan, which takes a few moments. The plan buttons stay off until then, so you cannot pay twice.');
  const buttons = screen.getAllByRole('button', { name: /^Choose / });
  expect(buttons.length).toBeGreaterThan(1);
  for (const button of buttons) expect(button).toBeDisabled();
  expect(migrationCard()).not.toBeInTheDocument();
});

test('a locked workspace that just paid waits the same way', () => {
  show({ ...trial, plan: 'none', locked: true }, { checkoutReturned: true });
  expect(screen.getByRole('status')).toHaveTextContent(/confirming your plan/i);
  for (const button of screen.getAllByRole('button', { name: /^Choose / })) expect(button).toBeDisabled();
});

test('the same trial page without a checkout return keeps its plan buttons on', () => {
  show(trial);
  for (const button of screen.getAllByRole('button', { name: /^Choose / })) expect(button).toBeEnabled();
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

test('once the plan is paid the confirmation names the plan and no plan buttons remain', () => {
  show(team25, { checkoutReturned: true });
  expect(screen.getByRole('status')).toHaveTextContent('Payment confirmed. Welcome to Team 25.');
  expect(screen.queryByRole('button', { name: /^Choose / })).not.toBeInTheDocument();
  expect(screen.queryByText(/confirming your plan/i)).not.toBeInTheDocument();
});
