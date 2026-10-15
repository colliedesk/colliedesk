import { render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { BillingPage } from './billing-page';

const GiB = 1024 ** 3;
const founding = (used: number): Billing => ({
  plan: 'founding', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'active', current_period_end: '2027-10-01T00:00:00Z',
  cancel_at_period_end: false, active_subscriptions: 1, complimentary: false, complimentary_until: null,
  limits: { agents: 10, storage_bytes: 20 * GiB, storage_warn_bytes: 16 * GiB },
  usage: { agents: 2, pending_invites: 0, replies_last_24h: 0, storage_bytes: used },
});

function show(billing: Billing) {
  render(<BillingPage billing={billing} timezone="UTC" onCheckout={vi.fn()} onManage={vi.fn()} />);
}

test('a founding workspace sees its attachment usage against the 20 GiB cap', () => {
  show(founding(5 * GiB));
  expect(screen.getByText('5 GiB of 20 GiB attachment storage used')).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

test('at 80% of the cap the page warns and says how to avoid the limit', () => {
  show(founding(16 * GiB));
  const warning = screen.getByRole('status', { name: /storage/i });
  expect(warning).toHaveTextContent(/80%/);
  expect(warning).toHaveTextContent(/team/i);
});

test('over the cap the page says uploads and imports are refused, mail is still stored and nothing is deleted', () => {
  show(founding(20 * GiB + 5));
  const warning = screen.getByRole('alert', { name: /storage/i });
  expect(warning).toHaveTextContent(/new uploads and imports are refused/i);
  expect(warning).toHaveTextContent(/customer email is still stored/i);
  expect(warning).toHaveTextContent(/nothing is deleted/i);
  expect(warning).toHaveTextContent(/team/i);
});

test('a trial workspace is warned at 80% of its 2 GiB too', () => {
  show({ ...founding(1.7 * GiB), plan: 'trial', status: 'none', current_period_end: null, active_subscriptions: 0,
    limits: { agents: 10, storage_bytes: 2 * GiB, storage_warn_bytes: Math.floor(1.6 * GiB) } });
  expect(screen.getByText(/of 2 GiB attachment storage used/)).toBeInTheDocument();
  expect(screen.getByRole('status', { name: /storage/i })).toBeInTheDocument();
});

test('a team workspace has no storage line and no warning', () => {
  show({ ...founding(50 * GiB), plan: 'team', limits: { agents: 10, storage_bytes: null, storage_warn_bytes: null } });
  expect(screen.queryByText(/attachment storage/i)).not.toBeInTheDocument();
  expect(screen.queryByRole('status', { name: /storage/i })).not.toBeInTheDocument();
});
