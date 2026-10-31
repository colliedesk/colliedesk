import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { BillingRoute } from './billing-route';
import { readCheckoutIntent, rememberCheckoutIntent } from './checkout-intent';
import { readPlanIntent, rememberPlanIntent } from './plan-intent';

const { api, importsApi, InvalidRequestError } = vi.hoisted(() => ({
  api: { me: vi.fn(), config: vi.fn(), billing: vi.fn(), setupProgress: vi.fn(), startCheckout: vi.fn(), openBillingPortal: vi.fn(), foundingSlots: vi.fn(), requestMigration: vi.fn() },
  importsApi: { list: vi.fn() },
  InvalidRequestError: class InvalidRequestError extends Error {
    constructor(message: string, readonly detail?: Record<string, unknown>) { super(message); }
  },
}));
vi.mock('../../api/client', () => ({ api, InvalidRequestError }));
vi.mock('../../api/imports', () => ({ importsApi }));
// The Polar embed SDK is the only other boundary; the open-checkout module runs for real.
const embed = vi.hoisted(() => ({ create: vi.fn() }));
vi.mock('@polar-sh/checkout/embed', () => ({ PolarEmbedCheckout: { create: embed.create } }));

const me = (role: 'admin' | 'agent', admins?: string[]) => ({
  agent: { id: '11111111-1111-4111-8111-111111111111', email: 'sam@example.com', display_name: '', role, timezone_override: null },
  workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
  timezone: 'UTC',
  plan: admins ? { plan: 'trial', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, trial_extended: false, agents_over: 0, seat_paused: false, admins } : null,
});
const free = {
  plan: 'trial', trial_ends_at: '2026-10-15T09:00:00Z', locked: false, status: 'none', current_period_end: null, cancel_at_period_end: false, active_subscriptions: 0, checkout_refusals: [], storage_upgrade_plan: null, complimentary: false, complimentary_until: null, migration_requested_at: null, checkout_plans: ['solo_monthly', 'solo_yearly', 'small_monthly', 'small_yearly', 'team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly', 'founding'],
  limits: { agents: 10, replies_per_24h: 200, storage_bytes: 2147483648, storage_warn_bytes: 1717986918 }, usage: { agents: 1, pending_invites: 0, replies_last_24h: 0, storage_bytes: 0 },
};
// Once the Founding slots are taken the server stops offering it and the whole plan ladder is sold.
const foundingFull = { ...free, checkout_plans: free.checkout_plans.filter((plan) => plan !== 'founding') };
const assign = vi.fn();

function renderRoute(props: { checkoutReturned?: boolean } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><BillingRoute {...props} /></QueryClientProvider>);
  return client;
}

afterEach(() => { vi.useRealTimers(); });

beforeEach(() => {
  vi.clearAllMocks();
  window.sessionStorage.clear();
  window.localStorage.clear();
  importsApi.list.mockResolvedValue({ items: [] });
  api.foundingSlots.mockResolvedValue({ slots: 10, taken: null, open: true });
  Object.defineProperty(window, 'location', { configurable: true, value: { assign } });
  embed.create.mockResolvedValue({ addEventListener: vi.fn() });
  api.config.mockResolvedValue({ cloud_sign_in: { enabled: true, publishable_key: 'pk' }, billing: { enabled: true } });
});

test('an admin upgrades: the server-made checkout URL is opened in the overlay and nothing but the plan is sent', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(foundingFull);
  api.startCheckout.mockResolvedValue({ url: 'https://polar.example/checkout/1' });
  renderRoute();
  fireEvent.click(await screen.findByRole('radio', { name: /yearly/i }));
  fireEvent.click(screen.getByRole('button', { name: /team, \$690 per year/i }));
  await waitFor(() => expect(embed.create).toHaveBeenCalledWith('https://polar.example/checkout/1', expect.anything()));
  expect(assign).not.toHaveBeenCalled();
  expect(api.startCheckout).toHaveBeenCalledWith('team_yearly');
});

