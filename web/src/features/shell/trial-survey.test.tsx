import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, expect, test, vi } from 'vitest';
import { TrialSurvey } from './trial-survey';

// The analytics boundary: what PostHog would receive.
const analytics = vi.hoisted(() => ({ track: vi.fn(() => true) }));
vi.mock('../../lib/product-analytics', () => ({ track: analytics.track }));

const now = new Date('2026-10-19T08:00:00Z');
const inDays = (days: number) => new Date(now.getTime() + days * 86_400_000).toISOString();
const key = 'colliedesk-survey-switch-blocker-v1';

function meWith(overrides: { role?: 'admin' | 'agent'; plan?: string; endsIn?: number } = {}) {
  return {
    agent: { id: '11111111-1111-4111-8111-111111111111', email: 'ada@example.com', display_name: '', role: overrides.role ?? 'admin', timezone_override: null },
    workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
    timezone: 'UTC', scope: null,
    plan: { plan: overrides.plan ?? 'trial', trial_ends_at: inDays(overrides.endsIn ?? 5), locked: false, trial_extended: false, agents_over: 0, seat_paused: false },
  } as never;
}
const cloud = { enabled: true, realm: 'cloud' };
const show = (props: Partial<Parameters<typeof TrialSurvey>[0]> = {}) =>
  render(<TrialSurvey me={meWith()} analytics={cloud} now={now} {...props} />);
const question = /what is still missing for you to leave freshdesk for good/i;

beforeEach(() => { localStorage.clear(); analytics.track.mockReset(); analytics.track.mockReturnValue(true); });

test('asks the one question of a cloud trial admin whose trial ends within seven days', () => {
  show();
  expect(screen.getByRole('region', { name: question })).toBeInTheDocument();
  expect(screen.getByRole('textbox', { name: question })).toHaveAttribute('maxlength', '1000');
  expect(screen.getByRole('button', { name: 'Send' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Not now' })).toBeInTheDocument();
});

test('it shows on the last day of the window and not a day later', () => {
  const { unmount } = show({ me: meWith({ endsIn: 7 }) });
  expect(screen.getByRole('region', { name: question })).toBeInTheDocument();
  unmount();
  show({ me: meWith({ endsIn: 7.5 }) });
  expect(screen.queryByRole('region', { name: question })).not.toBeInTheDocument();
});

test.each([
  ['analytics is off', { analytics: { enabled: false, realm: 'cloud' } }],
  ['analytics config has not loaded', { analytics: undefined }],
  ['the install is self-hosted', { analytics: { enabled: true, realm: 'self-hosted' } }],
  ['the person is an agent, not an admin', { me: meWith({ role: 'agent' }) }],
  ['the workspace is on a paid plan', { me: meWith({ plan: 'team' }) }],
  ['the workspace has no plan', { me: { ...(meWith() as object), plan: null } as never }],
  ['the trial has more than seven days left', { me: meWith({ endsIn: 12 }) }],
])('stays hidden when %s', (_name, props) => {
  show(props as never);
  expect(screen.queryByRole('region', { name: question })).not.toBeInTheDocument();
});

test('stays hidden when it was answered or dismissed before', () => {
  localStorage.setItem(key, '1');
  show();
  expect(screen.queryByRole('region', { name: question })).not.toBeInTheDocument();
});

test('stays hidden when the browser refuses storage, so it cannot nag on every page', () => {
  const read = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('denied', 'SecurityError'); });
  show();
  expect(screen.queryByRole('region', { name: question })).not.toBeInTheDocument();
  read.mockRestore();
});

test('Send reports the typed answer, says thank you and never asks again', () => {
  const { unmount } = show();
  fireEvent.change(screen.getByRole('textbox', { name: question }), { target: { value: 'Automations and SLA reports' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(analytics.track).toHaveBeenCalledTimes(1);
  expect(analytics.track).toHaveBeenCalledWith('survey_answered', { survey: 'switch_blocker_v1', answer: 'Automations and SLA reports' });
  expect(screen.getByRole('status')).toHaveTextContent('Thank you');
  expect(screen.queryByRole('textbox', { name: question })).not.toBeInTheDocument();
  unmount();
  show();
  expect(screen.queryByRole('region', { name: question })).not.toBeInTheDocument();
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});

test('Send with an empty answer sends nothing', () => {
  show();
  expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();
  fireEvent.change(screen.getByRole('textbox', { name: question }), { target: { value: '   ' } });
  expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();
  expect(analytics.track).not.toHaveBeenCalled();
});

test('an answer the analytics library dropped is not thanked and can be sent again', () => {
  analytics.track.mockReturnValue(false);
  show();
  fireEvent.change(screen.getByRole('textbox', { name: question }), { target: { value: 'SSO' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(screen.getByRole('alert')).toHaveTextContent('could not be sent');
  expect(screen.getByRole('textbox', { name: question })).toHaveValue('SSO');
  expect(localStorage.getItem(key)).toBeNull();
});

test('Not now reports the dismissal without any text and never asks again', () => {
  const { unmount } = show();
  fireEvent.change(screen.getByRole('textbox', { name: question }), { target: { value: 'half-typed secret' } });
  fireEvent.click(screen.getByRole('button', { name: 'Not now' }));
  expect(analytics.track).toHaveBeenCalledWith('survey_dismissed', { survey: 'switch_blocker_v1' });
  expect(analytics.track).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole('region', { name: question })).not.toBeInTheDocument();
  unmount();
  show();
  expect(screen.queryByRole('region', { name: question })).not.toBeInTheDocument();
});
