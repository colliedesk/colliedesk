import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { Billing } from '../../api/schemas';
import { BillingPage } from './billing-page';
import { paidBilling, trialBilling } from './billing-fixtures';

const founding = paidBilling('founding', 10, 20);
const team25 = paidBilling('team25', 25, 250, { current_period_end: '2026-11-30T09:00:00Z' });
const sent = { freshdesk_address: 'northwind.freshdesk.com', times: '', requested_at: '2026-10-30T09:00:00Z' };

function show(billing: Billing, props: Partial<Parameters<typeof BillingPage>[0]> = {}) {
  const handlers = { onCheckout: vi.fn(), onManage: vi.fn(), onRequestMigration: vi.fn().mockResolvedValue(sent), onWelcomeDone: vi.fn(), onCheckAgain: vi.fn() };
  const view = render(<BillingPage billing={billing} timezone="UTC" importDone={false} {...handlers} {...props} />);
  return { ...handlers, ...view, props };
}
const rerenderWith = (view: ReturnType<typeof show>, billing: Billing, props: Partial<Parameters<typeof BillingPage>[0]> = {}) =>
  view.rerender(<BillingPage billing={billing} timezone="UTC" importDone={false} onCheckout={view.onCheckout} onManage={view.onManage} onRequestMigration={view.onRequestMigration} onWelcomeDone={view.onWelcomeDone} {...props} />);

beforeEach(() => { vi.useFakeTimers({ shouldAdvanceTime: true }); });
afterEach(() => { vi.useRealTimers(); });

test('back from the checkout, a workspace still on its trial sees the payment being confirmed in place of the plans', () => {
  show(trialBilling, { checkoutReturned: true, pendingPlan: 'founding' });
  expect(screen.getByRole('heading', { name: 'Confirming your payment' })).toBeInTheDocument();
  expect(screen.getByText('$99 every 3 months, price locked for life')).toBeInTheDocument();
  expect(screen.queryByRole('article')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /^Choose / })).not.toBeInTheDocument();
  expect(screen.queryByText(/payment received/i)).not.toBeInTheDocument();
  expect(document.querySelector('.billing-trial-line')).toBeInTheDocument();
});

test('a minute without a confirmation says so and Check again asks the page to look', () => {
  const view = show(trialBilling, { checkoutReturned: true, pendingPlan: 'founding' });
  act(() => { vi.advanceTimersByTime(60_000); });
  fireEvent.click(screen.getByRole('button', { name: 'Check again' }));
  expect(view.onCheckAgain).toHaveBeenCalledTimes(1);
});

test('a workspace that just paid and is locked waits the same way', () => {
  show({ ...trialBilling, plan: 'none', locked: true }, { checkoutReturned: true });
  expect(screen.getByRole('heading', { name: 'Confirming your payment' })).toBeInTheDocument();
});

test('when the plan flips to paid the wait gives way to the welcome, without a reload', () => {
  const view = show(trialBilling, { checkoutReturned: true, pendingPlan: 'founding', welcome: true });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  rerenderWith(view, founding, { checkoutReturned: true, pendingPlan: 'founding', welcome: true });
  expect(screen.queryByRole('heading', { name: 'Confirming your payment' })).not.toBeInTheDocument();
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getByRole('heading', { name: 'Welcome to Founding' })).toBeInTheDocument();
  expect(within(dialog).getByText('Founding · $99 every 3 months · price locked for life')).toBeInTheDocument();
  expect(within(dialog).getByText('Next charge: $99 on Jan 28, 2027')).toBeInTheDocument();
  expect(document.querySelectorAll('.primary-action')).toHaveLength(1);
});

test('the welcome of Team 25 names the plan and the charge date, and the price when this browser chose it', () => {
  show(team25, { checkoutReturned: true, welcome: true, pendingPlan: 'team25_monthly' });
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getByText('Team 25 · $149 a month')).toBeInTheDocument();
  expect(within(dialog).getByText('Next charge: $149 on Nov 30, 2026')).toBeInTheDocument();
  expect(within(dialog).getByText(/we run the import with you on a short call/i)).toBeInTheDocument();
});

test('the welcome opens only on the return from a checkout, and only until it was closed once', () => {
  const { unmount } = show(founding, { checkoutReturned: false, welcome: true });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  unmount();
  show(founding, { checkoutReturned: true, welcome: false });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('closing the welcome tells the page, so a reload does not open it again', () => {
  const view = show(founding, { checkoutReturned: true, welcome: true });
  fireEvent.click(screen.getByRole('button', { name: 'Later' }));
  expect(view.onWelcomeDone).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('a plan with no migration, such as Team, is confirmed in a line and has no welcome', () => {
  show(paidBilling('team', 10, 100), { checkoutReturned: true, welcome: true, importDone: undefined });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(screen.getByRole('status')).toHaveTextContent('Payment confirmed. Welcome to Team.');
});

test('a complimentary Founding workspace is not welcomed and has no migration', () => {
  show(paidBilling('founding', 10, 20, { complimentary: true }), { checkoutReturned: true, welcome: true });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(screen.queryByText('Freshdesk migration')).not.toBeInTheDocument();
});

test('later, Send your Freshdesk address opens the same form, and a send closes with Done and the billing row stays the page own', async () => {
  const view = show(founding);
  fireEvent.click(screen.getByRole('button', { name: 'Send your Freshdesk address' }));
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getByRole('heading', { name: 'Move your Freshdesk data' })).toBeInTheDocument();
  fireEvent.change(within(dialog).getByLabelText('Freshdesk address'), { target: { value: 'northwind.freshdesk.com' } });
  await act(async () => { fireEvent.click(within(dialog).getByRole('button', { name: 'Send to Colliedesk' })); });
  expect(view.onRequestMigration).toHaveBeenCalledWith({ freshdesk_address: 'northwind.freshdesk.com', times: '' });
  expect(await screen.findByText('Sent. We reply by email within one working day.')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Done' }));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('the migration row states are Not requested, Requested with its date, and Done', () => {
  const { unmount } = show(founding);
  expect(screen.getByText('Not requested')).toBeInTheDocument();
  unmount();
  const requested = show({ ...founding, migration_requested_at: '2026-10-30T09:00:00Z' });
  expect(screen.getByText('Requested Oct 30')).toBeInTheDocument();
  requested.unmount();
  show(founding, { importDone: true });
  expect(screen.getByText('Done')).toBeInTheDocument();
});

test.each([
  ['a trial', trialBilling],
  ['a Team workspace', paidBilling('team', 10, 100)],
  ['a Solo workspace', paidBilling('solo', 1, 10)],
  ['a complimentary Founding workspace', paidBilling('founding', 10, 20, { complimentary: true })],
])('%s has no migration row', (_name, billing) => {
  show(billing);
  expect(screen.queryByText('Freshdesk migration')).not.toBeInTheDocument();
});

test('the migration row waits while it is not known whether an import finished', () => {
  show(founding, { importDone: undefined });
  expect(screen.queryByText('Freshdesk migration')).not.toBeInTheDocument();
});