test('when the overlay cannot open, the hosted checkout is used so the purchase is not lost', async () => {
  embed.create.mockRejectedValue(new Error('script blocked'));
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(foundingFull);
  api.startCheckout.mockResolvedValue({ url: 'https://polar.example/checkout/1' });
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: /team, \$69 per month/i }));
  await waitFor(() => expect(assign).toHaveBeenCalledWith('https://polar.example/checkout/1'));
});

test('a second click while the checkout is starting does not start another', async () => {
  embed.create.mockReturnValue(new Promise(() => {}));
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(foundingFull);
  api.startCheckout.mockResolvedValue({ url: 'https://polar.example/checkout/1' });
  renderRoute();
  const button = await screen.findByRole('button', { name: /team, \$69 per month/i });
  fireEvent.click(button);
  await waitFor(() => expect(embed.create).toHaveBeenCalledTimes(1));
  expect(button).toBeDisabled();
  expect(button).toHaveAttribute('aria-busy', 'true');
  fireEvent.click(button);
  expect(api.startCheckout).toHaveBeenCalledTimes(1);
});

test('Open billing portal opens the portal URL', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue({ ...free, plan: 'team', status: 'active', current_period_end: '2026-11-15T00:00:00Z' });
  api.openBillingPortal.mockResolvedValue({ url: 'https://polar.example/portal/1' });
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: /open billing portal/i }));
  await waitFor(() => expect(assign).toHaveBeenCalledWith('https://polar.example/portal/1'));
});

test('a failed checkout is said under the plans and Try again starts the same plan again', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  api.startCheckout.mockRejectedValueOnce(new Error('API 502')).mockResolvedValue({ url: 'https://polar.example/checkout/2' });
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: /choose founding/i }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Checkout did not open. Nothing was charged.');
  expect(assign).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  await waitFor(() => expect(embed.create).toHaveBeenCalledWith('https://polar.example/checkout/2', expect.anything()));
  expect(api.startCheckout.mock.calls).toEqual([['founding'], ['founding']]);
});

test('an agent is told billing is for admins and the billing API is never called', async () => {
  api.me.mockResolvedValue(me('agent'));
  renderRoute();
  expect(await screen.findByText(/only workspace admins/i)).toBeInTheDocument();
  expect(api.billing).not.toHaveBeenCalled();
});

test('an agent in a trial workspace is told which admins can choose the plan', async () => {
  api.me.mockResolvedValue(me('agent', ['Maya Reyes', 'Daniel Kim']));
  renderRoute();
  expect(await screen.findByText('Only admins can choose a plan. Ask Maya Reyes or Daniel Kim.')).toBeInTheDocument();
  expect(api.billing).not.toHaveBeenCalled();
});

test('a self-hosted instance shows no billing and never calls the billing API', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.config.mockResolvedValue({ cloud_sign_in: { enabled: false, publishable_key: null }, billing: { enabled: false } });
  renderRoute();
  expect(await screen.findByText(/billing is not enabled/i)).toBeInTheDocument();
  expect(api.billing).not.toHaveBeenCalled();
});

test('after checkout the page says the payment is being confirmed, never that it arrived, and offers no plan to buy', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  renderRoute({ checkoutReturned: true });
  expect(await screen.findByRole('heading', { name: 'Confirming your payment' })).toBeInTheDocument();
  expect(screen.queryByText(/payment received/i)).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /^Choose / })).not.toBeInTheDocument();
});

test('a stored founding intent starts the founding checkout once and clears the intent', async () => {
  rememberCheckoutIntent('founding');
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  api.startCheckout.mockResolvedValue({ url: 'https://polar.example/checkout/founding' });
  renderRoute();
  await waitFor(() => expect(embed.create).toHaveBeenCalledWith('https://polar.example/checkout/founding', expect.anything()));
  expect(api.startCheckout).toHaveBeenCalledTimes(1);
  expect(api.startCheckout).toHaveBeenCalledWith('founding');
  expect(readCheckoutIntent()).toBeUndefined();
});

