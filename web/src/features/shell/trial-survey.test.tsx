import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { permissionsOf, rolesOf } from '../../test-roles';
import { TrialSurvey } from './trial-survey';

// The analytics boundary: what PostHog would receive.
const analytics = vi.hoisted(() => ({ track: vi.fn(() => true) }));
vi.mock('../../lib/product-analytics', () => ({ track: analytics.track }));

const now = new Date('2026-10-19T08:00:00Z');
const inDays = (days: number) => new Date(now.getTime() + days * 86_400_000).toISOString();
const key = 'colliedesk-survey-switch-blocker-v1';

function meWith(overrides: { role?: 'admin' | 'agent'; plan?: string; endsIn?: number; internal?: boolean } = {}) {
  return {
    agent: { id: '11111111-1111-4111-8111-111111111111', email: 'ada@example.com', display_name: '', roles: rolesOf(overrides.role ?? 'admin'), timezone_override: null }, permissions: permissionsOf(overrides.role ?? 'admin'),
    workspace: { id: '22222222-2222-4222-8222-222222222222', slug: 'beta-desk', name: 'Beta Desk', timezone: 'UTC' },
    timezone: 'UTC', token: null,
    plan: { plan: overrides.plan ?? 'trial', trial_ends_at: inDays(overrides.endsIn ?? 5), locked: false, trial_extended: false, agents_over: 0, seat_paused: false, internal: overrides.internal ?? false },
  } as never;
}
const cloud = { enabled: true, realm: 'cloud' };
const show = (props: Partial<Parameters<typeof TrialSurvey>[0]> = {}) =>
  render(<TrialSurvey me={meWith()} analytics={cloud} now={now} {...props} />);
const question = /what is still missing for you to leave freshdesk for good/i;
const tellUs = () => screen.queryByRole('button', { name: 'Tell us' });
const open = () => fireEvent.click(screen.getByRole('button', { name: 'Tell us' }));
const answerBox = () => screen.getByRole('textbox', { name: question });

beforeEach(() => { localStorage.clear(); analytics.track.mockReset(); analytics.track.mockReturnValue(true); });
afterEach(() => { vi.unstubAllGlobals(); });

test('a cloud trial admin in the last week sees one short line, not an open form', () => {
  show();
  expect(screen.getByText("What's still missing to leave Freshdesk?")).toBeInTheDocument();
  expect(tellUs()).toHaveAttribute('aria-expanded', 'false');
  expect(screen.getByRole('button', { name: 'Hide the Freshdesk question' })).toBeInTheDocument();
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
  expect(analytics.track).not.toHaveBeenCalled();
});

