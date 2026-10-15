import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { LockedScreen } from './locked-screen';

const ended: Billing = {
  plan: 'none', trial_ends_at: '2026-10-15T09:00:00Z', locked: true, status: 'none', current_period_end: null, cancel_at_period_end: false,
  active_subscriptions: 0, complimentary: false, complimentary_until: null,
  limits: { agents: 0, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, usage: { agents: 2, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};
const exportSlot = <button type="button">Export all data</button>;

test('an admin whose trial ended can choose each plan, export and sign out', () => {
  const onCheckout = vi.fn();
  const onSignOut = vi.fn();
  render(<LockedScreen isAdmin billing={ended} timezone="UTC" exportAction={exportSlot} onCheckout={onCheckout} onSignOut={onSignOut} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Your free trial has ended' })).toBeInTheDocument();
  expect(screen.getByText(/ended oct 15/i)).toBeInTheDocument();
  expect(screen.getByRole('heading', { name: 'Choose a plan' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: /team, \$49 per month/i }));
  fireEvent.click(screen.getByRole('button', { name: /team, \$490 per year/i }));
  fireEvent.click(screen.getByRole('button', { name: /founding/i }));
  expect(onCheckout.mock.calls).toEqual([['team_monthly'], ['team_yearly'], ['founding']]);
  expect(screen.getByRole('button', { name: 'Export all data' })).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Sign out' }));
  expect(onSignOut).toHaveBeenCalledTimes(1);
});

test('an admin of a workspace whose subscription ended is told that, not "trial"', () => {
  render(<LockedScreen isAdmin billing={{ ...ended, status: 'canceled', current_period_end: '2026-10-01T00:00:00Z' }} timezone="UTC"
    exportAction={exportSlot} onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.getByRole('heading', { level: 1, name: 'Your subscription has ended' })).toBeInTheDocument();
});

test('an agent is sent to a workspace admin and gets no plan or export controls', () => {
  render(<LockedScreen isAdmin={false} timezone="UTC" onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.getByRole('heading', { level: 1, name: 'This workspace is locked' })).toBeInTheDocument();
  expect(screen.getByText(/ask a workspace admin to choose a plan/i)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /team|founding/i })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /export/i })).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Sign out' })).toBeInTheDocument();
});

test('a failed checkout is said aloud', () => {
  render(<LockedScreen isAdmin billing={ended} timezone="UTC" checkoutFailed exportAction={exportSlot} onCheckout={vi.fn()} onSignOut={vi.fn()} />);
  expect(screen.getByRole('alert')).toHaveTextContent(/billing action failed/i);
});
