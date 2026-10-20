import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { PlanBanner } from './plan-banner';

const trialEnd = '2026-10-15T09:00:00Z';
const trial = { plan: 'trial' as const, trial_ends_at: trialEnd, locked: false, trial_extended: false, agents_over: 0, seat_paused: false };
const at = (iso: string) => new Date(iso);
const GiB = 1024 ** 3;
const billing = (storageBytes: number): Billing => ({
  plan: 'trial', trial_ends_at: trialEnd, locked: false, status: 'none', current_period_end: null, cancel_at_period_end: false,
  active_subscriptions: 0, complimentary: false, complimentary_until: null, checkout_refusals: [], storage_upgrade_plan: null, checkout_plans: ['team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 10, replies_per_24h: 200, storage_bytes: 2 * GiB, storage_warn_bytes: Math.floor(1.6 * GiB) },
  usage: { agents: 1, pending_invites: 0, replies_last_24h: 0, storage_bytes: storageBytes },
});

test('three days before the end the banner counts three days and an admin can choose a plan', () => {
  const choose = vi.fn();
  render(<PlanBanner plan={trial} isAdmin now={at('2026-10-12T09:00:00Z')} onChoosePlan={choose} />);
  expect(screen.getByRole('region', { name: /plan notices/i })).toHaveTextContent('Trial: 3 days left');
  fireEvent.click(screen.getByRole('button', { name: 'Choose a plan' }));
  expect(choose).toHaveBeenCalledTimes(1);
});

test('one hour short of a day still counts as one day, never zero', () => {
  render(<PlanBanner plan={trial} isAdmin now={at('2026-10-15T08:00:00Z')} onChoosePlan={vi.fn()} />);
  expect(screen.getByText('Trial: 1 day left')).toBeInTheDocument();
});

test('an agent sees the days but has no plan button', () => {
  render(<PlanBanner plan={trial} isAdmin={false} now={at('2026-10-12T09:00:00Z')} onChoosePlan={vi.fn()} />);
  expect(screen.getByText('Trial: 3 days left')).toBeInTheDocument();
  expect(screen.queryByRole('button')).not.toBeInTheDocument();
});

test('an extended trial says so, however many days are left', () => {
  const { rerender } = render(<PlanBanner plan={{ ...trial, trial_ends_at: '2026-10-20T09:00:00Z', trial_extended: true, agents_over: 0, seat_paused: false }} isAdmin now={at('2026-10-15T09:00:00Z')} onChoosePlan={vi.fn()} />);
  expect(screen.getByText('Trial: 5 days left')).toBeInTheDocument();
  expect(screen.getByText(/was extended/i)).toBeInTheDocument();
  rerender(<PlanBanner plan={{ ...trial, trial_ends_at: '2026-11-14T09:00:00Z' }} isAdmin now={at('2026-10-15T09:00:00Z')} onChoosePlan={vi.fn()} />);
  expect(screen.getByText('Trial: 30 days left')).toBeInTheDocument();
  expect(screen.queryByText(/extended/i)).not.toBeInTheDocument();
});

test('a browser clock a little behind the server does not turn a fresh trial into 15 days', () => {
  render(<PlanBanner plan={{ ...trial, trial_ends_at: '2026-10-29T09:00:00Z' }} isAdmin now={at('2026-10-15T08:59:30Z')} onChoosePlan={vi.fn()} />);
  expect(screen.getByText('Trial: 14 days left')).toBeInTheDocument();
});

test('storage at 85 percent warns the admin; 75 percent does not', () => {
  const { rerender } = render(<PlanBanner plan={trial} isAdmin now={at('2026-10-12T09:00:00Z')} billing={billing(1.7 * GiB)} onChoosePlan={vi.fn()} />);
  expect(screen.getByRole('status')).toHaveTextContent(/storage is 85% full/i);
  rerender(<PlanBanner plan={trial} isAdmin now={at('2026-10-12T09:00:00Z')} billing={billing(1.5 * GiB)} onChoosePlan={vi.fn()} />);
  expect(screen.queryByText(/storage is/i)).not.toBeInTheDocument();
});

test('a paid or self-hosted workspace shows nothing', () => {
  const { container, rerender } = render(<PlanBanner plan={{ plan: 'team', trial_ends_at: trialEnd, locked: false, trial_extended: false, agents_over: 0, seat_paused: false }} isAdmin onChoosePlan={vi.fn()} />);
  expect(container).toBeEmptyDOMElement();
  rerender(<PlanBanner plan={null} isAdmin onChoosePlan={vi.fn()} />);
  expect(container).toBeEmptyDOMElement();
});

test('an admin of a workspace over its agent cap is told on every page; an agent is not', () => {
  const over = { plan: 'small' as const, trial_ends_at: trialEnd, locked: false, trial_extended: false, agents_over: 2, seat_paused: false };
  const { container, rerender } = render(<PlanBanner plan={over} isAdmin />);
  expect(screen.getByRole('alert')).toHaveTextContent('This workspace has 2 agents more than the Small plan allows. The 2 most recently added cannot work until you deactivate agents in Settings > Team or upgrade under Manage billing.');
  rerender(<PlanBanner plan={over} isAdmin={false} />);
  expect(container).toBeEmptyDOMElement();
});