test('Tell us opens the question with the answer box, a call link, Not now and Send, and reports the opening once', () => {
  show();
  open();
  expect(screen.getByRole('dialog', { name: question })).toBeInTheDocument();
  expect(tellUs()).toHaveAttribute('aria-expanded', 'true');
  expect(answerBox()).toHaveAttribute('maxlength', '1000');
  expect(answerBox()).toHaveFocus();
  expect(screen.getByRole('link', { name: /book 15 minutes/i })).toHaveAttribute('href', 'https://cal.com/chinh/colliedesk-switch');
  expect(screen.getByRole('button', { name: 'Send' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Not now' })).toBeInTheDocument();
  expect(analytics.track).toHaveBeenCalledWith('survey_opened', { survey: 'switch_blocker_v1' });
  fireEvent.keyDown(answerBox(), { key: 'Escape' });
  open();
  expect(analytics.track).toHaveBeenCalledTimes(1);
});

test('Escape closes the question, gives focus back to Tell us and keeps what was typed', () => {
  show();
  open();
  fireEvent.change(answerBox(), { target: { value: 'Automations' } });
  fireEvent.keyDown(answerBox(), { key: 'Escape' });
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(tellUs()).toHaveFocus();
  open();
  expect(answerBox()).toHaveValue('Automations');
  expect(localStorage.getItem(key)).toBeNull();
});

test('a press outside closes the question and keeps what was typed', () => {
  show();
  open();
  fireEvent.change(answerBox(), { target: { value: 'SLA reports' } });
  fireEvent.pointerDown(document.body);
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  open();
  expect(answerBox()).toHaveValue('SLA reports');
});

test('on a phone the question opens as a sheet from the bottom of the screen', () => {
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  show();
  open();
  expect(screen.getByRole('dialog', { name: question })).toHaveClass('trial-survey-sheet');
});

test('it shows on the last day of the window and not a day later', () => {
  const { unmount } = show({ me: meWith({ endsIn: 7 }) });
  expect(tellUs()).toBeInTheDocument();
  unmount();
  show({ me: meWith({ endsIn: 7.5 }) });
  expect(tellUs()).not.toBeInTheDocument();
});

test.each([
  ['analytics is off', { analytics: { enabled: false, realm: 'cloud' } }],
  ['analytics config has not loaded', { analytics: undefined }],
  ['the install is self-hosted', { analytics: { enabled: true, realm: 'self-hosted' } }],
  ['the person is an agent, not an admin', { me: meWith({ role: 'agent' }) }],
  ['the person is on the team running this cloud', { me: meWith({ internal: true }) }],
  ['the workspace is on a paid plan', { me: meWith({ plan: 'team' }) }],
  ['the workspace has no plan', { me: { ...(meWith() as object), plan: null } as never }],
  ['the trial has more than seven days left', { me: meWith({ endsIn: 12 }) }],
])('stays hidden when %s', (_name, props) => {
  const { container } = show(props as never);
  expect(container).toBeEmptyDOMElement();
});

test('stays hidden when it was answered or dismissed before', () => {
  localStorage.setItem(key, '1');
  const { container } = show();
  expect(container).toBeEmptyDOMElement();
});

test('stays hidden when the browser refuses storage, so it cannot nag on every page', () => {
  const read = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('denied', 'SecurityError'); });
  const { container } = show();
  expect(container).toBeEmptyDOMElement();
  read.mockRestore();
});

test('Send reports the typed answer, says thank you in the line and never asks again', () => {
  const { unmount } = show();
  open();
  analytics.track.mockClear();
  fireEvent.change(answerBox(), { target: { value: 'Automations and SLA reports' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(analytics.track).toHaveBeenCalledTimes(1);
  expect(analytics.track).toHaveBeenCalledWith('survey_answered', { survey: 'switch_blocker_v1', answer: 'Automations and SLA reports' });
  expect(screen.getByRole('status')).toHaveTextContent('Thank you');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(tellUs()).not.toBeInTheDocument();
  unmount();
  const again = show();
  expect(again.container).toBeEmptyDOMElement();
});

test('Send with an empty answer sends nothing', () => {
  show();
  open();
  analytics.track.mockClear();
  expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();
  fireEvent.change(answerBox(), { target: { value: '   ' } });
  expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();
  expect(analytics.track).not.toHaveBeenCalled();
});

test('an answer the analytics library dropped is not thanked and can be sent again', () => {
  show();
  open();
  analytics.track.mockReturnValue(false);
  fireEvent.change(answerBox(), { target: { value: 'SSO' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send' }));
  expect(screen.getByRole('alert')).toHaveTextContent('could not be sent');
  expect(answerBox()).toHaveValue('SSO');
  expect(localStorage.getItem(key)).toBeNull();
});

test('Not now reports the dismissal without any text and never asks again', () => {
  const { unmount, container } = show();
  open();
  analytics.track.mockClear();
  fireEvent.change(answerBox(), { target: { value: 'half-typed secret' } });
  fireEvent.click(screen.getByRole('button', { name: 'Not now' }));
  expect(analytics.track).toHaveBeenCalledWith('survey_dismissed', { survey: 'switch_blocker_v1' });
  expect(analytics.track).toHaveBeenCalledTimes(1);
  expect(container).toBeEmptyDOMElement();
  unmount();
  expect(show().container).toBeEmptyDOMElement();
});

test('the close mark on the line dismisses the question the same way, without opening it', () => {
  const { unmount, container } = show();
  fireEvent.click(screen.getByRole('button', { name: 'Hide the Freshdesk question' }));
  expect(analytics.track).toHaveBeenCalledWith('survey_dismissed', { survey: 'switch_blocker_v1' });
  expect(analytics.track).toHaveBeenCalledTimes(1);
  expect(container).toBeEmptyDOMElement();
  unmount();
  expect(show().container).toBeEmptyDOMElement();
});