test('a remembered Team 25 plan puts its card first and marked, starts no checkout and stays remembered', async () => {
  rememberPlanIntent('team25');
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  renderRoute();
  const team25 = await screen.findByRole('article', { name: 'Team 25' });
  expect(screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'))).toEqual(['Team 25', 'Founding']);
  expect(team25).toHaveTextContent('Your pick');
  expect(api.startCheckout).not.toHaveBeenCalled();
  expect(readPlanIntent()).toBe('team25');
});

test('without a remembered plan no card is called the visitor\'s pick; the one that fits the team is marked', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  renderRoute();
  await screen.findByRole('article', { name: 'Team 25' });
  expect(screen.queryByText('Your pick')).not.toBeInTheDocument();
  expect(within(screen.getByRole('article', { name: 'Founding' })).getByText('Fits your team')).toBeInTheDocument();
});

test('the agents of the Freshdesk account the workspace read decide which plan fits, and the places left come from the server', async () => {
  const agent = (id: number) => ({ id, name: `Agent ${id}`, email: `a${id}@example.com`, active: true });
  importsApi.list.mockResolvedValue({ items: [{ kind: 'import', state: 'previewed', created_at: '2026-10-20T09:00:00Z',
    preview: { agents: Array.from({ length: 14 }, (_, index) => agent(index + 1)) } }] });
  api.foundingSlots.mockResolvedValue({ slots: 10, taken: 3, open: true });
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  renderRoute();
  await waitFor(() => expect(screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'))).toEqual(['Team 25', 'Founding']));
  expect(screen.getByText('Your Freshdesk has 14 agents.')).toBeInTheDocument();
  expect(await screen.findByText('Price locked for life · 7 places left')).toBeInTheDocument();
});

test('an intent for a plan the server does not offer is dropped and the billing page is shown', async () => {
  rememberCheckoutIntent('founding');
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue({ ...free, checkout_plans: ['team_monthly', 'team_yearly'] });
  renderRoute();
  expect(await screen.findByRole('button', { name: /team, \$69 per month/i })).toBeInTheDocument();
  expect(api.startCheckout).not.toHaveBeenCalled();
  expect(readCheckoutIntent()).toBeUndefined();
});

test('an intent is dropped when the workspace already pays', async () => {
  rememberCheckoutIntent('founding');
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue({ ...free, plan: 'team', status: 'active', current_period_end: '2026-11-15T00:00:00Z' });
  renderRoute();
  await screen.findByRole('button', { name: /open billing portal/i });
  expect(api.startCheckout).not.toHaveBeenCalled();
  expect(readCheckoutIntent()).toBeUndefined();
});

test('an intent is dropped for an agent who is not an admin', async () => {
  rememberCheckoutIntent('founding');
  api.me.mockResolvedValue(me('agent'));
  renderRoute();
  await screen.findByText(/only workspace admins/i);
  expect(api.startCheckout).not.toHaveBeenCalled();
  expect(readCheckoutIntent()).toBeUndefined();
});

test('a failed intent checkout is not retried', async () => {
  rememberCheckoutIntent('founding');
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(free);
  api.startCheckout.mockRejectedValue(new Error('API 502'));
  renderRoute();
  expect(await screen.findByRole('alert')).toHaveTextContent('Checkout did not open. Nothing was charged.');
  expect(api.startCheckout).toHaveBeenCalledTimes(1);
  expect(readCheckoutIntent()).toBeUndefined();
});

test('an intent is dropped when the billing data cannot be loaded', async () => {
  rememberCheckoutIntent('founding');
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockRejectedValue(new Error('API 500'));
  renderRoute();
  expect(await screen.findByRole('alert')).toHaveTextContent(/billing action failed/i);
  expect(api.startCheckout).not.toHaveBeenCalled();
  expect(readCheckoutIntent()).toBeUndefined();
});

test('a checkout the payment provider refuses shows its reason and what to do, not the generic failure', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(foundingFull);
  api.startCheckout.mockRejectedValue(new InvalidRequestError('Polar, our payment provider, did not accept the address sam@example.com: The domain name example.com does not accept email. Change your account email to an address that receives mail, or contact us at support@colliedesk.com.'));
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: /team, \$69 per month/i }));
  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('did not accept the address sam@example.com');
  expect(alert).toHaveTextContent('Change your account email');
  expect(alert).not.toHaveTextContent('The billing action failed');
});

