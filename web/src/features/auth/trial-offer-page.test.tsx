import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { ConflictError, InvalidRequestError, PermissionError, UnauthorizedError } from '../../api/client';
import { returnToConsent } from './redeem-route';
import { TrialOfferPage } from './trial-offer-page';

const api = vi.hoisted(() => ({ me: vi.fn(), config: vi.fn(), trialOffer: vi.fn(), acceptTrialOffer: vi.fn() }));
vi.mock('../../api/client', async (actual) => ({ ...(await actual<object>()), api }));

const me = { agent: { email: 'ada@example.com', display_name: 'Ada' }, workspace: { name: 'Acme Support' }, timezone: 'UTC', plan: { plan: 'none', locked: true } };
const open = {
  id: '00000000-0000-0000-0000-000000000001', state: 'open', workspace_name: 'Acme Support', expires_at: '2026-11-19T09:00:00Z', accepted_at: null, accepted_by: null,
  revoked_at: null, trial_ends_at: '2026-10-15T09:00:00Z', plan: 'none', extends_to: '2026-11-03T09:00:00Z', can_accept: true, refusal: null, accept_by: ['Ada'],
};

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><TrialOfferPage token="tok" here="/trial-offer?token=tok" /></QueryClientProvider>);
}

// The dates shown depend on the year of now: the clock is the day after the offer was made.
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-21T12:00:00Z'));
  vi.clearAllMocks();
  api.me.mockResolvedValue(me);
  api.config.mockResolvedValue({ app_origin: 'https://desk.example.com', cloud_sign_in: { enabled: true } });
  api.trialOffer.mockResolvedValue(open);
});

afterEach(() => { vi.useRealTimers(); });

test('signed out, the page sends the person to sign in and back to this same page, and reads nothing', async () => {
  api.me.mockRejectedValue(new UnauthorizedError());
  show();
  expect(await screen.findByRole('heading', { name: 'Sign in to continue your trial' })).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Sign in' }).getAttribute('href')).toBe('https://desk.example.com/sign-in?next=%2Ftrial-offer%3Ftoken%3Dtok');
  expect(api.trialOffer).not.toHaveBeenCalled();
  // The sign-in page comes back here once signed in.
  expect(returnToConsent('/trial-offer?token=tok')).toBe('/trial-offer?token=tok');
  expect(returnToConsent('/settings')).toBeUndefined();
});

test('an admin sees the workspace and one button; opening changes nothing until Continue trial is pressed', async () => {
  api.acceptTrialOffer.mockResolvedValue({ offer: { ...open, state: 'accepted', can_accept: false, refusal: 'accepted' }, previous_trial_ends_at: open.trial_ends_at, trial_ends_at: '2026-11-04T12:00:00Z', extended: true });
  show();
  expect(await screen.findByRole('heading', { name: 'Continue the trial of Acme Support' })).toBeTruthy();
  expect(screen.getByText('Continue your trial: 14 days, starting when you press Continue.')).toBeTruthy();
  expect(api.trialOffer).toHaveBeenCalledWith('tok');
  expect(api.acceptTrialOffer).not.toHaveBeenCalled();
  expect(screen.getAllByRole('button')).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Continue trial' }));
  expect(await screen.findByText('Your trial now ends Nov 4, 12:00 PM.')).toBeTruthy();
  expect(api.acceptTrialOffer).toHaveBeenCalledWith('tok', expect.any(String));
  expect(screen.getByRole('link', { name: 'Open Colliedesk' }).getAttribute('href')).toBe('/');
});

test.each([
  ['accepted', 'This offer was already accepted.'],
  ['expired', 'This offer has expired.'],
  ['revoked', 'This offer was withdrawn.'],
])('a %s offer says so, gives the trial end and support, and offers no button', async (state, message) => {
  api.trialOffer.mockResolvedValue({ ...open, state, can_accept: false, refusal: state });
  show();
  expect(await screen.findByText(message)).toBeTruthy();
  expect(screen.getByText('Your trial ended Oct 15, 9:00 AM.')).toBeTruthy();
  expect(screen.getByRole('link', { name: 'support@colliedesk.com' }).getAttribute('href')).toBe('mailto:support@colliedesk.com');
  expect(screen.queryByRole('button', { name: 'Continue trial' })).toBeNull();
});

test('a workspace with a plan is told it needs no trial and nothing changes', async () => {
  api.trialOffer.mockResolvedValue({ ...open, plan: 'team', can_accept: false, refusal: 'has_plan' });
  show();
  expect(await screen.findByText('Acme Support has a plan, so it needs no trial. Nothing changed.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Continue trial' })).toBeNull();
});

test('an agent who is not an admin is told which admins can accept', async () => {
  api.trialOffer.mockResolvedValue({ ...open, can_accept: false, refusal: 'not_admin', accept_by: ['Grace', 'admin@example.com'] });
  show();
  expect(await screen.findByText('Only an admin can accept this offer. Ask Grace or admin@example.com to open this link.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Continue trial' })).toBeNull();
});

test('another workspace\'s link is refused without naming it, with a way to sign in to the right one', async () => {
  api.trialOffer.mockRejectedValue(new PermissionError('This trial offer is for another workspace than the one you are signed in to.', { code: 'trial_offer_other_workspace' }));
  show();
  expect(await screen.findByText('This trial offer is for another workspace than the one you are signed in to.')).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Sign in to another workspace' }).getAttribute('href')).toBe('https://desk.example.com/sign-in?next=%2Ftrial-offer%3Ftoken%3Dtok');
});

test('an unknown link says so', async () => {
  api.trialOffer.mockRejectedValue(new InvalidRequestError('No trial offer has this link.', { code: 'trial_offer_not_found' }));
  show();
  expect(await screen.findByText('No trial offer has this link.')).toBeTruthy();
});

test('an accept refused meanwhile shows the server\'s reason', async () => {
  api.acceptTrialOffer.mockRejectedValue(new ConflictError('This trial offer was withdrawn.', { code: 'trial_offer_revoked' }));
  show();
  fireEvent.click(await screen.findByRole('button', { name: 'Continue trial' }));
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('This trial offer was withdrawn.'));
});
