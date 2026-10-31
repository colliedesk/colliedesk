import type { Billing } from '../../api/schemas';

// Billing bodies the page tests share; each test changes only what it is about.
export const GiB = 1024 ** 3;

export const trialBilling: Billing = {
  plan: 'trial', trial_ends_at: '2026-11-13T09:00:00Z', locked: false, status: 'none', current_period_end: null, cancel_at_period_end: false,
  active_subscriptions: 0, complimentary: false, complimentary_until: null, checkout_refusals: [], storage_upgrade_plan: null, migration_requested_at: null,
  checkout_plans: ['team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 25, replies_per_24h: 200, storage_bytes: 2 * GiB, storage_warn_bytes: 1.6 * GiB },
  usage: { agents: 3, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0.2 * GiB },
};

// paidBilling is a workspace that pays for a plan with the given agents and attachment storage.
export const paidBilling = (plan: Billing['plan'], agents: number, storageGiB: number, patch: Partial<Billing> = {}): Billing => ({
  ...trialBilling, plan, status: 'active', active_subscriptions: 1, current_period_end: '2027-01-28T09:00:00Z', checkout_plans: [],
  limits: { agents, replies_per_24h: null, storage_bytes: storageGiB * GiB, storage_warn_bytes: storageGiB * GiB * 0.8 },
  usage: { agents: 3, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0.2 * GiB }, ...patch,
});