// Billing switched off by the operator: nothing is sold, but a subscription can always be seen and cancelled.
const switchedOff = { cloud_sign_in: { enabled: true, publishable_key: 'pk' }, billing: { enabled: false, switched_off: true } };
const paid = { ...free, plan: 'team', status: 'active', active_subscriptions: 1, current_period_end: '2026-11-15T09:00:00Z', checkout_plans: [] };

test('with billing switched off a workspace that pays sees its plan and the portal to cancel, and nothing for sale', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.config.mockResolvedValue(switchedOff);
  api.billing.mockResolvedValue(paid);
  api.openBillingPortal.mockResolvedValue({ url: 'https://polar.example/portal' });
  renderRoute();
  expect(await screen.findByRole('note')).toHaveTextContent('Billing is turned off on this server: nothing is charged or locked meanwhile.');
  expect(screen.queryByRole('heading', { name: /choose a plan/i })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'Open billing portal' }));
  await waitFor(() => expect(assign).toHaveBeenCalledWith('https://polar.example/portal'));
  expect(api.startCheckout).not.toHaveBeenCalled();
});

test('with billing switched off a workspace that never paid sees no billing, and a remembered plan is dropped', async () => {
  rememberCheckoutIntent('team_monthly');
  api.me.mockResolvedValue(me('admin'));
  api.config.mockResolvedValue(switchedOff);
  api.billing.mockResolvedValue({ ...free, checkout_plans: [] });
  renderRoute();
  expect(await screen.findByText(/billing is not enabled/i)).toBeInTheDocument();
  expect(api.startCheckout).not.toHaveBeenCalled();
  expect(readCheckoutIntent()).toBeUndefined();
});

// The step after paying: the wait for the payment provider, the welcome once, and the migration row.
const foundingPaid = { ...free, plan: 'founding', status: 'active', active_subscriptions: 1, current_period_end: '2027-01-30T09:00:00Z', checkout_plans: [], limits: { ...free.limits, agents: 10, replies_per_24h: null } };
const setup = (importState: 'none' | 'running' | 'done') => ({
  steps: [{ key: 'email', state: 'none', detail: null }, { key: 'import', state: importState, detail: null }, { key: 'invite', state: 'alone', detail: null }, { key: 'first_reply', state: 'none', detail: null }],
  complete: false, recommended: null,
});
const sent = { freshdesk_address: 'northwind.freshdesk.com', times: '', requested_at: '2026-10-31T09:00:00Z' };
const migrationRow = () => screen.queryByText('Freshdesk migration');

test('after checkout the plans stay away until the provider confirms, then the welcome opens in place and is closed for good', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValueOnce(free).mockResolvedValue(foundingPaid);
  api.setupProgress.mockResolvedValue(setup('none'));
  const client = renderRoute({ checkoutReturned: true });
  expect(await screen.findByRole('heading', { name: 'Confirming your payment' })).toBeInTheDocument();
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(api.setupProgress).not.toHaveBeenCalled();
  await client.invalidateQueries({ queryKey: ['billing'] });
  const welcome = await screen.findByRole('dialog');
  expect(within(welcome).getByRole('heading', { name: 'Welcome to Founding' })).toBeInTheDocument();
  expect(screen.queryByRole('heading', { name: 'Confirming your payment' })).not.toBeInTheDocument();
  fireEvent.click(within(welcome).getByRole('button', { name: 'Later' }));
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(await screen.findByText('Freshdesk migration')).toBeInTheDocument();
});

