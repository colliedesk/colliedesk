import { render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { BillingPage } from './billing-page';

const GiB = 1024 ** 3;
const founding = (used: number): Billing => ({
  plan: 'founding', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'active', current_period_end: '2027-10-01T00:00:00Z',
  cancel_at_period_end: false, active_subscriptions: 1, complimentary: false, complimentary_until: null, checkout_plans: ['solo_monthly', 'solo_yearly', 'small_monthly', 'small_yearly', 'team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 10, replies_per_24h: null, storage_bytes: 20 * GiB, storage_warn_bytes: 16 * GiB },
  usage: { agents: 2, pending_invites: 0, replies_last_24h: 0, storage_bytes: used },
});

function show(billing: Billing) {
  render(<BillingPage billing={billing} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
}

test('a founding workspace sees its attachment usage against the 20 GiB cap', () => {
  show(founding(5 * GiB));
  expect(screen.getByText('Attachment storage: 5.0 GiB of 20.0 GiB')).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('at 80% of the cap the page warns, and at the cap the warning gives way to the full notice', () => {
  show(founding(16 * GiB));
  expect(screen.getByRole('status')).toHaveTextContent(/80% full/i);
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('at the cap only the full notice shows, not the 80% warning too', () => {
  show(founding(20 * GiB));
  expect(screen.getByRole('alert', { name: /storage/i })).toBeInTheDocument();
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

test('over the cap the page says uploads and imports are refused, mail is still stored and nothing is deleted', () => {
  show(founding(20 * GiB + 5));
  const warning = screen.getByRole('alert', { name: /storage/i });
  expect(warning).toHaveTextContent(/new uploads and imports are refused/i);
  expect(warning).toHaveTextContent(/customer email is still stored/i);
  expect(warning).toHaveTextContent(/nothing is deleted/i);
  expect(warning).toHaveTextContent(/team/i);
});

test('Solo and Small at or over the cap are pointed to the next plan up', () => {
  const small = (plan: 'solo' | 'small', limit: number, used: number): Billing => ({ ...founding(used), plan,
    limits: { agents: plan === 'solo' ? 1 : 3, replies_per_24h: null, storage_bytes: limit, storage_warn_bytes: limit * 0.8 } });
  const { unmount } = render(<BillingPage billing={small('solo', 10 * GiB, 10 * GiB)} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
  expect(screen.getByRole('alert', { name: /storage/i })).toHaveTextContent('Upgrade to the Small plan under Manage billing for a higher limit.');
  unmount();
  show(small('small', 30 * GiB, 24 * GiB));
  expect(screen.getByRole('status')).toHaveTextContent('To raise the limit, upgrade to Team under Manage billing.');
});

test('a trial workspace is warned at 80% of its 2 GiB too', () => {
  show({ ...founding(1.7 * GiB), plan: 'trial', status: 'none', current_period_end: null, active_subscriptions: 0,
    limits: { agents: 10, replies_per_24h: null, storage_bytes: 2 * GiB, storage_warn_bytes: Math.floor(1.6 * GiB) } });
  expect(screen.getByText(/Attachment storage: 1\.7 GiB of 2\.0 GiB/)).toBeInTheDocument();
  expect(screen.getByRole('status')).toBeInTheDocument();
});

const teamPlan = (plan: 'team' | 'team25', used: number): Billing => ({
  ...founding(used), plan,
  limits: plan === 'team'
    ? { agents: 10, replies_per_24h: null, storage_bytes: 100 * GiB, storage_warn_bytes: 80 * GiB }
    : { agents: 25, replies_per_24h: null, storage_bytes: 250 * GiB, storage_warn_bytes: 200 * GiB },
});

test('a team workspace sees its usage against the 100 GiB cap and no warning below 80%', () => {
  show(teamPlan('team', 50 * GiB));
  expect(screen.getByText('Attachment storage: 50.0 GiB of 100.0 GiB')).toBeInTheDocument();
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('a team workspace at 80% is warned that only its own uploads and imports will stop', () => {
  show(teamPlan('team', 80 * GiB));
  const warning = screen.getByRole('status');
  expect(warning).toHaveTextContent(/80% full/i);
  expect(warning).toHaveTextContent(/customer email is always kept/i);
  expect(warning).not.toHaveTextContent(/upgrade to team/i);
});

test('a team 25 workspace over its 250 GiB cap hears that mail is stored and who to ask for more', () => {
  show(teamPlan('team25', 250 * GiB + 5));
  const notice = screen.getByRole('alert', { name: /storage/i });
  expect(notice).toHaveTextContent(/new uploads and imports are refused/i);
  expect(notice).toHaveTextContent(/customer email is still stored/i);
  expect(notice).toHaveTextContent(/contact us/i);
  expect(notice).not.toHaveTextContent(/all incoming email is refused/i);
});

test('a full trial says incoming email with attachments is refused, not stored', () => {
  const trial: Billing = { ...founding(2 * GiB), plan: 'trial', status: 'none', current_period_end: null, active_subscriptions: 0,
    limits: { agents: 10, replies_per_24h: null, storage_bytes: 2 * GiB, storage_warn_bytes: Math.floor(1.6 * GiB) } };
  show(trial);
  const warning = screen.getByRole('alert', { name: /storage/i });
  expect(warning).toHaveTextContent(/all incoming email is refused/i);
  expect(warning).toHaveTextContent(/upgrade to keep receiving email/i);
  expect(warning).toHaveTextContent(/imported files do not count/i);
  expect(warning).not.toHaveTextContent(/customer email is still stored/i);
  // There is no way to delete an attachment, so the page must not tell the admin to free space.
  expect(warning).not.toHaveTextContent(/free space/i);
});

test('a locked workspace at the cap gets the trial wording too', () => {
  show({ ...founding(2 * GiB), plan: 'none', locked: true, status: 'canceled',
    limits: { agents: 0, replies_per_24h: null, storage_bytes: 2 * GiB, storage_warn_bytes: Math.floor(1.6 * GiB) } });
  expect(screen.getByRole('alert', { name: /storage/i })).toHaveTextContent(/all incoming email is refused/i);
});

test('usage just under the limit never reads as full', () => {
  show({ ...founding(2147000000), plan: 'trial', status: 'none', current_period_end: null, active_subscriptions: 0,
    limits: { agents: 10, replies_per_24h: null, storage_bytes: 2 * GiB, storage_warn_bytes: Math.floor(1.6 * GiB) } });
  expect(screen.getByText('Attachment storage: 1.9 GiB of 2.0 GiB')).toBeInTheDocument();
});

test('a founding workspace nearing the limit is told its mail is safe and how to remove the limit', () => {
  show(founding(17 * GiB));
  const warning = screen.getByRole('status');
  expect(warning).toHaveTextContent(/nearing/i);
  expect(warning).toHaveTextContent(/20\.0 GiB/);
  expect(warning).toHaveTextContent(/customer email is always kept/i);
  expect(warning).toHaveTextContent(/uploads and new imports stop/i);
  expect(warning).toHaveTextContent(/upgrade to team under manage billing/i);
  // A paying workspace is never told to choose a plan.
  expect(warning).not.toHaveTextContent(/choose a plan/i);
});

test('a trial nearing the limit keeps the plan-choosing advice', () => {
  show({ ...founding(1.7 * GiB), plan: 'trial', status: 'none', current_period_end: null, active_subscriptions: 0,
    limits: { agents: 10, replies_per_24h: null, storage_bytes: 2 * GiB, storage_warn_bytes: Math.floor(1.6 * GiB) } });
  expect(screen.getByRole('status')).toHaveTextContent(/choose a plan/i);
});
