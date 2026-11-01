import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { BillingPage } from './billing-page';
import { paidBilling, trialBilling } from './billing-fixtures';

const request = vi.fn().mockResolvedValue({ freshdesk_address: 'a.freshdesk.com', times: '', requested_at: '2026-10-30T09:00:00Z' });
function show(billing: Billing, props: Partial<Parameters<typeof BillingPage>[0]> = {}) {
  const handlers = { onCheckout: vi.fn(), onManage: vi.fn(), onRequestMigration: request };
  const view = render(<BillingPage billing={billing} timezone="UTC" {...handlers} {...props} />);
  return { ...handlers, ...view };
}
const founding = paidBilling('founding', 10, 20);
const primaries = () => document.querySelectorAll('.primary-action');

test('a trial workspace opens with its trial in one line and then the plans, Founding first for a small team', () => {
  show(trialBilling, { placesLeft: 10 });
  expect(screen.getByRole('heading', { level: 1, name: 'Plan and billing' })).toBeInTheDocument();
  expect(document.querySelector('.billing-trial-line')).toHaveTextContent('Free trial·ends Nov 13, 2026·3 of 25 agents·0.2 of 2 GiB');
  expect(screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'))).toEqual(['Founding', 'Team 25']);
  expect(screen.queryByRole('button', { name: /manage billing|cancel plan/i })).not.toBeInTheDocument();
  expect(primaries()).toHaveLength(1);
});

test('the agents of the trial line count pending invites, which hold a seat', () => {
  show({ ...trialBilling, usage: { ...trialBilling.usage, agents: 3, pending_invites: 2 } });
  expect(document.querySelector('.billing-trial-line')).toHaveTextContent('5 of 25 agents');
});

test('choosing a plan hands its checkout plan to the page', () => {
  const { onCheckout } = show(trialBilling);
  fireEvent.click(screen.getByRole('button', { name: 'Choose Founding, $99 per quarter for life' }));
  expect(onCheckout).toHaveBeenCalledWith('founding');
});

test('a server without the optional products offers only the plans it can sell', () => {
  show({ ...trialBilling, checkout_plans: ['team_monthly', 'team_yearly'] });
  expect(screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'))).toEqual(['Team']);
});

test('a plan the workspace does not fit stays visible, cannot be chosen and says why', () => {
  show({ ...trialBilling, usage: { ...trialBilling.usage, agents: 12 }, checkout_refusals: [{ plan: 'founding', reason: 'Founding allows up to 10 agents.' }] });
  expect(screen.getByRole('article', { name: 'Founding' })).toHaveAttribute('data-fit', 'no');
  expect(screen.getByRole('button', { name: 'Choose Founding, $99 per quarter for life' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Choose Team 25, $149 per month' })).toBeEnabled();
});

test('the Freshdesk agent count the route read reaches the plan that fits', () => {
  show(trialBilling, { freshdeskAgents: 14 });
  expect(screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'))).toEqual(['Team 25', 'Founding']);
  expect(screen.getByText('Your Freshdesk has 14 agents.')).toBeInTheDocument();
});

test('a visitor who came for Team 25 finds it first and marked, and nothing is bought for them', () => {
  const { onCheckout } = show(trialBilling, { preferredPlan: 'team25' });
  expect(screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'))).toEqual(['Team 25', 'Founding']);
  expect(within(screen.getByRole('article', { name: 'Team 25' })).getByText('Your pick')).toBeInTheDocument();
  expect(onCheckout).not.toHaveBeenCalled();
});

test('a checkout that did not open is said under the plans, and Try again starts the same checkout again', () => {
  const onRetryCheckout = vi.fn();
  show(trialBilling, { checkoutFailed: true, onRetryCheckout });
  expect(screen.getByRole('alert')).toHaveTextContent('Checkout did not open. Nothing was charged.');
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(onRetryCheckout).toHaveBeenCalledTimes(1);
});

test('a refusal the server explained is shown in its own words', () => {
  show(trialBilling, { checkoutFailed: 'Polar did not accept your address.' });
  expect(screen.getByRole('alert')).toHaveTextContent('Polar did not accept your address.');
  expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
});

test('while a checkout opens, the plan being bought says so', () => {
  show(trialBilling, { starting: 'founding' });
  expect(screen.getByRole('button', { name: 'Choose Founding, $99 per quarter for life' })).toHaveTextContent('Opening checkout…');
});

test('a failed portal call is said aloud', () => {
  show(founding, { portalFailed: true, importDone: false });
  expect(screen.getByRole('alert')).toHaveTextContent(/billing action failed/i);
});

test('a failed payment is flagged while the plan stays active', () => {
  show({ ...founding, status: 'past_due' }, { importDone: false });
  expect(screen.getByRole('alert')).toHaveTextContent(/last payment failed/i);
  expect(screen.getByText('Plan', { selector: 'dt' })).toBeInTheDocument();
});

test('two active subscriptions are flagged so the admin cancels the extra one', () => {
  show({ ...founding, active_subscriptions: 2 });
  expect(screen.getByRole('alert')).toHaveTextContent(/paying twice/i);
});

test('a workspace with more agents than its plan tells the admin who is paused and what to do', () => {
  show({ ...paidBilling('small', 3, 30), usage: { agents: 5, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 } });
  expect(screen.getByRole('alert', { name: 'Agents' })).toHaveTextContent(/2 agents more than the Small plan allows/);
});

test('with billing switched off the plan and the portal stay and nothing is offered for sale', () => {
  show(founding, { switchedOff: true, importDone: false });
  expect(screen.getByRole('note')).toHaveTextContent(/billing is turned off on this server/i);
  expect(screen.getByRole('button', { name: /open billing portal/i })).toBeInTheDocument();
  show({ ...trialBilling, status: 'canceled' }, { switchedOff: true });
  expect(screen.queryByRole('article')).not.toBeInTheDocument();
});

test('a workspace whose trial ended reads Trial ended in the same line and still offers the plans', () => {
  show({ ...trialBilling, plan: 'none', locked: true });
  expect(document.querySelector('.billing-trial-line')).toHaveTextContent('Free trial·ended Nov 13, 2026');
  expect(screen.getAllByRole('article')).toHaveLength(2);
});