test('a reload of the page the checkout returned to does not welcome the buyer again', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(foundingPaid);
  api.setupProgress.mockResolvedValue(setup('none'));
  renderRoute({ checkoutReturned: true });
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Later' }));
  cleanup();
  renderRoute({ checkoutReturned: true });
  await screen.findByText('Freshdesk migration');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
});

test('the page polls while the payment is confirmed, so the plan opens without a reload', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValueOnce(free).mockResolvedValue(foundingPaid);
  api.setupProgress.mockResolvedValue(setup('none'));
  renderRoute({ checkoutReturned: true });
  await screen.findByRole('heading', { name: 'Confirming your payment' });
  await act(async () => { await vi.advanceTimersByTimeAsync(2500); });
  expect(await screen.findByRole('heading', { name: 'Welcome to Founding' })).toBeInTheDocument();
});

test('a request for the Freshdesk migration is sent under one key, which a retry of the same request keeps', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(foundingPaid);
  api.setupProgress.mockResolvedValue(setup('none'));
  api.requestMigration.mockRejectedValueOnce(new Error('API 502')).mockResolvedValue(sent);
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: 'Send your Freshdesk address' }));
  const dialog = screen.getByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Freshdesk address'), { target: { value: 'northwind.freshdesk.com' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Send to Colliedesk' }));
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Could not send. Your details are still here.');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Try again' }));
  expect(await within(dialog).findByText('Sent. We reply by email within one working day.')).toBeInTheDocument();
  const [first, second] = api.requestMigration.mock.calls;
  expect(first[0]).toEqual({ freshdesk_address: 'northwind.freshdesk.com', times: '' });
  expect(typeof first[1]).toBe('string');
  expect(first[1]).not.toBe('');
  expect(second).toEqual(first);
});

test('a sent request shows on the migration row after the billing page is read again', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValueOnce(foundingPaid).mockResolvedValue({ ...foundingPaid, migration_requested_at: '2026-10-31T09:00:00Z' });
  api.setupProgress.mockResolvedValue(setup('none'));
  api.requestMigration.mockResolvedValue(sent);
  renderRoute();
  fireEvent.click(await screen.findByRole('button', { name: 'Send your Freshdesk address' }));
  const dialog = screen.getByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Freshdesk address'), { target: { value: 'northwind.freshdesk.com' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Send to Colliedesk' }));
  fireEvent.click(await within(dialog).findByRole('button', { name: 'Done' }));
  expect(await screen.findByText('Requested Oct 31')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Send your Freshdesk address' })).not.toBeInTheDocument();
});

test('a paid Team 25 workspace whose import is running shows the migration row as not requested', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue({ ...foundingPaid, plan: 'team25', limits: { ...foundingPaid.limits, agents: 25 } });
  api.setupProgress.mockResolvedValue(setup('running'));
  renderRoute();
  expect(await screen.findByText('Not requested')).toBeInTheDocument();
});

test('a paid Founding workspace with a finished Freshdesk import reads Done on the migration row', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(foundingPaid);
  api.setupProgress.mockResolvedValue(setup('done'));
  renderRoute();
  expect(await screen.findByText('Done')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Send your Freshdesk address' })).not.toBeInTheDocument();
});

test('when setup progress cannot be read the row still lets the admin send the address', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue(foundingPaid);
  api.setupProgress.mockRejectedValue(new Error('API 500'));
  renderRoute();
  expect(await screen.findByRole('button', { name: 'Send your Freshdesk address' })).toBeInTheDocument();
});

test('a workspace on a plan with no migration never asks for setup progress and has no migration row', async () => {
  api.me.mockResolvedValue(me('admin'));
  api.billing.mockResolvedValue({ ...foundingPaid, plan: 'team' });
  renderRoute();
  await screen.findByRole('button', { name: /open billing portal/i });
  expect(api.setupProgress).not.toHaveBeenCalled();
  expect(migrationRow()).not.toBeInTheDocument();
});
